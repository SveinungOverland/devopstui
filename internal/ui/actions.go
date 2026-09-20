package ui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/sync/errgroup"

	"github.com/sveinungoverland/devopstui/internal/model"
)

// onActionKey handles the edit/move keys shared by list and board views.
func (a *App) onActionKey(msg tea.KeyMsg) tea.Cmd {
	targets := a.targetItems()
	if len(targets) == 0 {
		return nil
	}
	cur := targets[0]
	switch {
	case key.Matches(msg, keys.Edit):
		if len(targets) > 1 {
			return a.setFlash("edit works on one item; use s/a/m for bulk changes", true)
		}
		return a.openForm(cur)
	case key.Matches(msg, keys.Title):
		if len(targets) > 1 {
			return a.setFlash("title works on one item", true)
		}
		a.popup = newPrompt(fmt.Sprintf("Title of #%d", cur.ID), cur.Title, func(v string) tea.Cmd {
			if v == "" || v == cur.Title {
				return nil
			}
			return a.update(cur, model.Patch{Field: model.FieldTitle, Value: v})
		})
	case key.Matches(msg, keys.Desc):
		if len(targets) > 1 {
			return a.setFlash("description works on one item", true)
		}
		return a.editDescription(cur, func(v string) tea.Cmd {
			// The editor stays open on ctrl+w, so this can run more than
			// once: re-resolve to carry the revision the last write made.
			return a.update(a.fresh(cur), model.Patch{Field: model.FieldDescription, Value: v})
		})
	case key.Matches(msg, keys.State):
		return a.pickState(targets)
	case key.Matches(msg, keys.Assign):
		return a.pickAssignee(targets)
	case key.Matches(msg, keys.Move), key.Matches(msg, keys.Iteration):
		return a.pickMoveTarget(targets)
	case key.Matches(msg, keys.MoveNext):
		next, ok := a.nextIteration()
		if !ok {
			return a.setFlash("no next sprint", true)
		}
		return a.moveTo(targets, next.Path, next.Name)
	case key.Matches(msg, keys.MoveBacklog):
		return a.moveTo(targets, a.ctx.Project, "backlog")
	case key.Matches(msg, keys.Parent):
		return a.pickParent(targets)
	case key.Matches(msg, keys.Effort):
		return a.promptNumber(targets, "Effort", model.FieldEffort, cur.Effort)
	case key.Matches(msg, keys.Priority):
		return a.promptNumber(targets, "Priority (1-4)", model.FieldPriority, float64(cur.Priority))
	}
	return nil
}

func (a *App) nextIteration() (model.Iteration, bool) {
	for i, it := range a.iterations {
		if it.Path == a.ctx.Iteration.Path && i+1 < len(a.iterations) {
			return a.iterations[i+1], true
		}
	}
	return model.Iteration{}, false
}

// ------------------------------------------------------------ create

// createChild prompts for a title and creates a child under the highlighted
// item: Feature under Epic, requirement under Feature, task under a
// requirement. On a task, the sibling's parent is used. With nothing
// highlighted a requirement is created in the current sprint.
func (a *App) createChild() tea.Cmd {
	return a.newItem(false)
}

// createBug prompts for a title and creates a Bug wherever the team's
// BugsBehavior says bugs live: at the requirement level, the task level, or
// nowhere at all. It otherwise follows the same parent-resolution rules as
// createChild.
func (a *App) createBug() tea.Cmd {
	return a.newItem(true)
}

func (a *App) newItem(forceBug bool) tea.Cmd {
	cfg := a.ctx.Backlog
	parent := a.currentItem()
	if parent != nil && cfg.TaskLevel(parent) {
		parent = a.lookup(parent.ParentID) // new sibling task
	}
	typ := cfg.ChildType(parent)
	if typ == "" {
		return a.setFlash(fmt.Sprintf("%s cannot have children", parent.Type), true)
	}
	if forceBug {
		if !cfg.BugChildOf(parent) {
			if cfg.BugsBehavior == "off" {
				return a.setFlash("bugs are off for this process", true)
			}
			return a.setFlash("can't add a bug here", true)
		}
		typ = "Bug"
	}
	area := a.ctx.Project
	if fa := model.DefaultArea(a.ctx.FilterAreas); fa != "" {
		area = fa // new work belongs to the filtered team
	}
	n := model.NewItem{Type: typ, IterationPath: a.ctx.Iteration.Path, AreaPath: area}
	title := "New " + typ + " in " + a.ctx.Iteration.Name
	if parent != nil {
		n.ParentID = parent.ID
		n.IterationPath = parent.IterationPath
		if parent.AreaPath != "" && a.ctx.FilterTeam == "" {
			n.AreaPath = parent.AreaPath
		}
		title = fmt.Sprintf("New %s under #%d %s", typ, parent.ID, trunc(parent.Title, 30))
	}
	if a.view == viewSprint || a.view == viewBoard {
		n.IterationPath = a.ctx.Iteration.Path // keep new work in the sprint you are looking at
	}
	// A task belongs to whoever owns the requirement above it, so it
	// starts assigned to the same person. Higher levels are left alone:
	// a Feature is rarely done by whoever owns the Epic.
	if parent != nil && parent.AssignedTo != "" && cfg.IsTaskType(typ) {
		n.AssignedTo = parent.AssigneeRef()
		title += sMuted.Render("  → " + parent.AssignedTo)
	}
	a.popup = newPrompt(title, "", func(v string) tea.Cmd {
		if v == "" {
			return nil
		}
		n.Title = v
		a.busy = "creating " + typ
		project := a.ctx.Project
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			it, err := a.client.Create(ctx, project, n)
			return createdMsg{item: it, err: err}
		}
	})
	return nil
}

