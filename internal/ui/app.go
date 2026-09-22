// Package ui is the Bubble Tea front end.
package ui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sveinungoverland/devopstui/internal/ado"
	"github.com/sveinungoverland/devopstui/internal/config"
	"github.com/sveinungoverland/devopstui/internal/model"
)

type viewID int

const (
	viewDash viewID = iota
	viewSprint
	viewBoard
	viewBacklog
	// viewItem is the full-screen view of one work item. It is not a tab:
	// you drill into it with D and leave it with esc.
	viewItem
)

func (v viewID) String() string {
	return [...]string{"Dashboard", "Sprint", "Board", "Backlog", "Item"}[v]
}

// tabViews are the numbered views shown in the header.
var tabViews = []viewID{viewDash, viewSprint, viewBoard, viewBacklog}

// App is the root model. It is used by pointer so closures in popups can
// reach it safely.
type App struct {
	client  ado.Client
	cfg     config.Config
	cfgPath string
	savePAT bool

	ctx        model.Context
	me         string
	projects   []model.Project
	teams      []model.Team
	iterations []model.Iteration
	// projectIterations is the project's full iteration tree, offered
	// alongside the team's sprints (iterations) by movableIterations, for the
	// pickers that set a work item's iteration. Sprint navigation ([, ],
	// :sprint, the current-sprint pick) stays on iterations, since it depends
	// on team-only data (Timeframe, the "current" sprint).
	projectIterations []model.Iteration
	boards            []model.Board

	w, h int
	view viewID

	sprint, backlog *list
	board           *board

	// Dashboard: a kanban of the requirement-level items assigned to @Me
	// (dashBoard) on top, and below it a kanban-with-swimlanes of those
	// PBIs' children (dashLanes) — state as columns, PBI as the lane.
	// myItems is the raw MyItems() result the top kanban is filtered from;
	// dashChildren is the last ChildrenOf() fetch for "my PBIs", keyed by
	// parent id, used both to build the lanes and as a lookup/childItems
	// source for those PBIs. dashLanesReq guards that fetch against a
	// slower, superseded response landing after a fresher one.
	myItems        []*model.WorkItem
	dashChildren   map[int][]*model.WorkItem
	dashLanesReq   int
	dashBoard      *board
	dashLanes      *lanes
	dashFocusLanes bool // false = kanban focused, true = lanes focused
	previewDash    bool // side preview pane beside the Dashboard's kanban
	dashShowDone   bool // show Done/Closed items on the top kanban

	// item is the drill-down view; itemStack keeps the trail so esc walks
	// back out, and itemReturn is the tab to land on at the bottom.
	item         *itemView
	itemStack    []*model.WorkItem
	itemReturn   viewID
	detail       viewport.Model
	focusDetail  bool
	previewList  bool // detail pane beside list views
	previewBoard bool // detail pane beside the board

	// comments caches each item's discussion by id, fetched on demand (the
	// drill-down's C toggle, and the Board's preview pane which has the
	// vertical room to show it alongside the description). commentsLoading
	// tracks in-flight fetches so a fast cursor doesn't refire them.
	comments        map[int][]model.Comment
	commentsLoading map[int]bool
	// commentsGen is bumped every time a comment is posted for an id, so a
	// discussion fetch dispatched before the post (and still in flight when
	// it lands) can tell its snapshot predates the post and skip clobbering
	// the cache with it.
	commentsGen map[int]int

	popup     popup
	cmd       cmdbar
	cmdActive bool

	spin     spinner.Model
	loading  map[viewID]bool
	busy     string // label for an in-flight write
	lastLoad time.Time
	gen      int
	// refreshGen identifies the live auto-refresh timer chain;
	// refreshEvery remembers the interval across an off/on toggle.
	refreshGen   int
	refreshEvery int

	flash    string
	flashErr bool

	pendingJump int // item to select once the next load lands (after create)
}

// New builds the app. The client may be a Fake for demo mode.
func New(client ado.Client, cfg config.Config, cfgPath string, savePAT bool) *App {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = lipgloss.NewStyle().Foreground(cAccent)
	a := &App{
		client: client, cfg: cfg, cfgPath: cfgPath, savePAT: savePAT,
		sprint:    newList("sprint is empty"),
		backlog:   newList("backlog is empty"),
		board:     newBoard(),
		dashBoard: newBoard(),
		dashLanes: newLanes(),
		spin:      sp,
		loading:   map[viewID]bool{},
		cmd:       newCmdbar(),
		view:      viewSprint,

		comments:        map[int][]model.Comment{},
		commentsLoading: map[int]bool{},
		commentsGen:     map[int]int{},

		previewList:  true,
		previewBoard: true,
		previewDash:  true,
	}
	a.wireLists()
	a.sprint.showDone = !cfg.HideDone
	a.ctx.Org = cfg.Org
	a.ctx.Project = cfg.Project
	a.ctx.Team = cfg.Team
	a.ctx.FilterTeam = cfg.FilterTeam
	a.refreshEvery = cfg.RefreshSeconds
	a.dashShowDone = cfg.DashShowDone
	return a
}

// include is the active team filter, nil when off.
func (a *App) include() func(*model.WorkItem) bool {
	if a.ctx.FilterTeam == "" || len(a.ctx.FilterAreas) == 0 {
		return nil
	}
	areas := a.ctx.FilterAreas
	return func(w *model.WorkItem) bool { return model.InAreas(w.AreaPath, areas) }
}

// applyTeamFilter pushes the current filter into every view.
func (a *App) applyTeamFilter() {
	inc := a.include()
	for _, l := range []*list{a.sprint, a.backlog} {
		l.include = inc
		if l.all != nil {
			l.rebuild()
		}
	}
	a.board.setItems(a.currentBoard(), a.sprint.all, a.ctx.Backlog, inc)
	a.refreshDashboard()
	a.refreshDetail()
}

// refreshDashboard rebuilds the Dashboard's kanban and lanes from whatever
// is already loaded (myItems, dashChildren) and the current team/sprint
// filters, without refetching. Call loadDashLanes to actually refetch
// children.
func (a *App) refreshDashboard() {
	inc := a.dashInclude()
	items := a.myItems
	if !a.dashShowDone {
		var kept []*model.WorkItem
		for _, it := range items {
			if !isDone(it.State) {
				kept = append(kept, it)
			}
		}
		items = kept
	}
	a.dashBoard.setItems(a.currentBoard(), items, a.ctx.Backlog, inc)
	a.dashLanes.setLanes(a.dashLaneParents(), a.dashChildren, nil)
}

// dashInclude combines the team filter with restricting to the selected
// sprint, when one is selected — the Dashboard's own extra scoping on top
// of "assigned to @Me", so it doesn't span every sprint at once while
// you're clearly looking at one. Progress badges on the kanban still count
// every loaded task regardless (board.setItems computes those before this
// filter is applied), same trade-off already accepted for the team filter.
func (a *App) dashInclude() func(*model.WorkItem) bool {
	team := a.include()
	iter := a.ctx.Iteration.Path
	if iter == "" {
		return team
	}
	return func(w *model.WorkItem) bool {
		if w.IterationPath != iter {
			return false
		}
		return team == nil || team(w)
	}
}

// dashLayout splits the Dashboard's body height between the kanban (top)
// and the lanes (bottom); topH also sizes the side preview pane when shown.
// Both heights include their panels' borders; the 1 is the summary line.
func (a *App) dashLayout() (topH, botH int) {
	budget := max(a.bodyHeight()-1, 11)
	// The lanes show every child (no more "+N" collapsing), so they get
	// the bigger share of the body.
	topH = max(budget*2/5, 5)
	botH = max(budget-topH, 6)
	return topH, botH
}

// myPBIs is the requirement-level items assigned to @Me, in the order
// MyItems returned them, after dashInclude (team filter + selected
// sprint). This is exactly the set dashBoard.setItems buckets into cards,
// kept available separately because the lanes and the ChildrenOf fetch
// need just the id list.
func (a *App) myPBIs() []*model.WorkItem {
	include := a.dashInclude()
	var out []*model.WorkItem
	for _, it := range a.myItems {
		if a.ctx.Backlog.TaskLevel(it) || it.Kind == model.KindEpic || it.Kind == model.KindFeature {
			continue
		}
		if include != nil && !include(it) {
			continue
		}
		out = append(out, it)
	}
	return out
}

// dashLaneParents is myPBIs further narrowed to those currently being
// worked on, so a "New" or "Done" PBI with a stray old task doesn't get a
// lane of its own. loadDashLanes still fetches children for the full
// myPBIs set (dashChildren keeps its current breadth for lookup/childItems'
// fallback use) — only which parents render as a lane changes here.
func (a *App) dashLaneParents() []*model.WorkItem {
	pbis := a.myPBIs()
	out := make([]*model.WorkItem, 0, len(pbis))
	for _, p := range pbis {
		if isActiveState(p.State) {
			out = append(out, p)
		}
	}
	return out
}

type teamFilterMsg struct {
	team  string
	areas []model.TeamArea
	err   error
}

// setTeamFilter resolves the team's areas and applies the filter; an empty
// team clears it.
func (a *App) setTeamFilter(team string) tea.Cmd {
	if team == "" {
		a.ctx.FilterTeam, a.ctx.FilterAreas = "", nil
		a.applyTeamFilter()
		a.persist()
		return a.setFlash("team filter off", false)
	}
	project := a.ctx.Project
	return func() tea.Msg {
		areas, err := a.client.TeamAreas(context.Background(), project, team)
		return teamFilterMsg{team: team, areas: areas, err: err}
	}
}

