// Package ui is the Bubble Tea front end.
package ui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/sync/errgroup"

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
	viewTeam
	// viewChanges is what was done to work items over a period.
	viewChanges
	// viewItem is the full-screen view of one work item. It is not a tab:
	// you drill into it with D and leave it with esc.
	viewItem
)

func (v viewID) String() string {
	return [...]string{"Dashboard", "Sprint", "Board", "Backlog", "Team", "Changes", "Item"}[v]
}

// tabViews are the numbered views shown in the header.
var tabViews = []viewID{viewDash, viewSprint, viewBoard, viewBacklog, viewTeam, viewChanges}

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
	// team is the Board's items grouped by assignee, for stand-ups.
	team *team
	// changes is the feed of what was done over a period. changesReq
	// guards its fetch against a slower, superseded response.
	changes    *changes
	changesReq int

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
	// itemPanes[i] is how level i was showing when the next one was opened.
	item         *itemView
	itemStack    []*model.WorkItem
	itemPanes    []itemPane
	itemReturn   viewID
	detail       viewport.Model
	focusDetail  bool
	previewList  bool // detail pane beside list views
	previewBoard bool // detail pane beside the board
	previewTeam  bool // detail pane beside the Team view
	previewChg   bool // detail pane beside the Changes view

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

	spin    spinner.Model
	loading map[viewID]bool
	// loadGen counts the loads started per data set, so a response is
	// applied only if nothing newer was asked for since: switching sprint
	// twice in a row must not end on whichever answer happens to be slower.
	loadGen  map[viewID]int
	busy     string // label for an in-flight write
	lastLoad time.Time

	// ctxReq and filterReq do the same for the context (project, team and
	// their settings) and the team filter. ctxLoading and filterLoading
	// drive the header spinner; ctxLoaded is set once a context has landed,
	// so a refresh knows whether there is anything to refresh yet.
	ctxReq, filterReq         int
	ctxLoading, filterLoading bool
	ctxLoaded                 bool
	// refreshGen identifies the live auto-refresh timer chain;
	// refreshEvery remembers the interval across an off/on toggle.
	refreshGen   int
	refreshEvery int

	flash    string
	flashErr bool

	pendingJump int // item to select once the next load lands (after create)

	// jumps is the ctrl+o/ctrl+n history. jumpIdx == len(jumps) means the
	// current position is newer than every entry and not recorded yet.
	jumps   []jump
	jumpIdx int
	// pendingGoto is set by the g leader; the next key completes it.
	pendingGoto bool

	// health holds the staleness threshold every view flags items by.
	health *health
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
		team:      newTeam(),
		changes:   newChanges(),
		dashBoard: newBoard(),
		dashLanes: newLanes(),
		spin:      sp,
		loading:   map[viewID]bool{},
		loadGen:   map[viewID]int{},
		cmd:       newCmdbar(),
		view:      viewSprint,

		comments:        map[int][]model.Comment{},
		commentsLoading: map[int]bool{},
		commentsGen:     map[int]int{},

		previewList:  true,
		previewBoard: true,
		previewTeam:  true,
		previewChg:   true,
		previewDash:  true,

		health: &health{staleAfter: cfg.StaleAfter()},
	}
	a.wireLists()
	a.sprint.showDone = !cfg.HideDone
	a.ctx.Org = cfg.Org
	a.ctx.Project = cfg.Project
	a.ctx.Team = cfg.Team
	a.ctx.FilterTeam = cfg.FilterTeam
	a.refreshEvery = cfg.RefreshSeconds
	a.dashShowDone = cfg.DashShowDone
	a.board.list = cfg.BoardList
	a.team.showDone = cfg.TeamShowDone
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
	a.rebucket(a.sprint.all)
	a.refreshDashboard()
	a.refreshDetail()
}

// rebucket rebuilds the Board and the Team view, the two views that lay the
// sprint's items out by board column, from items.
func (a *App) rebucket(items []*model.WorkItem) {
	def, inc := a.currentBoard(), a.include()
	a.board.setItems(def, items, a.ctx.Backlog, inc)
	a.team.setItems(def, items, a.ctx.Backlog, inc)
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
	a.dashLanes.sprint = a.ctx.Iteration.Path
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
	req   int
	team  string
	areas []model.TeamArea
	err   error
}

// setTeamFilter resolves the team's areas and applies the filter; an empty
// team clears it. Either way it supersedes a filter still being resolved,
// so picking one team and then another never ends on the first.
func (a *App) setTeamFilter(team string) tea.Cmd {
	a.filterReq++
	if team == "" {
		a.filterLoading = false
		a.ctx.FilterTeam, a.ctx.FilterAreas = "", nil
		a.applyTeamFilter()
		a.persist()
		return tea.Batch(a.reloadFiltered(), a.setFlash("team filter off", false))
	}
	a.filterLoading = true
	req, project := a.filterReq, a.ctx.Project
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		areas, err := a.client.TeamAreas(ctx, project, team)
		return teamFilterMsg{req: req, team: team, areas: areas, err: err}
	}
}

// filterUnresolved reports a filter team whose area paths have not been
// read: it is named in the config, but the call for its areas failed. The
// views show everything until a refresh gets through.
func (a *App) filterUnresolved() bool {
	return a.ctx.FilterTeam != "" && len(a.ctx.FilterAreas) == 0
}

// filterAreas is the team filter as the backend takes it, nil when off.
func (a *App) filterAreas() []model.TeamArea {
	if a.ctx.FilterTeam == "" {
		return nil
	}
	return a.ctx.FilterAreas
}

// reloadFiltered refetches what the backend filters by area itself, after
// the filter changed: the Backlog (too big to read whole and filter here)
// and the Changes. The Sprint, Board, Team and Dashboard filter what they
// already hold, so they have followed the change by the time this runs.
func (a *App) reloadFiltered() tea.Cmd {
	var cmds []tea.Cmd
	if a.backlog.all != nil || a.loading[viewBacklog] {
		cmds = append(cmds, a.loadView(viewBacklog))
	}
	cmds = append(cmds, a.refreshChanges(false))
	return tea.Batch(cmds...)
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
	}).selectValue(func(pi pickItem) bool { return pi.Value.(string) == a.ctx.FilterTeam })
	return nil
}

// findTeam resolves a typed team name: exactly (ignoring case), or else
// the one team whose name contains it.
func (a *App) findTeam(name string) (model.Team, bool) {
	var partial []model.Team
	for _, t := range a.teams {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
		if strings.Contains(strings.ToLower(t.Name), strings.ToLower(name)) {
			partial = append(partial, t)
		}
	}
	if len(partial) == 1 {
		return partial[0], true
	}
	return model.Team{}, false
}

// wireLists gives the lists their callbacks into the app.
func (a *App) wireLists() {
	for _, l := range []*list{a.sprint, a.backlog} {
		l.taskLevel = func(w *model.WorkItem) bool { return a.ctx.Backlog.TaskLevel(w) }
		l.health = a.health
		l.parentTitle = func(id int) string {
			if p := a.lookup(id); p != nil {
				return p.Title
			}
			return ""
		}
	}
	a.board.health, a.dashBoard.health, a.dashLanes.health = a.health, a.health, a.health
	a.team.health = a.health
	// Tasks already show in the PBI's detail preview and progress badge, so
	// the flat sprint view drops them. Backlog's query never returns
	// task-level items in the first place, flat or not.
	a.sprint.hideTasksFlat = true
}

