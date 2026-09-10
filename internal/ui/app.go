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
)

func (v viewID) String() string {
	return [...]string{"Dashboard", "Sprint", "Board", "Backlog"}[v]
}

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
	boards     []model.Board

	w, h int
	view viewID

	sprint, backlog, dash *list
	board                 *board
	detail                viewport.Model
	focusDetail           bool
	previewList           bool // detail pane beside list views
	previewBoard          bool // detail pane beside the board

	popup     popup
	cmd       cmdbar
	cmdActive bool

	spin     spinner.Model
	loading  map[viewID]bool
	busy     string // label for an in-flight write
	lastLoad time.Time
	gen      int

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
		sprint:  newList("sprint is empty"),
		backlog: newList("backlog is empty"),
		dash:    newList("nothing assigned to you"),
		board:   newBoard(),
		spin:    sp,
		loading: map[viewID]bool{},
		cmd:     newCmdbar(),
		view:    viewSprint,

		previewList:  true,
		previewBoard: true,
	}
	a.dash.flat = true
	a.dash.showIter = true
	a.wireLists()
	a.ctx.Org = cfg.Org
	a.ctx.Project = cfg.Project
	a.ctx.Team = cfg.Team
	a.ctx.FilterTeam = cfg.FilterTeam
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
	for _, l := range []*list{a.sprint, a.backlog, a.dash} {
		l.include = inc
		if l.all != nil {
			l.rebuild()
		}
	}
	a.board.setItems(a.currentBoard(), a.sprint.all, a.ctx.Backlog, inc)
	a.refreshDetail()
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
	for _, l := range []*list{a.sprint, a.backlog, a.dash} {
		l.taskLevel = func(w *model.WorkItem) bool { return a.ctx.Backlog.TaskLevel(w) }
		l.parentTitle = func(id int) string {
			if p := a.lookup(id); p != nil {
				return p.Title
			}
			return ""
		}
	}
	a.dash.progressItems = func() []*model.WorkItem {
		seen := map[int]bool{}
		var out []*model.WorkItem
		for _, l := range []*list{a.sprint, a.backlog, a.dash} {
			for _, it := range l.all {
				if !seen[it.ID] {
					seen[it.ID] = true
					out = append(out, it)
				}
			}
		}
		return out
	}
}

// ------------------------------------------------------------ messages

type contextLoadedMsg struct {
	me         string
	projects   []model.Project
	teams      []model.Team
	iterations []model.Iteration
	boards     []model.Board
	backlog    model.BacklogConfig
	filter     []model.TeamArea // areas of the configured filter team, if any
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

type popupMsg struct{ p popup }
type errMsg struct{ err error }
type flashMsg struct{ text string }
type clearFlashMsg struct{}
type tickMsg struct{}

// ------------------------------------------------------------ init

func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{a.spin.Tick, a.loadContext()}
	if a.cfg.RefreshSeconds > 0 {
		cmds = append(cmds, tick(a.cfg.RefreshSeconds))
	}
	return tea.Batch(cmds...)
}