func (a *App) pickTeamFilter() tea.Cmd {
	items := []pickItem{{Label: "All teams", Desc: "no filter", Value: ""}}
	for _, t := range a.teams {
		desc := ""
		if t.Name == a.ctx.Team {
			desc = "owns the sprint"
		}
		if t.Name == a.ctx.FilterTeam {
			desc = "← active"
		}
		items = append(items, pickItem{Label: t.Name, Desc: desc, Value: t.Name})
	}
	a.popup = newPicker("Team filter", items, func(pi pickItem) tea.Cmd {
		return a.setTeamFilter(pi.Value.(string))
	})
	return nil
}

// wireLists gives the lists their callbacks into the app.
func (a *App) wireLists() {
	for _, l := range []*list{a.sprint, a.backlog} {
		l.taskLevel = func(w *model.WorkItem) bool { return a.ctx.Backlog.TaskLevel(w) }
		l.parentTitle = func(id int) string {
			if p := a.lookup(id); p != nil {
				return p.Title
			}
			return ""
		}
	}
	// Tasks already show in the PBI's detail preview and progress badge, so
	// the flat sprint view drops them. Backlog's query never returns
	// task-level items in the first place, flat or not.
	a.sprint.hideTasksFlat = true
}

// ------------------------------------------------------------ messages

type contextLoadedMsg struct {
	me                string
	projects          []model.Project
	teams             []model.Team
	iterations        []model.Iteration
	projectIterations []model.Iteration
	boards            []model.Board
	backlog           model.BacklogConfig
	filter            []model.TeamArea // areas of the configured filter team, if any
	err               error
}

type createdMsg struct {
	item *model.WorkItem
	err  error
}

type itemsLoadedMsg struct {
	view     viewID
	gen      int
	items    []*model.WorkItem
	external []*model.WorkItem
	err      error
}

type itemUpdatedMsg struct {
	item *model.WorkItem
	err  error
}

type commentAddedMsg struct {
	id      int
	comment model.Comment
	err     error
}

// dashLanesLoadedMsg carries the bulk ChildrenOf fetch for the Dashboard's
// lanes, issued after MyItems lands (it needs "my PBI" ids first). req ties
// it to the request that produced it: only the response matching the
// App's current dashLanesReq is applied, so an overlapping reload can never
// lose to an older, slower one that happens to land later.
type dashLanesLoadedMsg struct {
	req      int
	states   []string
	children map[int][]*model.WorkItem
	err      error
}

type popupMsg struct{ p popup }
type errMsg struct{ err error }
type flashMsg struct{ text string }
type clearFlashMsg struct{}

// tickMsg drives auto refresh. gen identifies the timer chain that
// produced it, so a toggle never leaves two chains running.
type tickMsg struct{ gen int }

// defaultRefreshSeconds is the interval used when auto refresh is turned
// on without one configured.
const defaultRefreshSeconds = 60

// ------------------------------------------------------------ init

func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{a.spin.Tick, a.loadContext()}
	if a.cfg.RefreshSeconds > 0 {
		cmds = append(cmds, a.startTicking())
	}
	return tea.Batch(cmds...)
}

// startTicking begins a new timer chain and abandons any previous one.
func (a *App) startTicking() tea.Cmd {
	a.refreshGen++
	return tick(a.refreshGen, a.cfg.RefreshSeconds)
}

func tick(gen, sec int) tea.Cmd {
	if sec <= 0 {
		return nil
	}
	return tea.Tick(time.Duration(sec)*time.Second, func(time.Time) tea.Msg { return tickMsg{gen: gen} })
}

// toggleAutoRefresh turns auto refresh on or off and remembers the choice
// in the config file. The interval is whatever was last configured.
func (a *App) toggleAutoRefresh() tea.Cmd {
	if a.cfg.RefreshSeconds > 0 {
		return a.setAutoRefresh(0)
	}
	every := a.refreshEvery
	if every <= 0 {
		every = defaultRefreshSeconds
	}
	return a.setAutoRefresh(every)
}

// setAutoRefresh sets the interval in seconds; 0 turns it off.
func (a *App) setAutoRefresh(sec int) tea.Cmd {
	if sec > 0 {
		a.refreshEvery = sec
	}
	a.cfg.RefreshSeconds = sec
	a.persist()
	if sec == 0 {
		a.refreshGen++ // orphan the running chain
		return a.setFlash("auto refresh off", false)
	}
	return tea.Batch(a.startTicking(), a.setFlash(fmt.Sprintf("auto refresh every %ds", sec), false))
}

func (a *App) loadContext() tea.Cmd {
	project, team, filterTeam := a.ctx.Project, a.ctx.Team, a.ctx.FilterTeam
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var m contextLoadedMsg
		if project != "" && filterTeam != "" {
			// Best effort: a bad filter team must not block startup.
			m.filter, _ = a.client.TeamAreas(ctx, project, filterTeam)
		}
		var err error
		if m.me, err = a.client.Me(ctx); err != nil {
			m.err = err
			return m
		}
		if m.projects, err = a.client.Projects(ctx); err != nil {
			m.err = err
			return m
		}
		if project == "" {
			return m
		}
		if m.teams, err = a.client.Teams(ctx, project); err != nil {
			m.err = err
			return m
		}
		if team == "" {
			return m
		}
		if m.iterations, err = a.client.Iterations(ctx, project, team); err != nil {
			m.err = err
			return m
		}
		// Best effort: a project-wide read can fail on permissions the team
		// iteration read doesn't need, and must not block startup.
		m.projectIterations, _ = a.client.ProjectIterations(ctx, project)
		if m.boards, err = a.client.Boards(ctx, project, team); err != nil {
			m.err = err
			return m
		}
		if m.backlog, err = a.client.BacklogConfig(ctx, project, team); err != nil {
			m.err = err
		}
		return m
	}
}

func (a *App) loadView(v viewID) tea.Cmd {
	if a.ctx.Project == "" || a.ctx.Team == "" {
		return nil
	}
	if (v == viewSprint || v == viewBoard) && a.ctx.Iteration.Path == "" {
		// Never query with an empty path; the server rejects it and the
		// message is cryptic. Tell the user what is missing instead.
		if len(a.iterations) == 0 {
			return a.setFlash(fmt.Sprintf("team %q has no sprints; add iterations to the team in Azure DevOps", a.ctx.Team), true)
		}
		return a.setFlash("no sprint selected: pick one with :sprint", true)
	}
	a.gen++
	gen := a.gen
	a.loading[v] = true
	c := a.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		m := itemsLoadedMsg{view: v, gen: gen}
		switch v {
		case viewSprint, viewBoard:
			m.view = viewSprint
			if c.Iteration.Path == c.Project {
				m.items, m.external, m.err = a.client.Unscheduled(ctx, c.Project, c.Team)
			} else {
				m.items, m.external, m.err = a.client.SprintItems(ctx, c.Project, c.Team, c.Iteration.Path)
			}
			if m.err != nil {
				m.err = fmt.Errorf("sprint %q: %w", c.Iteration.Path, m.err)
			}
		case viewBacklog:
			m.items, m.err = a.client.Backlog(ctx, c.Project, c.Team)
		case viewDash:
			m.items, m.err = a.client.MyItems(ctx, c.Project)
		}
		return m
	}
}

func (a *App) reloadAll() tea.Cmd {
	return tea.Batch(a.loadView(viewSprint), a.loadView(viewDash), a.loadView(viewBacklog))
}

// loadDashLanes bulk-fetches children for the current "my PBIs" set, for
// the Dashboard's lanes, plus the column order for the dominant child
// type. Called after MyItems lands, since it needs the PBI ids first.
// dashLanesReq is bumped on every call so a slower, superseded fetch can
// never overwrite a fresher one, regardless of arrival order.
func (a *App) loadDashLanes() tea.Cmd {
	pbis := a.myPBIs()
	a.dashLanesReq++
	req := a.dashLanesReq
	if len(pbis) == 0 {
		a.dashChildren = nil
		a.dashLanes.setLanes(nil, nil, nil)
		return nil
	}
	ids := make([]int, len(pbis))
	for i, p := range pbis {
		ids[i] = p.ID
	}
	project, cfg := a.ctx.Project, a.ctx.Backlog
	known := a.dashChildren
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		children, err := a.client.ChildrenOf(ctx, project, ids)
		if err != nil {
			return dashLanesLoadedMsg{req: req, err: err}
		}
		// Column order comes from the type most children share, same idea
		// as the item drill-down's kanban (dominantType, loadItemChildren).
		// Bugs are excluded: the lanes drop them (they carry different
		// states and already show on the kanban above when they're PBIs),
		// so they shouldn't skew which states the shared columns use.
		typ := dominantType(nonBugChildren(flattenChildren(children)))
		if typ == "" {
			typ = dominantType(nonBugChildren(flattenChildren(known)))
		}
		if typ == "" {
			typ = cfg.TaskType
		}
		if typ == "" {
			typ = "Task"
		}
		states, _ := a.client.States(ctx, project, typ)
		return dashLanesLoadedMsg{req: req, children: children, states: states}
	}
}