// ------------------------------------------------------------ single update

func (a *App) update(it *model.WorkItem, patches ...model.Patch) tea.Cmd {
	a.busy = fmt.Sprintf("saving #%d", it.ID)
	id, rev := it.ID, it.Rev
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		upd, err := a.client.Update(ctx, id, rev, patches)
		return itemUpdatedMsg{item: upd, err: err}
	}
}

// ------------------------------------------------------------ bulk

type bulkResult struct {
	id   int
	item *model.WorkItem
	err  error
}

type bulkDoneMsg struct {
	label   string
	results []bulkResult
	reload  bool
}

// bulk runs fn for every item with bounded parallelism.
func (a *App) bulk(items []*model.WorkItem, label string, fn func(context.Context, *model.WorkItem) (*model.WorkItem, error)) tea.Cmd {
	if len(items) == 1 {
		a.busy = fmt.Sprintf("%s #%d", label, items[0].ID)
	} else {
		a.busy = fmt.Sprintf("%s %d items", label, len(items))
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		results := make([]bulkResult, len(items))
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(4)
		for i, it := range items {
			g.Go(func() error {
				upd, err := fn(gctx, it)
				results[i] = bulkResult{id: it.ID, item: upd, err: err}
				return nil
			})
		}
		_ = g.Wait()
		return bulkDoneMsg{label: label, results: results, reload: true}
	}
}

func (a *App) onBulkDone(msg bulkDoneMsg) tea.Cmd {
	var failed []string
	ok := 0
	for _, r := range msg.results {
		if r.err != nil {
			failed = append(failed, sErr.Render(fmt.Sprintf("#%d", r.id))+" "+r.err.Error())
			continue
		}
		ok++
		if r.item != nil {
			a.applyUpdate(r.item)
		}
	}
	a.clearSelection()
	var cmds []tea.Cmd
	if msg.reload {
		cmds = append(cmds, a.reloadAll())
	}
	if len(failed) > 0 {
		a.popup = &report{title: fmt.Sprintf("%s: %d ok, %d failed", msg.label, ok, len(failed)), lines: failed}
		return tea.Batch(cmds...)
	}
	cmds = append(cmds, a.setFlash(fmt.Sprintf("%s: %d item(s)", msg.label, ok), false))
	return tea.Batch(cmds...)
}

// applyPatch is the common path for "set field X on all targets".
func (a *App) applyPatch(targets []*model.WorkItem, label string, patches ...model.Patch) tea.Cmd {
	run := func() tea.Cmd {
		if len(targets) == 1 {
			return a.update(targets[0], patches...)
		}
		return a.bulk(targets, label, func(ctx context.Context, it *model.WorkItem) (*model.WorkItem, error) {
			return a.client.Update(ctx, it.ID, it.Rev, patches)
		})
	}
	if a.needConfirm(targets) {
		a.popup = newConfirm(fmt.Sprintf("%s %d items?", label, len(targets)), describe(targets), run)
		return nil
	}
	return run()
}

func (a *App) needConfirm(targets []*model.WorkItem) bool {
	return len(targets) > 1 || a.cfg.Confirm()
}

func describe(items []*model.WorkItem) string {
	var lines []string
	for i, it := range items {
		if i == 8 {
			lines = append(lines, fmt.Sprintf("… and %d more", len(items)-8))
			break
		}
		lines = append(lines, fmt.Sprintf("#%d %s", it.ID, trunc(it.Title, 50)))
	}
	return strings.Join(lines, "\n")
}

// ------------------------------------------------------------ pickers