func tick(sec int) tea.Cmd {
	return tea.Tick(time.Duration(sec)*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
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
			m.items, m.external, m.err = a.client.SprintItems(ctx, c.Project, c.Team, c.Iteration.Path)
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
		return a, tea.Batch(a.loadView(a.view), tick(a.cfg.RefreshSeconds))

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
		switch msg.view {
		case viewSprint:
			a.sprint.setItems(msg.items, msg.external)
			a.board.setItems(a.currentBoard(), msg.items, a.ctx.Backlog, a.include())
		case viewBacklog:
			a.backlog.setItems(msg.items, nil)
		case viewDash:
			a.dash.setItems(msg.items, nil)
		}
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
			}
		}
		a.refreshDetail()
		return a, nil

	case itemUpdatedMsg:
		a.busy = ""
		if msg.err != nil {
			return a, a.setFlash(msg.err.Error(), true)
		}
		a.applyUpdate(msg.item)
		return a, a.setFlash(fmt.Sprintf("saved #%d", msg.item.ID), false)

	case createdMsg:
		a.busy = ""
		if msg.err != nil {
			return a, a.setFlash(msg.err.Error(), true)
		}
		a.pendingJump = msg.item.ID
		return a, tea.Batch(a.reloadAll(), a.setFlash(fmt.Sprintf("created %s #%d", msg.item.Type, msg.item.ID), false))

	case bulkDoneMsg:
		a.busy = ""
		return a, a.onBulkDone(msg)

	case popupMsg:
		a.popup = msg.p
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
			return a, a.setFlash("description unchanged", false)
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
	a.iterations = msg.iterations
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
	for i := len(a.iterations) - 1; i >= 0; i-- {
		if a.iterations[i].Path != "" {
			return a.iterations[i]
		}
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

	// Global keys.
	switch {
	case key.Matches(msg, keys.Quit):
		if l := a.activeList(); l != nil && len(l.selected) > 0 {
			l.clearSelection()
			return nil
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
		if a.view == viewBoard {
			a.previewBoard = !a.previewBoard
		} else {
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
	if a.view == viewBoard {
		return a.onBoardKey(msg)
	}
	return a.onListKey(msg, a.activeList())
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
	default:
		return a.onActionKey(msg)
	}
	a.refreshDetail()
	return nil
}

func (a *App) onBoardKey(msg tea.KeyMsg) tea.Cmd {
	b := a.board
	switch {
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
	return nil
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
	case "help", "h":
		a.popup = helpPopup{}
		return nil
	}
	return a.setFlash("unknown command: "+word, true)
}

func (a *App) switchView(v viewID) tea.Cmd {
	a.view = v
	a.focusDetail = false
	a.refreshDetail()
	if a.needsLoad(v) {
		return a.loadView(v)
	}
	return nil
}

func (a *App) needsLoad(v viewID) bool {
	switch v {
	case viewSprint, viewBoard:
		return a.sprint.all == nil && !a.loading[viewSprint]
	case viewBacklog:
		return a.backlog.all == nil && !a.loading[viewBacklog]
	case viewDash:
		return a.dash.all == nil && !a.loading[viewDash]
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
	return a.loadView(viewSprint)
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
		a.sprint, a.backlog, a.dash = newList("sprint is empty"), newList("backlog is empty"), newList("nothing assigned to you")
		a.dash.flat, a.dash.showIter = true, true
		a.wireLists()
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
	case viewDash:
		return a.dash
	}
	return nil
}

// currentItem is the highlighted item in whichever view is active.
func (a *App) currentItem() *model.WorkItem {
	if a.view == viewBoard {
		return a.board.current()
	}
	if l := a.activeList(); l != nil {
		return l.current()
	}
	return nil
}

// targetItems is the selection or the highlighted item.
func (a *App) targetItems() []*model.WorkItem {
	if a.view == viewBoard {
		return a.board.targetItems()
	}
	if l := a.activeList(); l != nil {
		return l.targetItems()
	}
	return nil
}

func (a *App) clearSelection() {
	if a.view == viewBoard {
		a.board.selected = map[int]bool{}
		return
	}
	if l := a.activeList(); l != nil {
		l.clearSelection()
	}
}

// lookup finds an item by id in any loaded list.
func (a *App) lookup(id int) *model.WorkItem {
	for _, l := range []*list{a.sprint, a.backlog, a.dash} {
		if l.tree != nil {
			if n, ok := l.tree.Get(id); ok {
				return n.Item
			}
		}
	}
	return nil
}

func (a *App) applyUpdate(it *model.WorkItem) {
	a.sprint.apply(it)
	a.backlog.apply(it)
	a.dash.apply(it)
	a.board.setItems(a.currentBoard(), a.sprint.all, a.ctx.Backlog, a.include())
	a.refreshDetail()
}

// childItems returns the direct children of id across the loaded lists,
// sorted by kind then id.
func (a *App) childItems(id int) []*model.WorkItem {
	seen := map[int]bool{}
	var out []*model.WorkItem
	for _, l := range []*list{a.sprint, a.backlog, a.dash} {
		for _, it := range l.all {
			if it.ParentID == id && !seen[it.ID] {
				seen[it.ID] = true
				out = append(out, it)
			}
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
	if it != nil {
		parent = a.lookup(it.ParentID)
		children = a.childItems(it.ID)
	}
	w := a.detailWidth()
	if w == 0 {
		w = a.w - 4
	}
	a.detail.Width = w
	a.detail.Height = a.bodyHeight() - 2
	a.detail.SetContent(renderDetail(it, parent, children, w, a.ctx.Backlog))
	a.detail.GotoTop()
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
		return popupMsg{&report{title: fmt.Sprintf("#%d", id), lines: strings.Split(renderDetail(it, nil, nil, 70, a.ctx.Backlog), "\n")}}
	}
}

// ------------------------------------------------------------ view

func (a *App) bodyHeight() int { return max(a.h-5, 1) }

// detailWidth is the width of the side preview pane, 0 when hidden (toggled
// off with z, or the terminal is too narrow).
func (a *App) detailWidth() int {
	if a.w < 110 {
		return 0
	}
	if a.view == viewBoard {
		if !a.previewBoard {
			return 0
		}
		return min(a.w/3, 60)
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
	if a.me != "" {
		right = sMuted.Render(a.me+"  ") + right
	}
	line1 := pad(left, a.w-lipgloss.Width(right)) + right

	var tabs []string
	for _, v := range []viewID{viewDash, viewSprint, viewBoard, viewBacklog} {
		label := fmt.Sprintf("%d %s", int(v)+1, v)
		if v == a.view {
			tabs = append(tabs, sTabActive.Render(label))
		} else {
			tabs = append(tabs, sTab.Render(label))
		}
	}
	summary := ""
	if a.view == viewBoard {
		summary = sMuted.Render(a.currentBoard().Name)
	} else if l := a.activeList(); l != nil {
		summary = l.summary()
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
	l := a.activeList()
	lw := a.w - 2
	if dw > 0 {
		lw = a.w - dw - 4
	}
	content := l.view(lw, h-2)
	if a.view == viewDash {
		content = a.dashSummary(lw) + "\n" + l.view(lw, h-3)
	}
	left := listStyle.Width(lw).Height(h - 2).Render(content)
	if dw == 0 {
		return left
	}
	right := detailStyle.Width(dw).Height(h - 2).Render(a.detail.View())
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func (a *App) dashSummary(width int) string {
	inSprint, elsewhere := 0, 0
	var effort float64
	for _, it := range a.dash.all {
		if it.IterationPath == a.ctx.Iteration.Path {
			inSprint++
			effort += it.Effort
		} else {
			elsewhere++
		}
	}
	s := fmt.Sprintf("%s in %s · %s elsewhere · %s effort open",
		sKey.Render(strconv.Itoa(inSprint)), a.ctx.Iteration.Name, sKey.Render(strconv.Itoa(elsewhere)), sKey.Render(fmtEffort(effort)))
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
	if a.view == viewBoard {
		bindings = footerBoard
	}
	if a.focusDetail {
		bindings = []key.Binding{keys.Up, keys.Down, keys.Focus, keys.Edit, keys.Open}
	}
	var parts []string
	for _, kb := range bindings {
		parts = append(parts, sKey.Render(kb.Help().Key)+" "+sMuted.Render(kb.Help().Desc))
	}
	hints := strings.Join(parts, "  ")
	msg := ""
	if a.flash != "" {
		if a.flashErr {
			msg = sErr.Render(trunc(a.flash, a.w/2))
		} else {
			msg = sOK.Render(trunc(a.flash, a.w/2))
		}
	}
	return pad(trunc(hints, a.w-lipgloss.Width(msg)-1), a.w-lipgloss.Width(msg)-1) + msg
}