func flattenChildren(byParent map[int][]*model.WorkItem) []*model.WorkItem {
	var out []*model.WorkItem
	for _, children := range byParent {
		out = append(out, children...)
	}
	return out
}

// nonBugChildren drops Bugs: the Dashboard's lanes don't show them (see
// lanes.setLanes), so they shouldn't factor into picking the shared columns.
func nonBugChildren(items []*model.WorkItem) []*model.WorkItem {
	var out []*model.WorkItem
	for _, it := range items {
		if it.Kind != model.KindBug {
			out = append(out, it)
		}
	}
	return out
}

// ------------------------------------------------------------ update

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
		a.detail = viewport.New(a.detailWidth(), a.bodyHeight()-2)
		a.refreshDetail()
		return a, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		a.spin, cmd = a.spin.Update(msg)
		return a, cmd

	case tickMsg:
		if msg.gen != a.refreshGen || a.cfg.RefreshSeconds <= 0 {
			return a, nil // a stale chain, or auto refresh was turned off
		}
		next := tick(msg.gen, a.cfg.RefreshSeconds)
		if a.busy != "" || a.popup != nil || a.cmdActive {
			return a, next // never reload under an open dialog or a write
		}
		return a, tea.Batch(a.loadView(a.view), next)

	case contextLoadedMsg:
		return a, a.onContextLoaded(msg)

	case itemsLoadedMsg:
		a.loading[msg.view] = false
		if msg.err != nil {
			return a, a.setFlash(msg.err.Error(), true)
		}
		if msg.gen < a.gen-3 { // very stale
			return a, nil
		}
		a.lastLoad = time.Now()
		var cmd tea.Cmd
		switch msg.view {
		case viewSprint:
			a.sprint.setItems(msg.items, msg.external)
			a.board.setItems(a.currentBoard(), msg.items, a.ctx.Backlog, a.include())
		case viewBacklog:
			a.backlog.setItems(msg.items, nil)
		case viewDash:
			a.myItems = msg.items
			a.refreshDashboard()
			cmd = a.loadDashLanes()
		}
		a.syncItemView()
		if a.pendingJump != 0 {
			if l := a.activeList(); l != nil {
				if _, ok := l.tree.Get(a.pendingJump); ok {
					l.expandAll()
					l.jumpTo(a.pendingJump)
					a.pendingJump = 0
				}
			} else if a.view == viewBoard {
				a.board.jumpTo(a.pendingJump)
				a.pendingJump = 0
			} else if a.view == viewDash {
				a.dashBoard.jumpTo(a.pendingJump)
				a.pendingJump = 0
			}
		}
		a.refreshDetail()
		// Items were just refetched (explicit refresh, the auto-refresh
		// tick, or after a write); the discussion preview should not be
		// left showing a stale cache from before the reload.
		return a, tea.Batch(cmd, a.loadPreviewComments(true))

	case dashLanesLoadedMsg:
		if msg.req != a.dashLanesReq { // superseded by a newer fetch
			return a, nil
		}
		if msg.err != nil {
			return a, a.setFlash("active work: "+msg.err.Error(), true)
		}
		a.dashChildren = msg.children
		a.dashLanes.setLanes(a.dashLaneParents(), a.dashChildren, msg.states)
		return a, nil

	case itemUpdatedMsg:
		a.busy = ""
		a.reportSave(msg.err)
		if msg.err != nil {
			return a, a.setFlash(msg.err.Error(), true)
		}
		a.applyUpdate(msg.item)
		return a, a.setFlash(fmt.Sprintf("saved #%d", msg.item.ID), false)

	case commentAddedMsg:
		a.busy = ""
		a.reportSave(msg.err)
		if msg.err != nil {
			return a, a.setFlash(msg.err.Error(), true)
		}
		a.comments[msg.id] = append(a.comments[msg.id], msg.comment)
		a.commentsGen[msg.id]++
		if a.item != nil && a.item.item.ID == msg.id {
			a.item.setComments(a.comments[msg.id])
			a.item.showComments = true
		}
		return a, a.setFlash(fmt.Sprintf("commented on #%d", msg.id), false)

	case createdMsg:
		a.busy = ""
		if msg.err != nil {
			return a, a.setFlash(msg.err.Error(), true)
		}
		flash := a.setFlash(fmt.Sprintf("created %s #%d", msg.item.Type, msg.item.ID), false)
		if a.view == viewItem && a.item != nil {
			// Show it in the kanban straight away, then reconcile.
			a.item.setChildren(append(a.item.children, msg.item), nil)
			a.item.focusKan = true
			a.item.jumpTo(msg.item.ID)
			return a, tea.Batch(a.reloadAll(), a.loadItemChildren(a.item.item), flash)
		}
		a.pendingJump = msg.item.ID
		return a, tea.Batch(a.reloadAll(), flash)

	case bulkDoneMsg:
		a.busy = ""
		return a, a.onBulkDone(msg)

	case popupMsg:
		a.popup = msg.p
		return a, nil

	case itemLoadedMsg:
		if a.item == nil || a.item.item.ID != msg.id {
			return a, nil // drilled elsewhere while the fetch was in flight
		}
		a.item.loading = false
		if msg.err != nil {
			return a, a.setFlash("children: "+msg.err.Error(), true)
		}
		a.item.setChildren(msg.children, msg.states)
		if len(msg.children) == 0 {
			a.item.focusKan = false
		}
		return a, nil

	case commentsLoadedMsg:
		delete(a.commentsLoading, msg.id)
		// A comment posted after this fetch started means the fetch's
		// snapshot predates it; applying it now would silently drop the
		// posted comment from the cache and the discussion pane.
		stale := msg.since != a.commentsGen[msg.id]
		if msg.err == nil && !stale {
			a.comments[msg.id] = msg.comments
		}
		if a.item != nil && a.item.item.ID == msg.id {
			if msg.err != nil {
				a.item.commentsLoading = false
				return a, a.setFlash("discussion: "+msg.err.Error(), true)
			}
			if !stale {
				a.item.setComments(msg.comments)
			}
		}
		if it := a.currentItem(); it != nil && it.ID == msg.id &&
			(a.view == viewBoard || a.view == viewSprint || a.view == viewBacklog) {
			a.refreshDetail()
		}
		return a, nil

	case teamFilterMsg:
		if msg.err != nil {
			return a, a.setFlash("team filter: "+msg.err.Error(), true)
		}
		if len(msg.areas) == 0 {
			return a, a.setFlash(fmt.Sprintf("team %q has no area paths", msg.team), true)
		}
		a.ctx.FilterTeam, a.ctx.FilterAreas = msg.team, msg.areas
		a.applyTeamFilter()
		a.persist()
		return a, a.setFlash("filtering on "+msg.team, false)

	case editorDoneMsg:
		if msg.err != nil {
			return a, a.setFlash("editor: "+msg.err.Error(), true)
		}
		if !msg.changed {
			return a, a.setFlash(msg.noun+" unchanged", false)
		}
		return a, msg.apply(msg.text)

	case errMsg:
		a.busy = ""
		return a, a.setFlash(msg.err.Error(), true)

	case flashMsg:
		return a, a.setFlash(msg.text, false)

	case clearFlashMsg:
		a.flash = ""
		return a, nil

	case tea.KeyMsg:
		return a, a.onKey(msg)
	}

	// Anything else (cursor blink etc.) goes to the focused input.
	if a.popup != nil {
		return a, a.updatePopup(msg)
	}
	if a.cmdActive {
		var cmd tea.Cmd
		a.cmd.input, cmd = a.cmd.input.Update(msg)
		return a, cmd
	}
	if l := a.activeList(); l != nil && l.filtering {
		var cmd tea.Cmd
		l.filter, cmd = l.filter.Update(msg)
		return a, cmd
	}
	return a, nil
}

// reportSave tells a still-open description editor how its ctrl+w write
// went, so the "saved" baseline only moves when the server took the text.
func (a *App) reportSave(err error) {
	p := a.popup
	if f, ok := p.(*form); ok {
		p = f.child
	}
	if e, ok := p.(*mdEditor); ok {
		e.saveDone(err)
	}
}

// updatePopup forwards msg to the popup. A popup callback may itself open a
// new popup (picker → confirm), so only clear the slot when nothing replaced it.
func (a *App) updatePopup(msg tea.Msg) tea.Cmd {
	old := a.popup
	next, cmd := old.Update(msg)
	if a.popup == old {
		a.popup = next
	}
	return cmd
}

func (a *App) onContextLoaded(msg contextLoadedMsg) tea.Cmd {
	if msg.err != nil {
		return a.setFlash(msg.err.Error(), true)
	}
	a.me = msg.me
	a.projects = msg.projects
	if a.ctx.Project == "" {
		return a.pickProject()
	}
	a.teams = msg.teams
	if a.ctx.Team == "" {
		return a.pickTeam()
	}
	// A synthetic entry alongside the real sprints, picked the same way, so
	// the user can ask to see items with no sprint at all (project root is
	// the backlog convention already used by pickMoveTarget's "Backlog").
	a.iterations = append(append([]model.Iteration(nil), msg.iterations...), model.Iteration{Path: a.ctx.Project, Name: "Unscheduled"})
	a.projectIterations = msg.projectIterations
	a.boards = msg.boards
	a.ctx.Backlog = msg.backlog
	a.ctx.FilterAreas = msg.filter
	if a.ctx.FilterTeam != "" && len(msg.filter) == 0 {
		a.ctx.FilterTeam = "" // team unknown or without areas: filter off
	}
	a.applyTeamFilter()
	if a.ctx.Iteration.Path == "" {
		a.ctx.Iteration = a.currentIteration()
	}
	if a.ctx.Iteration.Path == "" {
		a.persist()
		return tea.Batch(a.loadView(viewDash), a.loadView(viewBacklog), a.loadView(viewSprint)) // sprint load only flashes the reason
	}
	a.persist()
	return a.reloadAll()
}