func (a *App) pickState(targets []*model.WorkItem) tea.Cmd {
	typ := targets[0].Type
	project := a.ctx.Project
	return func() tea.Msg {
		states, err := a.client.States(context.Background(), project, typ)
		if err != nil {
			return errMsg{err}
		}
		var items []pickItem
		for _, s := range states {
			items = append(items, pickItem{Label: s, Value: s})
		}
		return popupMsg{newPicker("State ("+typ+")", items, func(pi pickItem) tea.Cmd {
			return a.applyPatch(targets, "Set state "+pi.Label, model.Patch{Field: model.FieldState, Value: pi.Value})
		})}
	}
}

// pickAssignee opens a people picker. It shows people already seen on
// loaded items straight away, then searches the project and organisation
// as you type.
func (a *App) pickAssignee(targets []*model.WorkItem) tea.Cmd {
	pick := func(pi pickItem) tea.Cmd {
		p := pi.Value.(model.Person)
		label := "Unassign"
		if p.DisplayName != "" {
			label = "Assign " + p.DisplayName
		}
		return a.applyPatch(targets, label, model.Patch{Field: model.FieldAssignedTo, Value: p.Assignment()})
	}
	a.popup = a.peoplePicker("Assign to", pick)
	return a.searchPeople(a.popup.(*picker).token, "")
}

// peoplePicker builds a searchable picker seeded with the assignees
// already on screen, so it is useful before the first search returns.
func (a *App) peoplePicker(title string, pick func(pickItem) tea.Cmd) *picker {
	items := append([]pickItem{{Label: "Unassigned", Value: model.Person{}}}, peopleItems(a.knownPeople())...)
	p := newPicker(title, items, pick)
	return p.searchable(1, func(token int, q string) tea.Cmd { return a.searchPeople(token, q) })
}

func (a *App) searchPeople(token int, query string) tea.Cmd {
	project := a.ctx.Project
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		people, err := a.client.People(ctx, project, query)
		return pickerItemsMsg{token: token, query: query, items: peopleItems(people), err: err}
	}
}

func peopleItems(people []model.Person) []pickItem {
	items := make([]pickItem, 0, len(people))
	for _, p := range people {
		items = append(items, pickItem{Label: p.DisplayName, Desc: p.UniqueName, Value: p})
	}
	return items
}

// knownPeople are the assignees on the items currently loaded, plus the
// signed-in user, so the picker is never empty while a search runs.
func (a *App) knownPeople() []model.Person {
	seen := map[string]bool{}
	var out []model.Person
	add := func(name string) {
		if name == "" || seen[strings.ToLower(name)] {
			return
		}
		seen[strings.ToLower(name)] = true
		out = append(out, model.Person{DisplayName: name})
	}
	add(a.me)
	for _, l := range []*list{a.sprint, a.backlog} {
		for _, it := range l.all {
			add(it.AssignedTo)
		}
	}
	for _, it := range a.myItems {
		add(it.AssignedTo)
	}
	for _, children := range a.dashChildren {
		for _, c := range children {
			add(c.AssignedTo)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DisplayName < out[j].DisplayName })
	return out
}

func (a *App) pickMoveTarget(targets []*model.WorkItem) tea.Cmd {
	items := []pickItem{{Label: "Backlog", Desc: "no sprint", Value: model.Iteration{Path: a.ctx.Project, Name: "backlog"}}}
	for _, it := range a.movableIterations() {
		items = append(items, pickItem{Label: it.Name, Desc: iterDesc(it), Value: it})
	}
	a.popup = newPicker("Move to", items, func(pi pickItem) tea.Cmd {
		it := pi.Value.(model.Iteration)
		return a.moveTo(targets, it.Path, it.Name)
	})
	return nil
}

// moveTo changes the iteration of targets, offering to bring children along
// when a target has any in the current tree.
func (a *App) moveTo(targets []*model.WorkItem, path, name string) tea.Cmd {
	children := a.childrenOf(targets)
	label := "Move to " + name
	patch := model.Patch{Field: model.FieldIterationPath, Value: path}
	run := func(with []*model.WorkItem) func() tea.Cmd {
		return func() tea.Cmd {
			all := append(append([]*model.WorkItem(nil), targets...), with...)
			return a.bulk(all, label, func(ctx context.Context, it *model.WorkItem) (*model.WorkItem, error) {
				return a.client.Update(ctx, it.ID, it.Rev, []model.Patch{patch})
			})
		}
	}
	if len(children) > 0 {
		a.popup = newChoice(fmt.Sprintf("%s: %d item(s) with %d children", label, len(targets), len(children)), describe(targets),
			[]choiceOpt{
				{"y", "move with children", run(children)},
				{"o", "move only these", run(nil)},
				{"n", "cancel", nil},
			})
		return nil
	}
	if a.needConfirm(targets) {
		a.popup = newConfirm(fmt.Sprintf("%s: %d item(s)?", label, len(targets)), describe(targets), run(nil))
		return nil
	}
	return run(nil)()
}