// ------------------------------------------------------------ messages

type contextLoadedMsg struct {
	req int
	// project and team are the context this was loaded for. They only
	// become the App's context once this lands without an error.
	project, team string
	// refresh marks a reload of the context already on screen, which
	// refetches every view; a switch reloads only what the switch changed.
	refresh bool

	me                string
	projects          []model.Project
	teams             []model.Team
	iterations        []model.Iteration
	projectIterations []model.Iteration
	boards            []model.Board
	backlog           model.BacklogConfig
	// filterTeam is the team filter asked for, with its areas or the
	// reason they could not be read.
	filterTeam string
	filter     []model.TeamArea
	filterErr  error
	err        error
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

// changesLoadedMsg carries a Changes fetch. req ties it to the request, as
// dashLanesLoadedMsg does; key is the query it answers.
type changesLoadedMsg struct {
	req int
	key string
	set model.ChangeSet
	err error
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

// loadContext (re)loads the context the App is in.
func (a *App) loadContext() tea.Cmd {
	return a.loadContextFor(a.ctx.Project, a.ctx.Team, true)
}

// switchContext loads another project or team. The App stays where it is
// until that lands: a switch that fails must not leave the header naming
// one team over another team's sprints and board.
func (a *App) switchContext(project, team string) tea.Cmd {
	return a.loadContextFor(project, team, false)
}

func (a *App) loadContextFor(project, team string, refresh bool) tea.Cmd {
	a.ctxReq++
	a.ctxLoading = true
	req := a.ctxReq
	filterTeam := a.ctx.FilterTeam
	if project != a.ctx.Project {
		filterTeam = "" // a team filter does not carry over to another project
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// The calls are independent, so run them side by side; startup
		// otherwise waits on each round trip in turn. Any error discards
		// the whole result (see onContextLoaded).
		m := contextLoadedMsg{req: req, project: project, team: team, refresh: refresh, filterTeam: filterTeam}
		g, gctx := errgroup.WithContext(ctx)
		g.Go(func() (err error) { m.me, err = a.client.Me(gctx); return })
		g.Go(func() (err error) { m.projects, err = a.client.Projects(gctx); return })
		if project != "" {
			g.Go(func() (err error) { m.teams, err = a.client.Teams(gctx, project); return })
			if filterTeam != "" {
				// Best effort: a bad filter team must not block startup.
				g.Go(func() error { m.filter, m.filterErr = a.client.TeamAreas(gctx, project, filterTeam); return nil })
			}
		}
		if project != "" && team != "" {
			g.Go(func() (err error) { m.iterations, err = a.client.Iterations(gctx, project, team); return })
			// Best effort: a project-wide read can fail on permissions the team
			// iteration read doesn't need, and must not block startup.
			g.Go(func() error { m.projectIterations, _ = a.client.ProjectIterations(gctx, project); return nil })
			g.Go(func() (err error) { m.boards, err = a.client.Boards(gctx, project, team); return })
			g.Go(func() (err error) { m.backlog, err = a.client.BacklogConfig(gctx, project, team); return })
		}
		m.err = g.Wait()
		return m
	}
}

// loadTarget is the data set a view shows: the Sprint, Board and Team
// views are three layouts of the sprint's items.
func loadTarget(v viewID) viewID {
	switch v {
	case viewBoard, viewTeam:
		return viewSprint
	}
	return v
}

func (a *App) loadView(v viewID) tea.Cmd {
	if a.ctx.Project == "" || a.ctx.Team == "" {
		return nil
	}
	v = loadTarget(v)
	if v == viewChanges {
		return a.loadChanges()
	}
	if v == viewItem {
		return nil // the drill-down refreshes through the lists behind it
	}
	if v == viewSprint && a.ctx.Iteration.Path == "" {
		// Never query with an empty path; the server rejects it and the
		// message is cryptic. Tell the user what is missing instead.
		if len(a.iterations) == 0 {
			return a.setFlash(fmt.Sprintf("team %q has no sprints; add iterations to the team in Azure DevOps", a.ctx.Team), true)
		}
		return a.setFlash("no sprint selected: pick one with :sprint", true)
	}
	a.loadGen[v]++
	gen := a.loadGen[v]
	a.loading[v] = true
	c := a.ctx
	areas := a.filterAreas()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		m := itemsLoadedMsg{view: v, gen: gen}
		switch v {
		case viewSprint:
			if c.Iteration.Path == c.Project {
				m.items, m.external, m.err = a.client.Unscheduled(ctx, c.Project, c.Team)
			} else {
				m.items, m.external, m.err = a.client.SprintItems(ctx, c.Project, c.Team, c.Iteration.Path)
			}
			if m.err != nil {
				m.err = fmt.Errorf("sprint %q: %w", c.Iteration.Path, m.err)
			}
		case viewBacklog:
			m.items, m.external, m.err = a.client.Backlog(ctx, c.Project, c.Team, areas)
		case viewDash:
			m.items, m.err = a.client.MyItems(ctx, c.Project)
		}
		return m
	}
}

// changesQuery is what the Changes view asks for right now: its period,
// the selected sprint's items unless widened to all sprints, and the team
// filter.
func (a *App) changesQuery() model.ChangeQuery {
	t := a.changes
	q := model.ChangeQuery{Areas: a.filterAreas()}
	q.Since, q.Until = t.period.window(t.now(), a.ctx.Iteration)
	if !t.allSprints {
		q.IterationPath = a.ctx.Iteration.Path
	}
	return q
}

// changesKey identifies a query, so the view can tell whether what it
// holds still answers what it would ask.
func changesKey(project string, q model.ChangeQuery) string {
	return fmt.Sprintf("%s|%d|%d|%s|%v", project, q.Since.Unix(), q.Until.Unix(), q.IterationPath, q.Areas)
}

func (a *App) loadChanges() tea.Cmd {
	a.changesReq++
	req := a.changesReq
	a.loading[viewChanges] = true
	project, q := a.ctx.Project, a.changesQuery()
	key := changesKey(project, q)
	return func() tea.Msg {
		// Longer than the other loads: a sprint's worth of changes reads
		// one history per changed item.
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		set, err := a.client.Changes(ctx, project, q)
		return changesLoadedMsg{req: req, key: key, set: set, err: err}
	}
}

// refreshChanges reloads the Changes view when what it holds no longer
// answers its query, or always with force (after a write, which is itself
// a change). Off screen it only marks the data stale; switching to the
// view loads it then.
func (a *App) refreshChanges(force bool) tea.Cmd {
	if !a.changes.loaded && !a.loading[viewChanges] {
		return nil
	}
	if force {
		a.changes.key = ""
	}
	if a.view != viewChanges && !(a.view == viewItem && a.itemReturn == viewChanges) {
		return nil
	}
	if a.changes.key == changesKey(a.ctx.Project, a.changesQuery()) && !a.loading[viewChanges] {
		return nil
	}
	return a.loadView(viewChanges)
}

func (a *App) reloadAll() tea.Cmd {
	cmds := []tea.Cmd{a.loadView(viewSprint), a.loadView(viewDash), a.loadView(viewBacklog)}
	if a.changes.loaded || a.view == viewChanges {
		cmds = append(cmds, a.loadView(viewChanges))
	}
	return tea.Batch(cmds...)
}

// refreshEverything is :refresh: settings as well as items. The backend
// forgets what it cached, so a new sprint, team or board column shows up
// without a restart.
func (a *App) refreshEverything() tea.Cmd {
	if c, ok := a.client.(interface{ Forget() }); ok {
		c.Forget()
	}
	return a.loadContext()
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
	m, cmd := a.handle(msg)
	// Mentions come from the item's text and its discussion, which many
	// messages can change (a load, a save, a posted comment); re-scanning
	// after each is cheaper than remembering every path that edits them.
	// Spinner ticks never do, and arrive many times a second.
	if _, tick := msg.(spinner.TickMsg); !tick {
		if mcmd := a.resolveMentions(); mcmd != nil {
			cmd = tea.Batch(cmd, mcmd)
		}
	}
	return m, cmd
}

func (a *App) handle(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		if msg.gen != a.loadGen[msg.view] {
			return a, nil // a newer load of the same data is on its way
		}
		a.loading[msg.view] = false
		if msg.err != nil {
			return a, a.setFlash(msg.err.Error(), true)
		}
		a.lastLoad = time.Now()
		var cmd tea.Cmd
		switch msg.view {
		case viewSprint:
			a.sprint.setItems(msg.items, msg.external)
			a.rebucket(msg.items)
		case viewBacklog:
			a.backlog.setItems(msg.items, msg.external)
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
			} else if a.view == viewTeam {
				a.team.jumpTo(a.pendingJump)
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

	case changesLoadedMsg:
		if msg.req != a.changesReq {
			return a, nil // superseded by a newer fetch
		}
		a.loading[viewChanges] = false
		if msg.err != nil {
			return a, a.setFlash("changes: "+msg.err.Error(), true)
		}
		a.lastLoad = time.Now()
		a.changes.setData(msg.set, msg.key)
		if a.pendingJump != 0 && a.view == viewChanges {
			a.changes.jumpTo(a.pendingJump)
			a.pendingJump = 0
		}
		a.refreshDetail()
		return a, nil

	case dashLanesLoadedMsg:
		if msg.req != a.dashLanesReq { // superseded by a newer fetch
			return a, nil
		}
		if msg.err != nil {
			return a, a.setFlash("active work: "+msg.err.Error(), true)
		}
		a.dashChildren = msg.children
		a.dashLanes.sprint = a.ctx.Iteration.Path
		a.dashLanes.setLanes(a.dashLaneParents(), a.dashChildren, msg.states)
		return a, nil

	case itemUpdatedMsg:
		a.busy = ""
		a.reportSave(msg.err)
		if msg.err != nil {
			return a, a.setFlash(msg.err.Error(), true)
		}
		a.applyUpdate(msg.item)
		return a, tea.Batch(a.refreshChanges(true), a.setFlash(fmt.Sprintf("saved #%d", msg.item.ID), false))

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
		return a, tea.Batch(a.refreshChanges(true), a.setFlash(fmt.Sprintf("commented on #%d", msg.id), false))

	case createdMsg:
		a.busy = ""
		if msg.err != nil {
			return a, a.setFlash(msg.err.Error(), true)
		}
		flash := a.setFlash(fmt.Sprintf("created %s #%d", msg.item.Type, msg.item.ID), false)
		if a.view == viewItem && a.item != nil {
			// Show it in the kanban straight away, then reconcile.
			a.item.setChildren(append(a.item.children, msg.item), nil)
			a.item.focusKan, a.item.focusRel = true, false
			a.item.jumpTo(msg.item.ID)
			return a, tea.Batch(a.reloadAll(), a.loadItemChildren(a.item.item), flash)
		}
		a.pendingJump = msg.item.ID
		return a, tea.Batch(a.reloadAll(), flash)

	case bulkDoneMsg:
		a.busy = ""
		return a, tea.Batch(a.onBulkDone(msg), a.refreshChanges(true))

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

	case itemParentMsg:
		if a.item == nil || a.item.item.ID != msg.id {
			return a, nil
		}
		if msg.err != nil {
			return a, a.setFlash("parent: "+msg.err.Error(), true)
		}
		a.item.parent = msg.parent
		return a, nil

	case linksLoadedMsg:
		if a.item == nil || a.item.item.ID != msg.id {
			return a, nil
		}
		if msg.err != nil {
			a.item.setLinks(nil)
			return a, a.setFlash("links: "+msg.err.Error(), true)
		}
		a.item.setLinks(msg.links)
		return a, nil

	case mentionsLoadedMsg:
		if a.item == nil || a.item.item.ID != msg.id || !slices.Equal(a.item.mentionIDs, msg.ids) {
			return a, nil // superseded by a newer scan
		}
		if msg.err != nil {
			return a, a.setFlash("mentions: "+msg.err.Error(), true)
		}
		a.item.setMentions(msg.ids, mentionRows(msg.ids, msg.items))
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
		if it := a.currentItem(); it != nil && it.ID == msg.id && a.view.previewsComments() {
			a.refreshDetail()
		}
		return a, nil

	case teamFilterMsg:
		if msg.req != a.filterReq {
			return a, nil // another filter was picked, or it was turned off, since
		}
		a.filterLoading = false
		if msg.err != nil {
			return a, a.setFlash("team filter: "+msg.err.Error(), true)
		}
		if len(msg.areas) == 0 {
			return a, a.setFlash(fmt.Sprintf("team %q has no area paths", msg.team), true)
		}
		a.ctx.FilterTeam, a.ctx.FilterAreas = msg.team, msg.areas
		a.applyTeamFilter()
		a.persist()
		return a, tea.Batch(a.reloadFiltered(), a.setFlash("filtering on "+msg.team, false))

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

	case openItemMsg:
		return a, a.openItem(msg.item)

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
	if a.view == viewChanges && a.changes.filtering {
		var cmd tea.Cmd
		a.changes.filter, cmd = a.changes.filter.Update(msg)
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
	if msg.req != a.ctxReq {
		return nil // another project or team was picked since
	}
	a.ctxLoading = false
	if msg.err != nil {
		if msg.project != a.ctx.Project || msg.team != a.ctx.Team {
			to := msg.team
			if to == "" {
				to = msg.project
			}
			return a.setFlash(fmt.Sprintf("could not switch to %s: %v", to, msg.err), true)
		}
		return a.setFlash(msg.err.Error(), true)
	}
	a.me = msg.me
	a.projects = msg.projects
	if msg.project == "" {
		return a.pickProject()
	}
	first := !a.ctxLoaded
	projectChanged := msg.project != a.ctx.Project
	teamChanged := projectChanged || msg.team != a.ctx.Team
	if projectChanged {
		// Nothing loaded for the old project means anything here.
		a.ctx.Project, a.ctx.Team, a.ctx.Iteration, a.ctx.Board = msg.project, "", model.Iteration{}, ""
		a.ctx.FilterTeam, a.ctx.FilterAreas = "", nil
		a.sprint, a.backlog = newList("sprint is empty"), newList("backlog is empty")
		a.board, a.team = newBoard(), newTeam()
		a.dashBoard, a.dashLanes = newBoard(), newLanes()
		a.changes.setData(model.ChangeSet{}, "")
		a.changes.loaded = false
		a.myItems, a.dashChildren = nil, nil
		a.iterations, a.projectIterations, a.boards = nil, nil, nil
		a.wireLists()
		a.sprint.showDone = !a.cfg.HideDone
		a.board.list, a.team.showDone = a.cfg.BoardList, a.cfg.TeamShowDone
	}
	a.teams = msg.teams
	if msg.team == "" {
		return a.pickTeam()
	}
	a.ctxLoaded = true
	a.ctx.Team = msg.team
	// A synthetic entry alongside the real sprints, picked the same way, so
	// the user can ask to see items with no sprint at all (project root is
	// the backlog convention already used by pickMoveTarget's "Backlog").
	a.iterations = append(append([]model.Iteration(nil), msg.iterations...), model.Iteration{Path: a.ctx.Project, Name: "Unscheduled"})
	a.projectIterations = msg.projectIterations
	a.boards = msg.boards
	a.ctx.Backlog = msg.backlog

	var cmds []tea.Cmd
	filterChanged := false
	if msg.filterTeam == a.ctx.FilterTeam && msg.filterTeam != "" {
		switch {
		case msg.filterErr != nil:
			// Keep the name: a network blip at startup must not quietly
			// drop the filter and then save the config without it.
			cmds = append(cmds, a.setFlash(fmt.Sprintf("team filter %s not applied (%v); r retries", msg.filterTeam, msg.filterErr), true))
		case len(msg.filter) == 0:
			a.ctx.FilterTeam = "" // the team has no area paths to filter on
			filterChanged = len(a.ctx.FilterAreas) > 0
			a.ctx.FilterAreas = nil
			cmds = append(cmds, a.setFlash(fmt.Sprintf("team filter off: %q has no area paths", msg.filterTeam), true))
		default:
			filterChanged = !slices.Equal(a.ctx.FilterAreas, msg.filter)
			a.ctx.FilterAreas = msg.filter
		}
	}
	a.applyTeamFilter()

	// Keep the sprint on screen across a refresh; a new team starts on its
	// own current one.
	sprintChanged := teamChanged
	if !teamChanged {
		kept := false
		for _, it := range a.iterations {
			if it.Path == a.ctx.Iteration.Path {
				a.ctx.Iteration, kept = it, true
			}
		}
		if !kept {
			a.ctx.Iteration, sprintChanged = model.Iteration{}, true
		}
	} else {
		a.ctx.Iteration = model.Iteration{}
	}
	if a.ctx.Iteration.Path == "" {
		a.ctx.Iteration = a.currentIteration()
	}
	if teamChanged {
		a.sprint.clearSelection()
		a.rebucket(a.sprint.all) // the new team's board columns
	}
	a.persist()
	switch {
	case first || projectChanged || msg.refresh:
		cmds = append(cmds, a.reloadAll())
	default:
		// Only the team changed. The Backlog and the Dashboard read the
		// project, not the team, so what they hold is still right.
		if sprintChanged {
			a.refreshDashboard()
			cmds = append(cmds, a.loadView(viewSprint), a.loadDashLanes(), a.refreshChanges(false))
		}
		if filterChanged {
			cmds = append(cmds, a.reloadFiltered())
		}
		for _, v := range []viewID{viewBacklog, viewDash} {
			if a.needsLoad(v) {
				cmds = append(cmds, a.loadView(v))
			}
		}
	}
	a.refreshDetail()
	return tea.Batch(cmds...)
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
	if t := a.changes; a.view == viewChanges && t.filtering {
		switch msg.String() {
		case "esc":
			t.filtering = false
			t.filter.SetValue("")
			t.filter.Blur()
			t.build()
			a.refreshDetail()
			return nil
		case "enter":
			t.filtering = false
			t.filter.Blur()
			return nil
		}
		var cmd tea.Cmd
		t.filter, cmd = t.filter.Update(msg)
		t.build()
		a.refreshDetail()
		return cmd
	}
	if a.focusDetail {
		switch {
		case key.Matches(msg, keys.Quit):
			return tea.Quit
		case key.Matches(msg, keys.Focus), key.Matches(msg, keys.Back):
			a.focusDetail = false
			return nil
		}
		var cmd tea.Cmd
		a.detail, cmd = a.detail.Update(msg)
		return cmd
	}

	if a.pendingGoto {
		a.pendingGoto = false
		return a.onGotoKey(msg)
	}
	switch {
	case key.Matches(msg, keys.Goto):
		a.pendingGoto = true
		return nil
	case key.Matches(msg, keys.JumpBack):
		return a.jumpBack()
	case key.Matches(msg, keys.JumpFwd):
		return a.jumpForward()
	}

	if a.view == viewItem && a.item != nil {
		if cmd, handled := a.itemOverride(msg); handled {
			return tea.Batch(cmd, a.loadItemPreview())
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
		return tea.Quit
	case key.Matches(msg, keys.Help):
		a.popup = helpPopup{}
		return nil
	case key.Matches(msg, keys.Command):
		a.cmdActive = true
		a.cmd.input.SetValue("")
		return a.cmd.input.Focus()
	case key.Matches(msg, keys.Refresh):
		return a.refresh()
	case key.Matches(msg, keys.AutoRefresh):
		return a.toggleAutoRefresh()
	case key.Matches(msg, keys.Dashboard):
		return a.jumpView(viewDash)
	case key.Matches(msg, keys.Sprint):
		return a.jumpView(viewSprint)
	case key.Matches(msg, keys.Board):
		return a.jumpView(viewBoard)
	case key.Matches(msg, keys.Backlog):
		return a.jumpView(viewBacklog)
	case key.Matches(msg, keys.Team):
		return a.jumpView(viewTeam)
	case key.Matches(msg, keys.Changes):
		return a.jumpView(viewChanges)
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
		case viewTeam:
			a.previewTeam = !a.previewTeam
		case viewChanges:
			a.previewChg = !a.previewChg
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
		return tea.Batch(a.onItemKey(msg), a.loadItemPreview())
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
	if a.view == viewTeam {
		if msg.String() == "enter" { // enter drills in from a row
			return a.openItem(a.currentItem())
		}
		return a.onTeamKey(msg)
	}
	if a.view == viewDash {
		if msg.String() == "enter" { // enter drills in from a card
			return a.openItem(a.currentItem())
		}
		return a.onDashKey(msg)
	}
	if a.view == viewChanges {
		if msg.String() == "enter" { // enter drills in from a change
			return a.openItem(a.currentItem())
		}
		return a.onChangesKey(msg)
	}
	return a.onListKey(msg, a.activeList())
}

// refresh is r: reload what is on screen. It also retries what an earlier
// failure left missing, the context itself or the team filter's areas.
func (a *App) refresh() tea.Cmd {
	if !a.ctxLoaded || a.filterUnresolved() {
		return a.loadContext()
	}
	return a.loadView(a.view)
}

// dashOverride handles the keys the Dashboard's layout redefines: Tab
// toggles focus between the kanban (top) and the lanes (bottom); it is a
// no-op when there are no lanes to focus. Tabbing down from a kanban card
// that has a lane (an in-progress PBI) lands on that PBI's tasks; any other
// card leaves the lanes cursor where it was. The preview pane is never part
// of this cycle — it isn't a focusable pane at all on the Dashboard — so
// ctrl+u/ctrl+d (scrollPreview) are the only way to scroll it, and they
// work the same regardless of which of the two panes has focus.
func (a *App) dashOverride(msg tea.KeyMsg) (tea.Cmd, bool) {
	if key.Matches(msg, keys.Focus) {
		if len(a.dashLanes.ls) > 0 {
			if !a.dashFocusLanes {
				a.dashLanes.focusLane(a.dashBoard.currentID())
			}
			a.dashFocusLanes = !a.dashFocusLanes
			a.refreshDetail()
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
		b.move(0, -1<<20)
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
	case key.Matches(msg, keys.BoardLayout):
		b.list = !b.list
		b.clamp()
		a.cfg.BoardList = b.list
		a.persist()
	default:
		return a.onActionKey(msg)
	}
	a.refreshDetail()
	return a.loadPreviewComments(false)
}

func (a *App) onTeamKey(msg tea.KeyMsg) tea.Cmd {
	t := a.team
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
		t.move(1)
	case key.Matches(msg, keys.Up):
		t.move(-1)
	case key.Matches(msg, keys.Left):
		t.moveColumn(-1)
	case key.Matches(msg, keys.Right):
		t.moveColumn(1)
	case key.Matches(msg, keys.Top):
		t.move(-1 << 20)
	case key.Matches(msg, keys.Bottom):
		t.move(1 << 20)
	case key.Matches(msg, keys.NextPerson):
		t.movePerson(1)
	case key.Matches(msg, keys.PrevPerson):
		t.movePerson(-1)
	case key.Matches(msg, keys.FocusPerson):
		t.focus = !t.focus
	case key.Matches(msg, keys.Select):
		t.toggleSelect()
	case key.Matches(msg, keys.ClearSel):
		t.selected = map[int]bool{}
	case key.Matches(msg, keys.ColLeft):
		return a.moveColumn(-1)
	case key.Matches(msg, keys.ColRight):
		return a.moveColumn(1)
	case key.Matches(msg, keys.Closed):
		t.showDone = !t.showDone
		a.cfg.TeamShowDone = t.showDone
		a.persist()
		a.rebucket(a.sprint.all)
	default:
		return a.onActionKey(msg)
	}
	a.refreshDetail()
	return a.loadPreviewComments(false)
}

func (a *App) onChangesKey(msg tea.KeyMsg) tea.Cmd {
	t := a.changes
	switch {
	case key.Matches(msg, keys.PreviewDown):
		if a.scrollPreview(true) {
			return nil
		}
		t.move(a.bodyHeight() / 2)
	case key.Matches(msg, keys.PreviewUp):
		if a.scrollPreview(false) {
			return nil
		}
		t.move(-a.bodyHeight() / 2)
	case key.Matches(msg, keys.Down):
		t.move(1)
	case key.Matches(msg, keys.Up):
		t.move(-1)
	case key.Matches(msg, keys.Top):
		t.move(-1 << 20)
	case key.Matches(msg, keys.Bottom):
		t.move(1 << 20)
	case key.Matches(msg, keys.PageDown):
		t.move(a.bodyHeight() / 2)
	case key.Matches(msg, keys.PageUp):
		t.move(-a.bodyHeight() / 2)
	case key.Matches(msg, keys.NextGroup):
		t.moveGroup(1)
	case key.Matches(msg, keys.PrevGroup):
		t.moveGroup(-1)
	case key.Matches(msg, keys.PeriodShorter):
		return a.setChangePeriod(t.period.step(-1))
	case key.Matches(msg, keys.PeriodLonger):
		return a.setChangePeriod(t.period.step(1))
	case key.Matches(msg, keys.Period):
		return a.pickChangePeriod()
	case key.Matches(msg, keys.Grouping):
		cur, had := t.currentEntry()
		t.group = (t.group + 1) % 3
		t.build()
		if had { // stay on the same change in the new layout
			for i, e := range t.entries {
				if e.c.ItemID == cur.c.ItemID && e.c.At.Equal(cur.c.At) {
					t.cursor = i
				}
			}
		}
	case key.Matches(msg, keys.AllSprints):
		t.allSprints = !t.allSprints
		return a.loadView(viewChanges)
	case key.Matches(msg, keys.Filter):
		t.filtering = true
		return t.filter.Focus()
	case key.Matches(msg, keys.ClearSel):
		t.filter.SetValue("")
		t.build()
	default:
		return a.onActionKey(msg)
	}
	a.refreshDetail()
	return nil
}

// setChangePeriod changes how far back the Changes view looks and reloads
// it. A shorter period could be cut from what is loaded, but the reload is
// cheap (item histories are cached) and keeps one code path.
func (a *App) setChangePeriod(p changePeriod) tea.Cmd {
	if p == a.changes.period {
		return nil
	}
	a.changes.period = p
	a.changes.cursor = 0
	return a.loadView(viewChanges)
}

func (a *App) pickChangePeriod() tea.Cmd {
	now := a.changes.now()
	var items []pickItem
	for _, k := range periodCycle {
		p := changePeriod{kind: k}
		since, until := p.window(now, a.ctx.Iteration)
		desc := "from " + since.Format("Mon 2 Jan")
		if !until.IsZero() {
			desc += " to " + until.AddDate(0, 0, -1).Format("Mon 2 Jan")
		}
		if k == periodSprint && a.ctx.Iteration.Name != "" {
			desc = a.ctx.Iteration.Name + " · " + desc
		}
		items = append(items, pickItem{Label: p.label(now), Desc: desc, Value: p})
	}
	a.popup = newPicker("Changes: period", items, func(pi pickItem) tea.Cmd {
		return a.setChangePeriod(pi.Value.(changePeriod))
	}).selectValue(func(pi pickItem) bool { return pi.Value.(changePeriod) == a.changes.period })
	return nil
}

// changesCommand is :changes [today|yesterday|week|sprint|<days>]: switch
// to the Changes view, optionally setting its period.
func (a *App) changesCommand(arg string) tea.Cmd {
	p := a.changes.period
	switch strings.ToLower(arg) {
	case "":
	case "today":
		p = changePeriod{kind: periodToday}
	case "yesterday", "y":
		p = changePeriod{kind: periodYesterday}
	case "week", "w":
		p = changePeriod{kind: periodWeek}
	case "sprint", "s":
		p = changePeriod{kind: periodSprint}
	default:
		days, err := strconv.Atoi(arg)
		if err != nil || days < 1 || days > 90 {
			return a.setFlash("changes: give today, yesterday, week, sprint, or a number of days (1-90)", true)
		}
		p = changePeriod{kind: periodDays, days: days}
	}
	if p != a.changes.period {
		a.changes.period = p
		a.changes.cursor = 0
	}
	return a.jumpView(viewChanges)
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
		if arg == "" {
			return a.pickTeam()
		}
		if t, ok := a.findTeam(arg); ok {
			return a.switchContext(a.ctx.Project, t.Name)
		}
		return a.setFlash("unknown team: "+arg, true)
	case "filter", "tf":
		if arg == "" {
			return a.pickTeamFilter()
		}
		if arg == "off" || arg == "all" {
			return a.setTeamFilter("")
		}
		if t, ok := a.findTeam(arg); ok {
			return a.setTeamFilter(t.Name)
		}
		return a.setFlash("unknown team: "+arg, true)
	case "changes", "c":
		return a.changesCommand(arg)
	case "project", "proj", "p":
		return a.pickProject()
	case "board", "b":
		return a.pickBoard()
	case "backlog":
		return a.jumpView(viewBacklog)
	case "dash", "dashboard", "d":
		return a.jumpView(viewDash)
	case "refresh", "r":
		return a.refreshEverything()
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
	case "attention", "att":
		return a.toggleAttention()
	case "stale":
		if arg == "" {
			if d := a.cfg.Stale(); d > 0 {
				return a.setFlash(fmt.Sprintf("stale after %d days without a change", d), false)
			}
			return a.setFlash("stale flag off", false)
		}
		days, err := strconv.Atoi(arg)
		if arg == "off" {
			days, err = 0, nil
		}
		if err != nil || days < 0 {
			return a.setFlash("stale: give a number of days, or off", true)
		}
		return a.setStale(days)
	case "help", "h":
		a.popup = helpPopup{}
		return nil
	}
	return a.setFlash("unknown command: "+word, true)
}

// toggleAttention narrows the Sprint tree, Backlog, Board and Team view to
// items with a health signal. It applies to all of them at once, so
// switching view keeps the same question answered.
func (a *App) toggleAttention() tea.Cmd {
	on := !a.sprint.attention
	a.sprint.attention, a.backlog.attention, a.board.attention, a.team.attention = on, on, on, on
	a.rehealth()
	if on {
		return a.setFlash("showing only items that need attention", false)
	}
	return a.setFlash("showing all items", false)
}

// setStale changes the staleness threshold and writes it to the config.
func (a *App) setStale(days int) tea.Cmd {
	a.cfg.StaleDays = &days
	a.health.staleAfter = a.cfg.StaleAfter()
	a.persist()
	a.rehealth()
	if days == 0 {
		return a.setFlash("stale flag off", false)
	}
	return a.setFlash(fmt.Sprintf("stale after %d days without a change", days), false)
}

// rehealth recomputes every view's cached health signals.
func (a *App) rehealth() {
	for _, l := range []*list{a.sprint, a.backlog} {
		if l.all != nil {
			l.rebuild()
		}
	}
	a.rebucket(a.sprint.all)
	a.refreshDashboard()
	if a.item != nil {
		a.item.buildColumns()
	}
	a.refreshDetail()
}

func (a *App) switchView(v viewID) tea.Cmd {
	if v != viewItem {
		a.item, a.itemStack, a.itemPanes = nil, nil, nil
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
	case viewSprint, viewBoard, viewTeam:
		return a.sprint.all == nil && !a.loading[viewSprint]
	case viewBacklog:
		return a.backlog.all == nil && !a.loading[viewBacklog]
	case viewDash:
		return a.myItems == nil && !a.loading[viewDash]
	case viewChanges:
		// Also when the sprint, filter or day changed since it was loaded.
		return !a.loading[viewChanges] && (!a.changes.loaded || a.changes.key != changesKey(a.ctx.Project, a.changesQuery()))
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
	return tea.Batch(a.loadView(viewSprint), a.loadDashLanes(), a.refreshChanges(false))
}

// ------------------------------------------------------------ pickers for context

func (a *App) pickProject() tea.Cmd {
	var items []pickItem
	for _, p := range a.projects {
		items = append(items, pickItem{Label: p.Name, Value: p})
	}
	a.popup = newPicker("Project", items, func(pi pickItem) tea.Cmd {
		return a.switchContext(pi.Value.(model.Project).Name, "")
	}).selectValue(func(pi pickItem) bool { return pi.Value.(model.Project).Name == a.ctx.Project })
	return nil
}

func (a *App) pickTeam() tea.Cmd {
	var items []pickItem
	for _, t := range a.teams {
		desc := ""
		if t.Name == a.ctx.Team {
			desc = "← current"
		}
		items = append(items, pickItem{Label: t.Name, Desc: desc, Value: t})
	}
	a.popup = newPicker("Team", items, func(pi pickItem) tea.Cmd {
		return a.switchContext(a.ctx.Project, pi.Value.(model.Team).Name)
	}).selectValue(func(pi pickItem) bool { return pi.Value.(model.Team).Name == a.ctx.Team })
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
		a.rebucket(a.sprint.all)
		if a.view == viewTeam {
			return nil // the Team view's columns come from the board too
		}
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

// columns is a view laid out by board column, which H/L move items along.
type columns interface {
	adjacentColumn(dc int) (int, model.BoardColumn, bool)
}

// activeColumns is what a column move (H/L) applies to: the Board tab's
// board, the Team view, or the Dashboard's kanban when its lanes don't
// have focus. Lanes aren't a board (their state is fixed to "In
// Progress"), so there is no column move for them.
func (a *App) activeColumns() columns {
	switch a.view {
	case viewBoard:
		return a.board
	case viewTeam:
		return a.team
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
	case a.view == viewTeam:
		return a.team.current()
	case a.view == viewChanges:
		return a.changes.current()
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
	case a.view == viewItem, a.view == viewChanges:
		if it := a.currentItem(); it != nil {
			return []*model.WorkItem{it}
		}
		return nil
	case a.view == viewBoard:
		return a.board.targetItems()
	case a.view == viewTeam:
		return a.team.targetItems()
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
	case viewTeam:
		a.team.selected = map[int]bool{}
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
		if it, ok := l.get(id); ok {
			return it
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
	if it, ok := a.changes.items[id]; ok {
		return it
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
	a.changes.apply(it)
	a.rebucket(a.sprint.all)
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
		if a.view.previewsComments() {
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
	if a.view == viewChanges && a.detailWidth() > 0 {
		height-- // the tally line above the panels
	}
	a.detail.Width = w
	a.detail.Height = height
	content := renderDetail(it, parent, children, comments, w, a.ctx.Backlog, a.health)
	if a.view == viewChanges && it != nil {
		content = a.changes.changesDetail(it, w) + content
	}
	a.detail.SetContent(content)
	a.detail.GotoTop()
}

// previewsComments reports whether v's preview pane shows the discussion:
// only the full-height panes beside the Board, Team, Sprint and Backlog
// are tall enough to fit it alongside the children and description without
// the rest becoming unreadable.
func (v viewID) previewsComments() bool {
	switch v {
	case viewBoard, viewTeam, viewSprint, viewBacklog:
		return true
	}
	return false
}

// loadPreviewComments fetches the discussion for the currently selected
// item for the preview pane, on the views whose preview shows it. force
// bypasses the cache, for an explicit refresh.
func (a *App) loadPreviewComments(force bool) tea.Cmd {
	if !a.view.previewsComments() {
		return nil
	}
	if it := a.currentItem(); it != nil {
		return a.loadComments(it, force)
	}
	return nil
}

// projectOf is the project to address an item through. Items reached via a
// link or mention can live in another project than the one being browsed.
func (a *App) projectOf(it *model.WorkItem) string {
	if it != nil && it.Project != "" {
		return it.Project
	}
	return a.ctx.Project
}

// otherProject refuses an action that only makes sense inside the current
// project (its sprints, areas and parents) when a target lives elsewhere.
func (a *App) otherProject(targets []*model.WorkItem, action string) tea.Cmd {
	for _, it := range targets {
		if p := a.projectOf(it); !strings.EqualFold(p, a.ctx.Project) {
			return a.setFlash(fmt.Sprintf("#%d is in project %s; switch to it (:project) to %s", it.ID, p, action), true)
		}
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
			a.pushJump()
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
		return popupMsg{&report{title: fmt.Sprintf("#%d", id), lines: strings.Split(renderDetail(it, nil, nil, nil, 70, a.ctx.Backlog, a.health), "\n")}}
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
	a.pushJump()
	if a.view != viewItem || a.item == nil {
		a.itemReturn = a.view
		a.itemStack, a.itemPanes = nil, nil
	} else {
		a.itemPanes = append(a.itemPanes[:len(a.itemStack)-1], a.item.pane())
	}
	a.itemStack = append(a.itemStack, it)
	a.view = viewItem
	a.focusDetail = false
	return a.showItemView(it)
}

func (a *App) showItemView(it *model.WorkItem) tea.Cmd {
	v := newItemView(it, a.ctx.Backlog, a.health)
	v.kanban = a.cfg.ItemKanban
	v.src = itemSource{
		lookup:     a.lookup,
		childrenOf: a.childItems,
		commentsOf: func(id int) []model.Comment { return a.comments[id] },
	}
	v.parent = a.lookup(it.ParentID)
	if v.siblings {
		// The siblings are context for the item, not what it is about:
		// the description keeps focus.
		if it.ParentID != 0 {
			v.setChildren(a.childItems(it.ParentID), nil)
		}
	} else {
		v.focusKan = len(a.childItems(it.ID)) > 0
		v.setChildren(a.childItems(it.ID), nil)
	}
	if c, ok := a.comments[it.ID]; ok {
		v.setComments(c)
	}
	a.item = v
	return tea.Batch(a.loadItemChildren(it), a.loadComments(it, false), a.loadLinks(it.ID), a.loadItemParent())
}

type itemParentMsg struct {
	id     int // the item drilled into
	parent *model.WorkItem
	err    error
}

// loadItemParent fetches the parent of the item drilled into when no list
// has it, for the parent card a task-level item shows.
func (a *App) loadItemParent() tea.Cmd {
	v := a.item
	if v == nil || !v.siblings || v.item.ParentID == 0 || (v.parent != nil && v.parent.ID == v.item.ParentID) {
		return nil
	}
	id, pid := v.item.ID, v.item.ParentID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		p, err := a.client.Get(ctx, pid)
		return itemParentMsg{id: id, parent: p, err: err}
	}
}

type linksLoadedMsg struct {
	id    int
	links []model.RelatedItem
	err   error
}

func (a *App) loadLinks(id int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		links, err := a.client.Links(ctx, id)
		return linksLoadedMsg{id: id, links: links, err: err}
	}
}

type mentionsLoadedMsg struct {
	id    int
	ids   []int // the scan this resolves, in order of appearance
	items map[int]*model.WorkItem
	err   error
}

// resolveMentions re-scans the open item for #1234 mentions and resolves
// any new set: from the loaded lists where it can, fetching the rest. It
// waits for the links so an item that is both linked and mentioned only
// shows once, under its link.
func (a *App) resolveMentions() tea.Cmd {
	v := a.item
	if a.view != viewItem || v == nil || v.linksLoading {
		return nil
	}
	exclude := make([]int, 0, len(v.links))
	for _, l := range v.links {
		exclude = append(exclude, l.Item.ID)
	}
	ids := model.Mentions(v.item, v.comments, exclude...)
	if slices.Equal(ids, v.mentionIDs) {
		return nil
	}
	v.mentionIDs = ids
	known := map[int]*model.WorkItem{}
	for _, c := range v.children {
		known[c.ID] = c
	}
	for _, m := range v.mentions {
		known[m.Item.ID] = m.Item
	}
	found := map[int]*model.WorkItem{}
	var missing []int
	for _, id := range ids {
		if it := known[id]; it != nil {
			found[id] = it
		} else if it := a.lookup(id); it != nil {
			found[id] = it
		} else {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		v.setMentions(ids, mentionRows(ids, found))
		return nil
	}
	id := v.item.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		items, err := a.client.Items(ctx, missing)
		for _, it := range items {
			found[it.ID] = it
		}
		return mentionsLoadedMsg{id: id, ids: ids, items: found, err: err}
	}
}

// mentionRows keeps the order the mentions appear in, dropping ids that
// did not resolve (deleted, or not visible to this user).
func mentionRows(ids []int, items map[int]*model.WorkItem) []model.RelatedItem {
	var out []model.RelatedItem
	for _, id := range ids {
		if it := items[id]; it != nil {
			out = append(out, model.RelatedItem{Kind: model.RelMention, Item: it})
		}
	}
	return out
}

// loadItemChildren fetches what the drill-down's kanban lists: the item's
// children, or for a task-level item the children of its parent. The
// reply is keyed by the item, so one for an item since left is dropped.
func (a *App) loadItemChildren(it *model.WorkItem) tea.Cmd {
	project, id, cfg := a.projectOf(it), it.ID, a.ctx.Backlog
	owner := id
	if cfg.TaskLevel(it) {
		owner = it.ParentID
		if owner == 0 {
			return func() tea.Msg { return itemLoadedMsg{id: id} } // no parent, no siblings
		}
	}
	known := a.childItems(owner)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		children, err := a.client.Children(ctx, project, owner)
		if err != nil {
			return itemLoadedMsg{id: id, err: err}
		}
		// Column order comes from the type most of the children are, so a
		// task kanban reads To Do → In Progress → Done.
		typ := dominantType(children)
		if typ == "" {
			typ = dominantType(known)
		}
		if typ == "" && owner != id {
			typ = it.Type // siblings share the item's type, near enough
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
func (a *App) loadComments(it *model.WorkItem, force bool) tea.Cmd {
	id := it.ID
	if !force {
		if _, ok := a.comments[id]; ok {
			return nil
		}
	}
	if a.commentsLoading[id] {
		return nil
	}
	a.commentsLoading[id] = true
	project, since := a.projectOf(it), a.commentsGen[id]
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
	a.pushJump()
	if len(a.itemStack) > 1 {
		a.itemStack = a.itemStack[:len(a.itemStack)-1]
		cmd := a.showItemView(a.itemStack[len(a.itemStack)-1])
		if n := len(a.itemStack); len(a.itemPanes) >= n {
			a.item.restore(a.itemPanes[n-1])
			a.itemPanes = a.itemPanes[:n-1]
		}
		return cmd
	}
	a.itemStack, a.itemPanes = nil, nil
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
	for _, c := range a.childItems(a.item.listOwner()) {
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
	if p := a.lookup(a.item.item.ParentID); p != nil || a.item.parent == nil || a.item.parent.ID != a.item.item.ParentID {
		a.item.parent = p // keep a fetched parent no list has
	}
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
			v.cycleFocus()
		}
		return nil, true
	case key.Matches(msg, keys.Preview):
		v.descOnly = !v.descOnly
		if v.descOnly {
			v.focusKan, v.focusRel = false, false // the right side is gone; focus follows
		}
		return nil, true
	case key.Matches(msg, keys.Comments):
		v.showComments = !v.showComments
		return nil, true
	case key.Matches(msg, keys.ChildLayout):
		v.kanban = !v.kanban
		v.clamp()
		a.cfg.ItemKanban = v.kanban
		a.persist()
		return nil, true
	case key.Matches(msg, keys.Related):
		// A jump straight to Related, and back out to the description.
		if v.focusRel {
			v.focusRel = false
		} else {
			v.focusRel, v.focusKan, v.descOnly = true, false, false
		}
		return nil, true
	case key.Matches(msg, keys.Comment):
		return a.addComment(v.current()), true
	case key.Matches(msg, keys.Refresh):
		v.loading = true
		v.commentsLoading = true
		v.linksLoading = true
		v.mentionIDs = nil // re-resolve mentions once the links are back
		return tea.Batch(a.loadItemChildren(v.item), a.loadComments(v.item, true), a.loadLinks(v.item.ID), a.loadItemParent()), true
	case key.Matches(msg, keys.Details), msg.String() == "enter":
		if c := v.currentChild(); v.focusKan && c != nil && !v.isSelf(c) {
			return a.openItem(c), true
		}
		if r := v.currentRelated(); v.focusRel && r != nil {
			return a.openItem(r), true
		}
		return nil, true
	}
	return nil, false
}

func (a *App) onItemKey(msg tea.KeyMsg) tea.Cmd {
	v := a.item
	if v.focusRel {
		switch {
		case key.Matches(msg, keys.Down):
			v.moveRelated(1)
			return nil
		case key.Matches(msg, keys.Up):
			v.moveRelated(-1)
			return nil
		case key.Matches(msg, keys.Top):
			v.relRow = 0
			return nil
		case key.Matches(msg, keys.Bottom):
			v.moveRelated(1 << 20)
			return nil
		case key.Matches(msg, keys.PreviewDown):
			v.preview.HalfViewDown()
			return nil
		case key.Matches(msg, keys.PreviewUp):
			v.preview.HalfViewUp()
			return nil
		}
		return a.onActionKey(msg)
	}
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
		// The top of the column on the kanban, the first child in the list.
		v.move(0, -1<<20)
	case key.Matches(msg, keys.Bottom):
		v.move(0, 1<<20)
	case key.Matches(msg, keys.PreviewDown):
		v.preview.HalfViewDown()
	case key.Matches(msg, keys.PreviewUp):
		v.preview.HalfViewUp()
	case key.Matches(msg, keys.ColLeft):
		return a.moveChildState(-1)
	case key.Matches(msg, keys.ColRight):
		return a.moveChildState(1)
	default:
		return a.onActionKey(msg)
	}
	return nil
}

// loadItemPreview fetches the discussion of the item the drill-down is
// previewing, so it can show under the preview's description.
func (a *App) loadItemPreview() tea.Cmd {
	if a.view != viewItem || a.item == nil {
		return nil
	}
	if it := a.item.previewed(); it != nil {
		return a.loadComments(it, false)
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

// boardWidth and dashBoardWidth are the widths the Board and the
// Dashboard's kanban render at: the screen less the preview pane beside them.
func (a *App) boardWidth() int {
	if a.view != viewBoard && a.view != viewTeam && a.view != viewChanges {
		return a.w
	}
	if dw := a.detailWidth(); dw > 0 {
		return a.w - dw - 2
	}
	return a.w
}

func (a *App) dashBoardWidth() int {
	if a.view != viewDash {
		return a.w
	}
	if dw := a.detailWidth(); dw > 0 {
		return a.w - dw - 2
	}
	return a.w
}

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
	if a.view == viewBoard || a.view == viewTeam || a.view == viewChanges {
		if (a.view == viewBoard && !a.previewBoard) || (a.view == viewTeam && !a.previewTeam) ||
			(a.view == viewChanges && !a.previewChg) {
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
	// The header describes the boards' scroll position, so settle it for
	// this frame's widths before rendering either.
	a.board.scroll(a.boardWidth())
	a.dashBoard.scroll(a.dashBoardWidth())
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
			if a.filterUnresolved() {
				crumb += sErr.Render(" (not applied)")
			}
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
		switch {
		case a.item.focusKan:
			noun := "children"
			if a.item.siblings {
				noun = "siblings"
			}
			focus = fmt.Sprintf("%s %d/%d", noun, a.item.childIndex()+1, len(a.item.children))
		case a.item.focusRel:
			focus = "related"
			if n := len(a.item.related()); n > 0 {
				focus = fmt.Sprintf("related %d/%d", a.item.relIndex()+1, n)
			}
		}
		summary = sMuted.Render(focus)
	case a.view == viewBoard:
		name := a.currentBoard().Name
		if a.board.list {
			name += " · list"
		}
		if h := a.board.scrollHint(); h != "" {
			name += " · " + h
		}
		summary = sMuted.Render(name)
	case a.view == viewTeam:
		summary = sMuted.Render(a.team.summary())
	case a.view == viewChanges:
		summary = sMuted.Render(a.changes.scope())
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
	if a.ctxLoading || a.filterLoading {
		return true
	}
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
			return a.board.view(a.boardWidth(), h, true)
		}
		left := a.board.view(a.boardWidth(), h, !a.focusDetail)
		right := detailStyle.Width(dw).Height(h - 2).Render(a.detail.View())
		return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}
	if a.view == viewTeam {
		if dw == 0 {
			return a.team.view(a.boardWidth(), h, true)
		}
		left := a.team.view(a.boardWidth(), h, !a.focusDetail)
		right := detailStyle.Width(dw).Height(h - 2).Render(a.detail.View())
		return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}
	if a.view == viewChanges {
		// One line above the panels sums the period up, as on the Dashboard.
		tally := a.changes.tallyLine(a.w)
		ph := max(h-1, 3)
		left := a.changes.view(a.boardWidth(), ph, !a.focusDetail || dw == 0, a.loading[viewChanges])
		if dw == 0 {
			return tally + "\n" + left
		}
		right := detailStyle.Width(dw).Height(ph - 2).Render(a.detail.View())
		return tally + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, right)
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
		left := a.dashBoard.view(a.dashBoardWidth(), topH, !a.dashFocusLanes)
		top = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	} else {
		top = a.dashBoard.view(a.dashBoardWidth(), topH, !a.dashFocusLanes)
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
	if a.view == viewChanges && a.changes.filtering {
		return a.changes.filter.View() + sMuted.Render("   enter keep · esc clear")
	}
	bindings := footerTree
	switch {
	case a.view == viewItem && a.item != nil:
		bindings = footerItemDesc
		switch {
		case a.item.focusKan:
			bindings = footerItemKanban
		case a.item.focusRel:
			bindings = footerItemRelated
		}
		if a.item.siblings {
			// n on a task adds to its parent, beside it.
			bindings = slices.Clone(bindings)
			for i, k := range bindings {
				if k.Help() == keys.New.Help() {
					bindings[i] = b("new sibling", "n")
				}
			}
		}
	case a.view == viewBoard:
		bindings = footerBoard
	case a.view == viewTeam:
		bindings = footerTeam
	case a.view == viewChanges:
		bindings = footerChanges
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