// movableIterations is what the move popup and the edit form's Iteration
// field offer: the team's sprints (including the synthetic "Unscheduled"
// entry, so the current-sprint badge and existing order survive) plus any
// project iteration not already among them, for setting a work item's
// iteration to something outside the team's own sprints.
func (a *App) movableIterations() []model.Iteration {
	out := append([]model.Iteration(nil), a.iterations...)
	have := map[string]bool{}
	for _, it := range out {
		have[it.Path] = true
	}
	for _, it := range a.projectIterations {
		if !have[it.Path] {
			have[it.Path] = true
			out = append(out, it)
		}
	}
	return out
}

// currentIteration picks the sprint to show: the one Azure DevOps marks
// current, else the one whose dates cover today, else the first future
// one, else the last known.
func (a *App) currentIteration() model.Iteration {
	for _, it := range a.iterations {
		if it.Timeframe == "current" && it.Path != "" {
			return it
		}
	}
	now := time.Now()
	for _, it := range a.iterations {
		if it.Path != "" && !it.Start.IsZero() && !it.Start.After(now) && !it.Finish.Before(now.AddDate(0, 0, -1)) {
			return it
		}
	}
	for _, it := range a.iterations {
		if it.Path != "" && it.Start.After(now) {
			return it
		}
	}
	// Prefer any real sprint over the synthetic "Unscheduled" entry (no
	// dates, so it never matched above): it sits last in a.iterations and
	// would otherwise always win this scan.
	for i := len(a.iterations) - 1; i >= 0; i-- {
		if it := a.iterations[i]; it.Path != "" && it.Path != a.ctx.Project {
			return it
		}
	}
	if len(a.iterations) > 0 {
		return a.iterations[len(a.iterations)-1]
	}
	return model.Iteration{}
}

func (a *App) currentBoard() model.Board {
	for _, b := range a.boards {
		if b.Name == a.ctx.Board {
			return b
		}
	}
	// Prefer the requirement-level board, which is usually listed first.
	if len(a.boards) > 0 {
		return a.boards[0]
	}
	return model.Board{}
}

func (a *App) persist() {
	c := a.cfg
	c.Project, c.Team, c.FilterTeam = a.ctx.Project, a.ctx.Team, a.ctx.FilterTeam
	if !a.savePAT {
		c.PAT = ""
	}
	_ = config.Save(a.cfgPath, c)
}

func (a *App) setFlash(text string, isErr bool) tea.Cmd {
	a.flash, a.flashErr = text, isErr
	d := 3 * time.Second
	if isErr {
		d = 8 * time.Second
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return clearFlashMsg{} })
}

// ------------------------------------------------------------ keys

func (a *App) onKey(msg tea.KeyMsg) tea.Cmd {
	if a.popup != nil {
		return a.updatePopup(msg)
	}
	if a.cmdActive {
		return a.onCmdKey(msg)
	}
	if l := a.activeList(); l != nil && l.filtering {
		switch msg.String() {
		case "esc":
			l.filtering = false
			l.filter.SetValue("")
			l.filter.Blur()
			l.rebuild()
			a.refreshDetail()
			return nil
		case "enter":
			l.filtering = false
			l.filter.Blur()
			return nil
		}
		var cmd tea.Cmd
		l.filter, cmd = l.filter.Update(msg)
		l.rebuild()
		a.refreshDetail()
		return cmd
	}
	if a.focusDetail {
		switch {
		case key.Matches(msg, keys.Focus), key.Matches(msg, keys.Back), key.Matches(msg, keys.Quit):
			a.focusDetail = false
			return nil
		}
		var cmd tea.Cmd
		a.detail, cmd = a.detail.Update(msg)
		return cmd
	}

	if a.view == viewItem && a.item != nil {
		if cmd, handled := a.itemOverride(msg); handled {
			return cmd
		}
	}
	if a.view == viewDash {
		if cmd, handled := a.dashOverride(msg); handled {
			return cmd
		}
	}

	// Global keys.
	switch {
	case key.Matches(msg, keys.Quit):
		if l := a.activeList(); l != nil && len(l.selected) > 0 {
			l.clearSelection()
			return nil
		}
		if a.view == viewItem {
			return a.closeItem() // q walks out of the drill-down first
		}
		return tea.Quit
	case key.Matches(msg, keys.Help):
		a.popup = helpPopup{}
		return nil
	case key.Matches(msg, keys.Command):
		a.cmdActive = true
		a.cmd.input.SetValue("")
		return a.cmd.input.Focus()
	case key.Matches(msg, keys.Refresh):
		return a.loadView(a.view)
	case key.Matches(msg, keys.AutoRefresh):
		return a.toggleAutoRefresh()
	case key.Matches(msg, keys.Dashboard):
		return a.switchView(viewDash)
	case key.Matches(msg, keys.Sprint):
		return a.switchView(viewSprint)
	case key.Matches(msg, keys.Board):
		return a.switchView(viewBoard)
	case key.Matches(msg, keys.Backlog):
		return a.switchView(viewBacklog)
	case key.Matches(msg, keys.PrevSprint):
		return a.shiftSprint(-1)
	case key.Matches(msg, keys.NextSprint):
		return a.shiftSprint(1)
	case key.Matches(msg, keys.CurSprint):
		return a.setIteration(a.currentIteration())
	case key.Matches(msg, keys.TeamFilter):
		return a.pickTeamFilter()
	case key.Matches(msg, keys.Focus):
		a.focusDetail = true
		a.refreshDetail()
		return nil
	case key.Matches(msg, keys.Preview):
		switch a.view {
		case viewBoard:
			a.previewBoard = !a.previewBoard
		case viewDash:
			a.previewDash = !a.previewDash
		default:
			a.previewList = !a.previewList
		}
		a.refreshDetail()
		return nil
	case key.Matches(msg, keys.Open):
		return a.openBrowser()
	case key.Matches(msg, keys.Yank):
		return a.yank()
	}

	if key.Matches(msg, keys.New) {
		return a.createChild()
	}
	if key.Matches(msg, keys.NewBug) {
		return a.createBug()
	}
	if a.view == viewItem {
		return a.onItemKey(msg)
	}
	if key.Matches(msg, keys.Details) {
		return a.openItem(a.currentItem())
	}
	if a.view == viewBoard {
		if msg.String() == "enter" { // enter drills in from a card
			return a.openItem(a.currentItem())
		}
		return a.onBoardKey(msg)
	}
	if a.view == viewDash {
		if msg.String() == "enter" { // enter drills in from a card
			return a.openItem(a.currentItem())
		}
		return a.onDashKey(msg)
	}
	return a.onListKey(msg, a.activeList())
}

// dashOverride handles the keys the Dashboard's layout redefines: Tab
// toggles focus between the kanban (top) and the lanes (bottom); it is a
// no-op when there are no lanes to focus. The preview pane is never part
// of this cycle — it isn't a focusable pane at all on the Dashboard — so
// ctrl+u/ctrl+d (scrollPreview) are the only way to scroll it, and they
// work the same regardless of which of the two panes has focus.
func (a *App) dashOverride(msg tea.KeyMsg) (tea.Cmd, bool) {
	if key.Matches(msg, keys.Focus) {
		if len(a.dashLanes.ls) > 0 {
			a.dashFocusLanes = !a.dashFocusLanes
		}
		return nil, true
	}
	return nil, false
}

// scrollPreview scrolls the side preview pane by half a page when it is
// visible, without touching the underlying list/board selection. It must
// not be followed by refreshDetail() — that would call GotoTop() and undo
// the scroll — so callers return immediately on success. Reports false
// when there is no preview to scroll (hidden, or the terminal too narrow)
// so the caller can fall back to its own meaning for the same key.
func (a *App) scrollPreview(down bool) bool {
	if a.detailWidth() == 0 {
		return false
	}
	if down {
		a.detail.HalfViewDown()
	} else {
		a.detail.HalfViewUp()
	}
	return true
}