// childrenOf collects descendants of targets that are visible in the active
// tree and not already targets.
func (a *App) childrenOf(targets []*model.WorkItem) []*model.WorkItem {
	l := a.activeList()
	if l == nil || l.tree == nil || l.flat {
		return nil
	}
	in := map[int]bool{}
	for _, t := range targets {
		in[t.ID] = true
	}
	var out []*model.WorkItem
	for _, t := range targets {
		n, ok := l.tree.Get(t.ID)
		if !ok {
			continue
		}
		for _, id := range n.Descendants() {
			if !in[id] {
				in[id] = true
				if c, ok := l.tree.Get(id); ok && !c.External {
					out = append(out, c.Item)
				}
			}
		}
	}
	return out
}

func (a *App) pickParent(targets []*model.WorkItem) tea.Cmd {
	project := a.ctx.Project
	return func() tea.Msg {
		parents, err := a.client.Parents(context.Background(), project)
		if err != nil {
			return errMsg{err}
		}
		items := []pickItem{{Label: "No parent", Value: 0}}
		for _, p := range parents {
			items = append(items, pickItem{Label: fmt.Sprintf("%s %d %s", p.Kind.Tag(), p.ID, p.Title), Desc: p.State, Value: p.ID})
		}
		return popupMsg{newPicker("Parent", items, func(pi pickItem) tea.Cmd {
			pid := pi.Value.(int)
			label := "Set parent " + trunc(pi.Label, 30)
			run := func() tea.Cmd {
				return a.bulk(targets, label, func(ctx context.Context, it *model.WorkItem) (*model.WorkItem, error) {
					return a.client.SetParent(ctx, it.ID, pid)
				})
			}
			if a.needConfirm(targets) {
				a.popup = newConfirm(fmt.Sprintf("%s for %d item(s)?", label, len(targets)), describe(targets), run)
				return nil
			}
			return run()
		})}
	}
}

func (a *App) promptNumber(targets []*model.WorkItem, title, field string, current float64) tea.Cmd {
	initial := ""
	if current != 0 {
		initial = fmtEffort(current)
	}
	a.popup = newPrompt(title, initial, func(v string) tea.Cmd {
		if v == "" {
			return nil
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return a.setFlash("not a number: "+v, true)
		}
		var val any = f
		if field == model.FieldPriority {
			val = int(f)
		}
		return a.applyPatch(targets, title+" "+v, model.Patch{Field: field, Value: val})
	})
	return nil
}

// moveColumn changes the board column (via state) of the targets.
func (a *App) moveColumn(dc int) tea.Cmd {
	b := a.activeBoard()
	if b == nil {
		return nil
	}
	targets := a.targetItems()
	if len(targets) == 0 {
		return nil
	}
	_, col, ok := b.adjacentColumn(dc)
	if !ok {
		return nil
	}
	state := col.Name
	if len(col.States) > 0 {
		state = col.States[0]
	}
	return a.applyPatch(targets, "Move to "+col.Name, model.Patch{Field: model.FieldState, Value: state})
}

// moveLaneColumn changes the state of the Dashboard lanes' highlighted
// work item(s) to the neighbouring column. Unlike moveColumn, the lanes'
// columns are states directly (there is no board-column → state mapping to
// go through).
func (a *App) moveLaneColumn(dc int) tea.Cmd {
	targets := a.dashLanes.targetItems()
	if len(targets) == 0 {
		return nil
	}
	state, ok := a.dashLanes.adjacentState(dc)
	if !ok {
		return nil
	}
	return a.applyPatch(targets, "Move to "+state, model.Patch{Field: model.FieldState, Value: state})
}

// ------------------------------------------------------------ choice popup

type choiceOpt struct {
	key   string
	label string
	run   func() tea.Cmd
}

type choice struct {
	title, body string
	opts        []choiceOpt
}

func newChoice(title, body string, opts []choiceOpt) *choice {
	return &choice{title: title, body: body, opts: opts}
}

func (c *choice) Update(msg tea.Msg) (popup, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return c, nil
	}
	k := km.String()
	if k == "esc" {
		return nil, nil
	}
	for _, o := range c.opts {
		if o.key == k {
			if o.run == nil {
				return nil, nil
			}
			return nil, o.run()
		}
	}
	return c, nil
}

func (c *choice) View(w, h int) string {
	width := min(max(w-10, 30), 70)
	var b strings.Builder
	b.WriteString(sTitle.Render(c.title) + "\n\n" + wrap(c.body, width-2) + "\n\n")
	for _, o := range c.opts {
		b.WriteString(sKey.Render(o.key) + " " + o.label + "   ")
	}
	return sPopup.Width(width).Render(b.String())
}