func (a *App) onDashKey(msg tea.KeyMsg) tea.Cmd {
	if a.dashFocusLanes {
		ln := a.dashLanes
		switch {
		case key.Matches(msg, keys.PreviewDown):
			if a.scrollPreview(true) {
				return nil
			}
		case key.Matches(msg, keys.PreviewUp):
			if a.scrollPreview(false) {
				return nil
			}
		case key.Matches(msg, keys.Down):
			ln.move(1, 0)
		case key.Matches(msg, keys.Up):
			ln.move(-1, 0)
		case key.Matches(msg, keys.Left):
			ln.move(0, -1)
		case key.Matches(msg, keys.Right):
			ln.move(0, 1)
		case key.Matches(msg, keys.Select):
			ln.toggleSelect()
		case key.Matches(msg, keys.ClearSel):
			ln.selected = map[int]bool{}
		case key.Matches(msg, keys.ColLeft):
			return a.moveLaneColumn(-1)
		case key.Matches(msg, keys.ColRight):
			return a.moveLaneColumn(1)
		case key.Matches(msg, keys.Closed):
			a.dashShowDone = !a.dashShowDone
			a.cfg.DashShowDone = a.dashShowDone
			a.persist()
			a.refreshDashboard()
		default:
			return a.onActionKey(msg)
		}
		a.refreshDetail()
		return nil
	}
	b := a.dashBoard
	switch {
	case key.Matches(msg, keys.PreviewDown):
		if a.scrollPreview(true) {
			return nil
		}
	case key.Matches(msg, keys.PreviewUp):
		if a.scrollPreview(false) {
			return nil
		}
	case key.Matches(msg, keys.Down):
		b.move(0, 1)
	case key.Matches(msg, keys.Up):
		b.move(0, -1)
	case key.Matches(msg, keys.Left):
		b.move(-1, 0)
	case key.Matches(msg, keys.Right):
		b.move(1, 0)
	case key.Matches(msg, keys.Top):
		b.row = 0
	case key.Matches(msg, keys.Bottom):
		b.move(0, 1<<20)
	case key.Matches(msg, keys.Select):
		b.toggleSelect()
	case key.Matches(msg, keys.ClearSel):
		b.selected = map[int]bool{}
	case key.Matches(msg, keys.ColLeft):
		return a.moveColumn(-1)
	case key.Matches(msg, keys.ColRight):
		return a.moveColumn(1)
	case key.Matches(msg, keys.Closed):
		a.dashShowDone = !a.dashShowDone
		a.cfg.DashShowDone = a.dashShowDone
		a.persist()
		a.refreshDashboard()
	default:
		return a.onActionKey(msg)
	}
	a.refreshDetail()
	return nil
}

func (a *App) onListKey(msg tea.KeyMsg, l *list) tea.Cmd {
	switch {
	case key.Matches(msg, keys.Down):
		l.move(1)
	case key.Matches(msg, keys.Up):
		l.move(-1)
	case key.Matches(msg, keys.Top):
		l.move(-len(l.rows))
	case key.Matches(msg, keys.Bottom):
		l.move(len(l.rows))
	case key.Matches(msg, keys.PageDown):
		l.move(a.bodyHeight() / 2)
	case key.Matches(msg, keys.PageUp):
		l.move(-a.bodyHeight() / 2)
	case key.Matches(msg, keys.PreviewDown):
		if a.scrollPreview(true) {
			return nil
		}
		l.move(a.bodyHeight() / 2)
	case key.Matches(msg, keys.PreviewUp):
		if a.scrollPreview(false) {
			return nil
		}
		l.move(-a.bodyHeight() / 2)
	case key.Matches(msg, keys.Expand):
		if msg.String() == "enter" && a.detailWidth() == 0 {
			a.focusDetail = true
		} else {
			l.expand()
		}
	case key.Matches(msg, keys.Collapse):
		l.collapse()
	case key.Matches(msg, keys.ExpandAll):
		l.expandAll()
	case key.Matches(msg, keys.CollapseAll):
		l.collapseAll()
	case key.Matches(msg, keys.Filter):
		l.filtering = true
		return l.filter.Focus()
	case key.Matches(msg, keys.Select):
		l.toggleSelect()
	case key.Matches(msg, keys.Visual):
		l.toggleVisual()
	case key.Matches(msg, keys.SelectAll):
		l.selectAll()
	case key.Matches(msg, keys.ClearSel):
		l.clearSelection()
	case key.Matches(msg, keys.Flat):
		l.flat = !l.flat
		l.rebuild()
	case key.Matches(msg, keys.Closed):
		l.showDone = !l.showDone
		l.rebuild()
		if l == a.sprint {
			a.cfg.HideDone = !l.showDone
			a.persist()
		}
	default:
		return a.onActionKey(msg)
	}
	a.refreshDetail()
	return a.loadPreviewComments(false)
}

func (a *App) onBoardKey(msg tea.KeyMsg) tea.Cmd {
	b := a.board
	switch {
	case key.Matches(msg, keys.PreviewDown):
		if a.scrollPreview(true) {
			return nil
		}
	case key.Matches(msg, keys.PreviewUp):
		if a.scrollPreview(false) {
			return nil
		}
	case key.Matches(msg, keys.Down):
		b.move(0, 1)
	case key.Matches(msg, keys.Up):
		b.move(0, -1)
	case key.Matches(msg, keys.Left):
		b.move(-1, 0)
	case key.Matches(msg, keys.Right):
		b.move(1, 0)
	case key.Matches(msg, keys.Top):
		b.row = 0
	case key.Matches(msg, keys.Bottom):
		b.move(0, 1<<20)
	case key.Matches(msg, keys.Select):
		b.toggleSelect()
	case key.Matches(msg, keys.ClearSel):
		b.selected = map[int]bool{}
	case key.Matches(msg, keys.ColLeft):
		return a.moveColumn(-1)
	case key.Matches(msg, keys.ColRight):
		return a.moveColumn(1)
	default:
		return a.onActionKey(msg)
	}
	a.refreshDetail()
	return a.loadPreviewComments(false)
}

func (a *App) onCmdKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "ctrl+c":
		a.cmdActive = false
		a.cmd.input.Blur()
		return nil
	case "enter":
		a.cmdActive = false
		a.cmd.input.Blur()
		return a.runCommand(strings.TrimSpace(a.cmd.input.Value()))
	}
	var cmd tea.Cmd
	a.cmd.input, cmd = a.cmd.input.Update(msg)
	return cmd
}

func (a *App) runCommand(c string) tea.Cmd {
	if c == "" {
		return nil
	}
	if id, err := strconv.Atoi(c); err == nil {
		return a.showItem(id)
	}
	word, arg, _ := strings.Cut(c, " ")
	switch word {
	case "q", "quit":
		return tea.Quit
	case "sprint", "s", "iteration":
		if arg != "" {
			for _, it := range a.iterations {
				if strings.EqualFold(it.Name, arg) {
					return a.setIteration(it)
				}
			}
		}
		return a.pickIteration()
	case "team", "t":
		return a.pickTeam()
	case "filter", "tf":
		if arg == "" {
			return a.pickTeamFilter()
		}
		if arg == "off" || arg == "all" {
			return a.setTeamFilter("")
		}
		for _, t := range a.teams {
			if strings.EqualFold(t.Name, arg) {
				return a.setTeamFilter(t.Name)
			}
		}
		return a.setFlash("unknown team: "+arg, true)
	case "project", "proj", "p":
		return a.pickProject()
	case "board", "b":
		return a.pickBoard()
	case "backlog":
		return a.switchView(viewBacklog)
	case "dash", "dashboard", "d":
		return a.switchView(viewDash)
	case "refresh", "r":
		return a.reloadAll()
	case "auto":
		switch arg {
		case "":
			return a.toggleAutoRefresh()
		case "off", "0":
			return a.setAutoRefresh(0)
		case "on":
			every := a.refreshEvery
			if every <= 0 {
				every = defaultRefreshSeconds
			}
			return a.setAutoRefresh(every)
		}
		sec, err := strconv.Atoi(arg)
		if err != nil || sec < 5 {
			return a.setFlash("auto: give a number of seconds (5 or more), on, or off", true)
		}
		return a.setAutoRefresh(sec)
	case "help", "h":
		a.popup = helpPopup{}
		return nil
	}
	return a.setFlash("unknown command: "+word, true)
}

func (a *App) switchView(v viewID) tea.Cmd {
	if v != viewItem {
		a.item, a.itemStack = nil, nil
	}
	a.view = v
	a.focusDetail = false
	a.refreshDetail()
	if a.needsLoad(v) {
		return a.loadView(v)
	}
	return a.loadPreviewComments(false)
}

func (a *App) needsLoad(v viewID) bool {
	switch v {
	case viewSprint, viewBoard:
		return a.sprint.all == nil && !a.loading[viewSprint]
	case viewBacklog:
		return a.backlog.all == nil && !a.loading[viewBacklog]
	case viewDash:
		return a.myItems == nil && !a.loading[viewDash]
	}
	return false
}

func (a *App) shiftSprint(delta int) tea.Cmd {
	for i, it := range a.iterations {
		if it.Path == a.ctx.Iteration.Path {
			j := i + delta
			if j < 0 || j >= len(a.iterations) {
				return a.setFlash("no sprint in that direction", true)
			}
			return a.setIteration(a.iterations[j])
		}
	}
	return nil
}

func (a *App) setIteration(it model.Iteration) tea.Cmd {
	if it.Path == "" {
		return nil
	}
	a.ctx.Iteration = it
	a.sprint.clearSelection()
	// The Dashboard's kanban/lanes are also scoped to the selected sprint:
	// re-bucket immediately from what's already loaded, then refetch the
	// lanes' children since the qualifying set of "my PBIs" may have
	// changed.
	a.refreshDashboard()
	a.refreshDetail()
	return tea.Batch(a.loadView(viewSprint), a.loadDashLanes())
}

// ------------------------------------------------------------ pickers for context

func (a *App) pickProject() tea.Cmd {
	var items []pickItem
	for _, p := range a.projects {
		items = append(items, pickItem{Label: p.Name, Value: p})
	}
	a.popup = newPicker("Project", items, func(pi pickItem) tea.Cmd {
		p := pi.Value.(model.Project)
		a.ctx.Project, a.ctx.Team, a.ctx.Iteration = p.Name, "", model.Iteration{}
		a.sprint, a.backlog = newList("sprint is empty"), newList("backlog is empty")
		a.dashBoard, a.dashLanes = newBoard(), newLanes()
		a.myItems, a.dashChildren = nil, nil
		a.wireLists()
		a.sprint.showDone = !a.cfg.HideDone
		return a.loadContext()
	})
	return nil
}

func (a *App) pickTeam() tea.Cmd {
	var items []pickItem
	for _, t := range a.teams {
		items = append(items, pickItem{Label: t.Name, Value: t})
	}
	a.popup = newPicker("Team", items, func(pi pickItem) tea.Cmd {
		t := pi.Value.(model.Team)
		a.ctx.Team, a.ctx.Iteration = t.Name, model.Iteration{}
		return a.loadContext()
	})
	return nil
}

func (a *App) pickIteration() tea.Cmd {
	var items []pickItem
	for _, it := range a.iterations {
		items = append(items, pickItem{Label: it.Name, Desc: iterDesc(it), Value: it})
	}
	a.popup = newPicker("Sprint", items, func(pi pickItem) tea.Cmd {
		return a.setIteration(pi.Value.(model.Iteration))
	})
	return nil
}

func (a *App) pickBoard() tea.Cmd {
	var items []pickItem
	for _, b := range a.boards {
		items = append(items, pickItem{Label: b.Name, Desc: fmt.Sprintf("%d columns", len(b.Columns)), Value: b})
	}
	a.popup = newPicker("Board", items, func(pi pickItem) tea.Cmd {
		b := pi.Value.(model.Board)
		a.ctx.Board = b.Name
		a.board.setItems(b, a.sprint.all, a.ctx.Backlog, a.include())
		return a.switchView(viewBoard)
	})
	return nil
}

func iterDesc(it model.Iteration) string {
	s := ""
	if !it.Start.IsZero() {
		s = it.Start.Format("Jan 2") + " – " + it.Finish.Format("Jan 2")
	}
	if it.Timeframe == "current" {
		s += "  ← current"
	}
	return s
}

// ------------------------------------------------------------ helpers

func (a *App) activeList() *list {
	switch a.view {
	case viewSprint:
		return a.sprint
	case viewBacklog:
		return a.backlog
	}
	return nil
}

// activeBoard is the kanban a column move (H/L) applies to: the Board tab's
// board, or the Dashboard's kanban when its lanes don't have focus. Lanes
// aren't a board (their state is fixed to "In Progress"), so there is no
// column move for them.
func (a *App) activeBoard() *board {
	switch a.view {
	case viewBoard:
		return a.board
	case viewDash:
		if !a.dashFocusLanes {
			return a.dashBoard
		}
	}
	return nil
}

// currentItem is the highlighted item in whichever view is active.
func (a *App) currentItem() *model.WorkItem {
	switch {
	case a.view == viewItem && a.item != nil:
		return a.item.current()
	case a.view == viewBoard:
		return a.board.current()
	case a.view == viewDash:
		if a.dashFocusLanes {
			return a.dashLanes.current()
		}
		return a.dashBoard.current()
	}
	if l := a.activeList(); l != nil {
		return l.current()
	}
	return nil
}

// targetItems is the selection or the highlighted item.
func (a *App) targetItems() []*model.WorkItem {
	switch {
	case a.view == viewItem:
		if it := a.currentItem(); it != nil {
			return []*model.WorkItem{it}
		}
		return nil
	case a.view == viewBoard:
		return a.board.targetItems()
	case a.view == viewDash:
		if a.dashFocusLanes {
			return a.dashLanes.targetItems()
		}
		return a.dashBoard.targetItems()
	}
	if l := a.activeList(); l != nil {
		return l.targetItems()
	}
	return nil
}

func (a *App) clearSelection() {
	switch a.view {
	case viewBoard:
		a.board.selected = map[int]bool{}
	case viewDash:
		a.dashBoard.selected = map[int]bool{}
		a.dashLanes.selected = map[int]bool{}
	default:
		if l := a.activeList(); l != nil {
			l.clearSelection()
		}
	}
}

// lookup finds an item by id in any loaded list, or among the Dashboard's
// data (myItems and the last ChildrenOf fetch for "my PBIs").
func (a *App) lookup(id int) *model.WorkItem {
	for _, l := range []*list{a.sprint, a.backlog} {
		if l.tree != nil {
			if n, ok := l.tree.Get(id); ok {
				return n.Item
			}
		}
	}
	for _, it := range a.myItems {
		if it.ID == id {
			return it
		}
	}
	for _, children := range a.dashChildren {
		for _, c := range children {
			if c.ID == id {
				return c
			}
		}
	}
	return nil
}

func (a *App) applyUpdate(it *model.WorkItem) {
	a.sprint.apply(it)
	a.backlog.apply(it)
	for i, mi := range a.myItems {
		if mi.ID == it.ID {
			a.myItems[i] = it
		}
	}
	for _, children := range a.dashChildren {
		for i, c := range children {
			if c.ID == it.ID {
				children[i] = it
			}
		}
	}
	a.board.setItems(a.currentBoard(), a.sprint.all, a.ctx.Backlog, a.include())
	a.refreshDashboard()
	// The drill-down can hold items no list has (children fetched for it),
	// so swap those pointers too or they keep a superseded revision.
	if a.item != nil {
		a.item.apply(it)
		for i, s := range a.itemStack {
			if s.ID == it.ID {
				a.itemStack[i] = it
			}
		}
	}
	a.syncItemView()
	a.refreshDetail()
}

// fresh re-resolves an item by id. A popup that stays open across a write
// (the description editor) captured the item as it was when it opened; the
// next write from it needs the revision the last one produced.
func (a *App) fresh(it *model.WorkItem) *model.WorkItem {
	if f := a.lookup(it.ID); f != nil {
		return f
	}
	if a.item != nil {
		if a.item.item.ID == it.ID {
			return a.item.item
		}
		for _, c := range a.item.children {
			if c.ID == it.ID {
				return c
			}
		}
	}
	return it
}

// childItems returns the direct children of id across the loaded lists and
// the Dashboard's data, sorted by kind then id.
func (a *App) childItems(id int) []*model.WorkItem {
	seen := map[int]bool{}
	var out []*model.WorkItem
	for _, l := range []*list{a.sprint, a.backlog} {
		for _, it := range l.all {
			if it.ParentID == id && !seen[it.ID] {
				seen[it.ID] = true
				out = append(out, it)
			}
		}
	}
	for _, it := range a.myItems {
		if it.ParentID == id && !seen[it.ID] {
			seen[it.ID] = true
			out = append(out, it)
		}
	}
	for _, c := range a.dashChildren[id] {
		if !seen[c.ID] {
			seen[c.ID] = true
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (a *App) refreshDetail() {
	if a.w == 0 {
		return // no window size yet; the WindowSizeMsg handler calls back in
	}
	it := a.currentItem()
	var parent *model.WorkItem
	var children []*model.WorkItem
	var comments []model.Comment
	if it != nil {
		parent = a.lookup(it.ParentID)
		children = a.childItems(it.ID)
		// Only the Board, Sprint and Backlog previews show the discussion:
		// they are the panes tall enough (full terminal height) to fit it
		// alongside the children and description without the rest becoming
		// unreadable.
		if a.view == viewBoard || a.view == viewSprint || a.view == viewBacklog {
			comments = a.comments[it.ID]
		}
	}
	w := a.detailWidth()
	if w == 0 {
		w = a.w - 4
	}
	height := a.bodyHeight() - 2
	if a.view == viewDash {
		topH, _ := a.dashLayout()
		height = topH - 2
	}
	a.detail.Width = w
	a.detail.Height = height
	a.detail.SetContent(renderDetail(it, parent, children, comments, w, a.ctx.Backlog))
	a.detail.GotoTop()
}

// loadPreviewComments fetches the discussion for the currently selected
// item on the Board, Sprint or Backlog, for the preview pane. A no-op on
// other views. force bypasses the cache, for an explicit refresh.
func (a *App) loadPreviewComments(force bool) tea.Cmd {
	if a.view != viewBoard && a.view != viewSprint && a.view != viewBacklog {
		return nil
	}
	if it := a.currentItem(); it != nil {
		return a.loadComments(it.ID, force)
	}
	return nil
}

func (a *App) openBrowser() tea.Cmd {
	it := a.currentItem()
	if it == nil || it.URL == "" {
		return nil
	}
	cmd := "xdg-open"
	if runtime.GOOS == "darwin" {
		cmd = "open"
	}
	_ = exec.Command(cmd, it.URL).Start()
	return a.setFlash("opened #"+strconv.Itoa(it.ID), false)
}

func (a *App) yank() tea.Cmd {
	it := a.currentItem()
	if it == nil {
		return nil
	}
	text := strconv.Itoa(it.ID)
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("pbcopy")
	default:
		if _, err := exec.LookPath("wl-copy"); err == nil {
			c = exec.Command("wl-copy")
		} else {
			c = exec.Command("xclip", "-selection", "clipboard")
		}
	}
	c.Stdin = strings.NewReader(text)
	if err := c.Run(); err != nil {
		return a.setFlash("clipboard unavailable: "+text, true)
	}
	return a.setFlash("yanked "+text, false)
}

func (a *App) showItem(id int) tea.Cmd {
	if it := a.lookup(id); it != nil {
		if l := a.activeList(); l != nil {
			l.jumpTo(id)
			a.refreshDetail()
			return nil
		}
	}
	return func() tea.Msg {
		it, err := a.client.Get(context.Background(), id)
		if err != nil {
			return errMsg{err}
		}
		return popupMsg{&report{title: fmt.Sprintf("#%d", id), lines: strings.Split(renderDetail(it, nil, nil, nil, 70, a.ctx.Backlog), "\n")}}
	}
}

// ------------------------------------------------------------ view

// ------------------------------------------------------------ item drill-down

type itemLoadedMsg struct {
	id       int
	children []*model.WorkItem
	states   []string
	err      error
}

// openItem drills into a work item. The locally known children show at
// once; a fetch then fills in anything the current view has not loaded.
func (a *App) openItem(it *model.WorkItem) tea.Cmd {
	if it == nil {
		return nil
	}
	if a.view != viewItem {
		a.itemReturn = a.view
		a.itemStack = nil
	}
	a.itemStack = append(a.itemStack, it)
	a.view = viewItem
	a.focusDetail = false
	return a.showItemView(it)
}

func (a *App) showItemView(it *model.WorkItem) tea.Cmd {
	v := newItemView(it, a.ctx.Backlog)
	v.parent = a.lookup(it.ParentID)
	v.focusKan = len(a.childItems(it.ID)) > 0
	v.setChildren(a.childItems(it.ID), nil)
	if c, ok := a.comments[it.ID]; ok {
		v.setComments(c)
	}
	a.item = v
	return tea.Batch(a.loadItemChildren(it), a.loadComments(it.ID, false))
}

func (a *App) loadItemChildren(it *model.WorkItem) tea.Cmd {
	project, id, cfg := a.ctx.Project, it.ID, a.ctx.Backlog
	known := a.childItems(id)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		children, err := a.client.Children(ctx, project, id)
		if err != nil {
			return itemLoadedMsg{id: id, err: err}
		}
		// Column order comes from the type most of the children are, so a
		// task kanban reads To Do → In Progress → Done.
		typ := dominantType(children)
		if typ == "" {
			typ = dominantType(known)
		}
		if typ == "" {
			typ = cfg.ChildType(&model.WorkItem{Kind: it.Kind, Type: it.Type})
		}
		var states []string
		if typ != "" {
			states, _ = a.client.States(ctx, project, typ)
		}
		return itemLoadedMsg{id: id, children: children, states: states}
	}
}

type commentsLoadedMsg struct {
	id       int
	comments []model.Comment
	since    int // a.commentsGen[id] when the fetch was dispatched
	err      error
}

// loadComments fetches an item's discussion, caching by id so a repeat
// visit (or a fast cursor across Board cards) doesn't refetch or pile up
// duplicate in-flight requests. force bypasses the cache, for an explicit
// refresh.
func (a *App) loadComments(id int, force bool) tea.Cmd {
	if !force {
		if _, ok := a.comments[id]; ok {
			return nil
		}
	}
	if a.commentsLoading[id] {
		return nil
	}
	a.commentsLoading[id] = true
	project, since := a.ctx.Project, a.commentsGen[id]
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		comments, err := a.client.Comments(ctx, project, id)
		return commentsLoadedMsg{id: id, comments: comments, since: since, err: err}
	}
}

// dominantType is the work item type most of the items share.
func dominantType(items []*model.WorkItem) string {
	counts := map[string]int{}
	best, bestN := "", 0
	for _, it := range items {
		counts[it.Type]++
		if n := counts[it.Type]; n > bestN {
			best, bestN = it.Type, n
		}
	}
	return best
}

// closeItem walks one step back out of the drill-down.
func (a *App) closeItem() tea.Cmd {
	if len(a.itemStack) > 1 {
		a.itemStack = a.itemStack[:len(a.itemStack)-1]
		return a.showItemView(a.itemStack[len(a.itemStack)-1])
	}
	a.itemStack = nil
	a.item = nil
	return a.switchView(a.itemReturn)
}

// syncItemView refreshes the drill-down from the loaded lists after a write.
func (a *App) syncItemView() {
	if a.view != viewItem || a.item == nil {
		return
	}
	if fresh := a.lookup(a.item.item.ID); fresh != nil {
		a.item.item = fresh
		a.itemStack[len(a.itemStack)-1] = fresh
	}
	byID := map[int]*model.WorkItem{}
	for _, c := range a.childItems(a.item.item.ID) {
		byID[c.ID] = c
	}
	merged := make([]*model.WorkItem, 0, len(a.item.children))
	seen := map[int]bool{}
	for _, c := range a.item.children {
		seen[c.ID] = true
		if fresh, ok := byID[c.ID]; ok {
			merged = append(merged, fresh)
		} else {
			merged = append(merged, c)
		}
	}
	for _, c := range byID {
		if !seen[c.ID] {
			merged = append(merged, c)
		}
	}
	sortItems(merged)
	a.item.setChildren(merged, nil)
	a.item.parent = a.lookup(a.item.item.ParentID)
}

func sortItems(items []*model.WorkItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		return items[i].ID < items[j].ID
	})
}

// itemOverride handles the keys the drill-down redefines. It runs before
// the global bindings; everything it does not claim (tabs, :, ?, o, y…)
// keeps its usual meaning.
func (a *App) itemOverride(msg tea.KeyMsg) (tea.Cmd, bool) {
	v := a.item
	switch {
	case key.Matches(msg, keys.Back):
		return a.closeItem(), true
	case key.Matches(msg, keys.Focus):
		if !v.descOnly {
			v.focusKan = !v.focusKan
		}
		return nil, true
	case key.Matches(msg, keys.Preview):
		v.descOnly = !v.descOnly
		if v.descOnly {
			v.focusKan = false // the kanban is gone; focus follows
		}
		return nil, true
	case key.Matches(msg, keys.Comments):
		v.showComments = !v.showComments
		return nil, true
	case key.Matches(msg, keys.Comment):
		return a.addComment(v.current()), true
	case key.Matches(msg, keys.Refresh):
		v.loading = true
		v.commentsLoading = true
		return tea.Batch(a.loadItemChildren(v.item), a.loadComments(v.item.ID, true)), true
	case key.Matches(msg, keys.Details), msg.String() == "enter":
		if c := v.currentChild(); v.focusKan && c != nil {
			return a.openItem(c), true
		}
		return nil, true
	}
	return nil, false
}

func (a *App) onItemKey(msg tea.KeyMsg) tea.Cmd {
	v := a.item
	if !v.focusKan {
		switch {
		case key.Matches(msg, keys.Down):
			v.desc.LineDown(1)
			return nil
		case key.Matches(msg, keys.Up):
			v.desc.LineUp(1)
			return nil
		case key.Matches(msg, keys.PreviewDown):
			v.desc.HalfViewDown()
			return nil
		case key.Matches(msg, keys.PreviewUp):
			v.desc.HalfViewUp()
			return nil
		case key.Matches(msg, keys.Top):
			v.desc.GotoTop()
			return nil
		case key.Matches(msg, keys.Bottom):
			v.desc.GotoBottom()
			return nil
		}
		return a.onActionKey(msg)
	}
	switch {
	case key.Matches(msg, keys.Down):
		v.move(0, 1)
	case key.Matches(msg, keys.Up):
		v.move(0, -1)
	case key.Matches(msg, keys.Left):
		v.move(-1, 0)
	case key.Matches(msg, keys.Right):
		v.move(1, 0)
	case key.Matches(msg, keys.Top):
		v.row = 0
		v.clamp()
	case key.Matches(msg, keys.Bottom):
		v.move(0, 1<<20)
	case key.Matches(msg, keys.ColLeft):
		return a.moveChildState(-1)
	case key.Matches(msg, keys.ColRight):
		return a.moveChildState(1)
	default:
		return a.onActionKey(msg)
	}
	return nil
}

// moveChildState moves the highlighted child to the neighbouring column.
func (a *App) moveChildState(dc int) tea.Cmd {
	c := a.item.currentChild()
	if c == nil {
		return nil
	}
	state, ok := a.item.adjacentState(dc)
	if !ok {
		return nil
	}
	return a.applyPatch([]*model.WorkItem{c}, "Move to "+state, model.Patch{Field: model.FieldState, Value: state})
}

// bodyHeight is the rows left for the panels once the two header lines and
// the one footer line are taken.
func (a *App) bodyHeight() int { return max(a.h-3, 1) }

// previewMaxW caps the Board and Dashboard preview pane. It used to be 60,
// which on a full-screen terminal wrapped descriptions at ~56 characters next
// to columns with nothing in them.
const previewMaxW = 100

// detailWidth is the width of the side preview pane, 0 when hidden (toggled
// off with z, or the terminal is too narrow).
func (a *App) detailWidth() int {
	if a.w < 110 || a.view == viewItem {
		return 0 // the drill-down lays out its own panes
	}
	if a.view == viewBoard {
		if !a.previewBoard {
			return 0
		}
		return min(a.w/3, previewMaxW)
	}
	if a.view == viewDash {
		if !a.previewDash {
			return 0
		}
		return min(a.w/3, previewMaxW)
	}
	if !a.previewList {
		return 0
	}
	return a.w*2/5 - 2
}

func (a *App) View() string {
	if a.w == 0 {
		return "loading…"
	}
	header := a.renderHeader()
	body := a.renderBody()
	footer := a.renderFooter()
	screen := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	if a.popup != nil {
		screen = overlay(screen, a.popup.View(a.w, a.h), a.w, a.h)
	}
	return screen
}

func (a *App) renderHeader() string {
	sep := sCrumbSep.Render(" › ")
	crumbs := []string{sCrumb.Render(orgName(a.ctx.Org))}
	if a.ctx.Project != "" {
		crumbs = append(crumbs, sCrumb.Render(a.ctx.Project))
	}
	if a.ctx.Team != "" {
		crumb := sCrumb.Render(a.ctx.Team)
		if a.ctx.FilterTeam != "" {
			crumb += sSelected.Render(" ⌕ " + a.ctx.FilterTeam)
		}
		crumbs = append(crumbs, crumb)
	}
	if a.ctx.Iteration.Path != "" {
		s := sHeader.Render(a.ctx.Iteration.Name)
		if !a.ctx.Iteration.Start.IsZero() {
			s += sMuted.Render(" " + a.ctx.Iteration.Start.Format("Jan 2") + " – " + a.ctx.Iteration.Finish.Format("Jan 2"))
		}
		crumbs = append(crumbs, s)
	}
	left := sHeader.Render("devopstui") + "  " + strings.Join(crumbs, sep)
	right := ""
	if a.busy != "" {
		right = a.spin.View() + " " + a.busy
	} else if a.isLoading() {
		right = a.spin.View() + " loading"
	} else if !a.lastLoad.IsZero() {
		right = sMuted.Render("⟳ " + ago(a.lastLoad))
	}
	if a.cfg.RefreshSeconds > 0 {
		right += sOK.Render(fmt.Sprintf(" ↻%ds", a.cfg.RefreshSeconds))
	}
	if a.me != "" {
		right = sMuted.Render(a.me+"  ") + right
	}
	line1 := pad(left, a.w-lipgloss.Width(right)-2) + "  " + right // a truncated crumb never touches the name

	var tabs []string
	for _, v := range tabViews {
		label := fmt.Sprintf("%d %s", int(v)+1, v)
		if v == a.view {
			tabs = append(tabs, sTabActive.Render(label))
		} else {
			tabs = append(tabs, sTab.Render(label))
		}
	}
	summary := ""
	switch {
	case a.view == viewItem && a.item != nil:
		var trail []string
		for _, it := range a.itemStack {
			trail = append(trail, sMuted.Render(fmt.Sprintf("#%d", it.ID)))
		}
		tabs = append(tabs, sCrumbSep.Render("▸ ")+strings.Join(trail, sCrumbSep.Render(" ▸ ")))
		focus := strings.ToLower(narrativeTitle(a.item.item, narrativeSections(a.item.item)))
		if a.item.focusKan {
			focus = fmt.Sprintf("children %d/%d", a.item.row+1, len(a.item.children))
		}
		summary = sMuted.Render(focus)
	case a.view == viewBoard:
		name := a.currentBoard().Name
		if h := a.board.scrollHint(); h != "" {
			name += " · " + h
		}
		summary = sMuted.Render(name)
	case a.view == viewDash:
		// The counts are on the summary line right below; this just says
		// which half has the keys.
		focus := "kanban"
		if a.dashFocusLanes {
			focus = "lanes"
		}
		if h := a.dashBoard.scrollHint(); h != "" && !a.dashFocusLanes {
			focus += " · " + h
		}
		summary = sMuted.Render(focus)
	default:
		if l := a.activeList(); l != nil {
			summary = l.summary()
		}
	}
	line2 := pad(" "+strings.Join(tabs, "   "), a.w-lipgloss.Width(summary)-1) + summary
	return line1 + "\n" + line2
}

func orgName(url string) string {
	url = strings.TrimSuffix(url, "/")
	if i := strings.LastIndex(url, "/"); i >= 0 {
		return url[i+1:]
	}
	if url == "" {
		return "demo"
	}
	return url
}

func (a *App) isLoading() bool {
	for _, v := range a.loading {
		if v {
			return true
		}
	}
	return false
}

func (a *App) renderBody() string {
	h := a.bodyHeight()
	dw := a.detailWidth()
	listStyle := sPanel
	detailStyle := sPanel
	if a.focusDetail {
		detailStyle = sPanelFocus
	} else {
		listStyle = sPanelFocus
	}

	if a.view == viewItem && a.item != nil {
		return a.item.view(a.w, h, a.spin.View())
	}
	if a.focusDetail && dw == 0 {
		return detailStyle.Width(a.w - 2).Height(h - 2).Render(a.detail.View())
	}
	if a.view == viewBoard {
		if dw == 0 {
			return a.board.view(a.w, h, true)
		}
		left := a.board.view(a.w-dw-2, h, !a.focusDetail)
		right := detailStyle.Width(dw).Height(h - 2).Render(a.detail.View())
		return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}
	if a.view == viewDash {
		return a.renderDash(a.w, h)
	}
	l := a.activeList()
	lw := a.w - 2
	if dw > 0 {
		lw = a.w - dw - 4
	}
	content := l.view(lw, h-2)
	left := listStyle.Width(lw).Height(h - 2).Render(content)
	if dw == 0 {
		return left
	}
	right := detailStyle.Width(dw).Height(h - 2).Render(a.detail.View())
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

// renderDash lays out the Dashboard's two rows: a kanban of the PBIs
// assigned to @Me on top, and swimlanes of their active subitems below.
func (a *App) renderDash(w, h int) string {
	summary := a.dashSummary(w)
	topH, botH := a.dashLayout()

	dw := a.detailWidth()
	var top string
	if dw > 0 {
		// The preview is never focusable on the Dashboard, so it never
		// takes the focused-panel style.
		right := sPanel.Width(dw).Height(topH - 2).Render(a.detail.View())
		left := a.dashBoard.view(w-dw-2, topH, !a.dashFocusLanes)
		top = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	} else {
		top = a.dashBoard.view(w, topH, !a.dashFocusLanes)
	}
	bottom := a.dashLanes.view(w, botH, a.dashFocusLanes)
	return summary + "\n" + top + "\n" + bottom
}

func (a *App) dashSummary(width int) string {
	pbis := 0
	for _, it := range a.myPBIs() {
		if a.dashShowDone || !isDone(it.State) {
			pbis++
		}
	}
	children := 0
	for _, l := range a.dashLanes.ls {
		for _, col := range l.cols {
			children += len(col)
		}
	}
	count := func(n int, noun string) string {
		p := plural(n, noun)
		i := strings.IndexByte(p, ' ')
		return sKey.Render(p[:i]) + p[i:]
	}
	s := count(pbis, "PBI") + " assigned to you · " + count(children, "work item") + " across " + count(len(a.dashLanes.ls), "lane")
	return pad(s, width)
}

func (a *App) renderFooter() string {
	if a.cmdActive {
		return a.cmd.input.View()
	}
	if l := a.activeList(); l != nil && l.filtering {
		return l.filter.View() + sMuted.Render("   enter keep · esc clear")
	}
	bindings := footerTree
	switch {
	case a.view == viewItem && a.item != nil:
		bindings = footerItemDesc
		if a.item.focusKan {
			bindings = footerItemKanban
		}
	case a.view == viewBoard:
		bindings = footerBoard
	case a.view == viewDash:
		bindings = footerDashKanban
		if a.dashFocusLanes {
			bindings = footerDashLanes
		}
	}
	if a.focusDetail {
		bindings = []key.Binding{keys.Up, keys.Down, keys.Focus, keys.Edit, keys.Open}
	}
	msg := ""
	if a.flash != "" {
		if a.flashErr {
			msg = sErr.Render(trunc(a.flash, a.w/2))
		} else {
			msg = sOK.Render(trunc(a.flash, a.w/2))
		}
	}
	avail := a.w - 1
	if msg != "" {
		avail -= lipgloss.Width(msg) + 3 // keep the flash clear of the hints
	}
	return pad(footerHints(bindings, avail), a.w-lipgloss.Width(msg)-1) + msg
}

// footerHints renders as many whole key hints as fit in width, in order.
// When some don't fit, the last slot goes to "? more" so the rest are one
// key away, instead of cutting a hint off mid-word.
func footerHints(bindings []key.Binding, width int) string {
	hint := func(k, desc string) string { return sKey.Render(k) + " " + sMuted.Render(desc) }
	var parts []string
	for _, kb := range bindings {
		parts = append(parts, hint(kb.Help().Key, kb.Help().Desc))
	}
	if all := strings.Join(parts, "  "); lipgloss.Width(all) <= width {
		return all
	}
	more := hint(keys.Help.Help().Key, "more")
	out := ""
	for _, p := range parts {
		next := p
		if out != "" {
			next = out + "  " + p
		}
		if lipgloss.Width(next)+2+lipgloss.Width(more) > width {
			break
		}
		out = next
	}
	if out == "" {
		return trunc(more, width)
	}
	return out + "  " + more
}
