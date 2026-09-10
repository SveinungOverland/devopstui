package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	"github.com/sveinungoverland/devopstui/internal/model"
)

// list renders a hierarchy (or flat list) of work items with a cursor,
// multi-selection, expand/collapse and a fuzzy filter. It is shared by the
// sprint, backlog and dashboard views.
type list struct {
	all       []*model.WorkItem
	external  []*model.WorkItem
	tree      *model.Tree
	rows      []*model.Node
	cursor    int
	offset    int
	collapsed map[int]bool
	selected  map[int]bool
	visual    int // anchor row for v-mode, -1 when off

	filter    textinput.Model
	filtering bool
	flat      bool
	showDone  bool
	showIter  bool // show iteration column (dashboard)
	empty     string

	// taskLevel tells which items count as tasks for progress badges; set
	// by the App from the team's backlog configuration.
	taskLevel func(*model.WorkItem) bool
	// parentTitle resolves a parent's title for flat lists (dashboard).
	parentTitle func(id int) string
	// progressItems supplies the items to count task progress over; nil
	// means this list's own items. The dashboard uses every loaded item so
	// badges match the sprint view.
	progressItems func() []*model.WorkItem
	progress      map[int]progress // parent id → task progress
	parents       map[int]int      // item id → parent id, kept when flattening
}

// progress summarises the task-level children of an item.
type progress struct {
	done, total int
	remaining   float64
}

func (p progress) badge() string {
	if p.total == 0 {
		return ""
	}
	s := fmt.Sprintf("%d/%d", p.done, p.total)
	if p.done == p.total {
		return sOK.Render(s)
	}
	return sMuted.Render(s)
}

func newList(empty string) *list {
	in := textinput.New()
	in.Prompt = "/"
	return &list{collapsed: map[int]bool{}, selected: map[int]bool{}, visual: -1, filter: in, empty: empty}
}

// setItems replaces the data and rebuilds rows, keeping the cursor on the
// same item when possible.
func (l *list) setItems(items, external []*model.WorkItem) {
	cur := l.currentID()
	l.all, l.external = items, external
	l.rebuild()
	l.jumpTo(cur)
}

func (l *list) rebuild() {
	items := l.all
	ext := l.external
	src := l.all
	if l.progressItems != nil {
		src = l.progressItems()
	}
	l.progress = computeProgress(src, l.taskLevel)
	l.parents = make(map[int]int, len(l.all))
	for _, it := range l.all {
		l.parents[it.ID] = it.ParentID
	}
	q := strings.ToLower(l.filter.Value())
	if !l.showDone || q != "" {
		items = filterItems(items, q, l.showDone)
		if q != "" {
			ext = nil
		}
	}
	if l.flat || q != "" {
		flat := make([]*model.WorkItem, len(items))
		for i, it := range items {
			c := *it
			c.ParentID = 0
			flat[i] = &c
		}
		l.tree = model.BuildTree(flat, nil)
	} else {
		l.tree = model.BuildTree(items, ext)
	}
	l.rows = l.tree.Flatten(l.collapsed)
	if l.cursor >= len(l.rows) {
		l.cursor = max(len(l.rows)-1, 0)
	}
	for id := range l.selected {
		if _, ok := l.tree.Get(id); !ok {
			delete(l.selected, id)
		}
	}
}

// computeProgress counts task-level children per parent over all items,
// so hidden (done) tasks still count.
func computeProgress(items []*model.WorkItem, taskLevel func(*model.WorkItem) bool) map[int]progress {
	out := map[int]progress{}
	for _, it := range items {
		if it.ParentID == 0 || taskLevel == nil || !taskLevel(it) {
			continue
		}
		p := out[it.ParentID]
		p.total++
		if isDone(it.State) {
			p.done++
		} else {
			p.remaining += it.RemainingWork
		}
		out[it.ParentID] = p
	}
	return out
}

func filterItems(items []*model.WorkItem, q string, showDone bool) []*model.WorkItem {
	var out []*model.WorkItem
	for _, it := range items {
		if !showDone && isDone(it.State) {
			continue
		}
		if q != "" && !fuzzy(strings.ToLower(fmt.Sprintf("%d %s %s %s %s", it.ID, it.Title, it.State, it.AssignedTo, it.Type)), q) {
			continue
		}
		out = append(out, it)
	}
	return out
}

func isDone(state string) bool {
	switch state {
	case "Done", "Closed", "Removed", "Resolved", "Completed":
		return true
	}
	return false
}

// current returns the highlighted item, nil when the list is empty.
func (l *list) current() *model.WorkItem {
	if l.cursor < 0 || l.cursor >= len(l.rows) {
		return nil
	}
	return l.rows[l.cursor].Item
}

func (l *list) currentNode() *model.Node {
	if l.cursor < 0 || l.cursor >= len(l.rows) {
		return nil
	}
	return l.rows[l.cursor]
}

func (l *list) currentID() int {
	if it := l.current(); it != nil {
		return it.ID
	}
	return 0
}

// targets returns the IDs an action applies to: the selection when there is
// one, otherwise the highlighted item.
func (l *list) targets() []int {
	if len(l.selected) > 0 {
		var ids []int
		for _, r := range l.rows {
			if l.selected[r.Item.ID] {
				ids = append(ids, r.Item.ID)
			}
		}
		return ids
	}
	if it := l.current(); it != nil && !l.currentNode().External {
		return []int{it.ID}
	}
	return nil
}

// items returns the WorkItems for targets.
func (l *list) targetItems() []*model.WorkItem {
	var out []*model.WorkItem
	for _, id := range l.targets() {
		if n, ok := l.tree.Get(id); ok {
			out = append(out, n.Item)
		}
	}
	return out
}

func (l *list) jumpTo(id int) {
	for i, r := range l.rows {
		if r.Item.ID == id {
			l.cursor = i
			return
		}
	}
}

func (l *list) move(delta int) {
	if len(l.rows) == 0 {
		return
	}
	l.cursor = min(max(l.cursor+delta, 0), len(l.rows)-1)
	if l.visual >= 0 {
		l.applyVisual()
	}
}

func (l *list) applyVisual() {
	lo, hi := min(l.visual, l.cursor), max(l.visual, l.cursor)
	for i, r := range l.rows {
		if i >= lo && i <= hi && !r.External {
			l.selected[r.Item.ID] = true
		}
	}
}

func (l *list) toggleSelect() {
	n := l.currentNode()
	if n == nil || n.External {
		return
	}
	if l.selected[n.Item.ID] {
		delete(l.selected, n.Item.ID)
	} else {
		l.selected[n.Item.ID] = true
	}
	l.move(1)
}

func (l *list) toggleVisual() {
	if l.visual >= 0 {
		l.visual = -1
		return
	}
	l.visual = l.cursor
	l.applyVisual()
}

func (l *list) selectAll() {
	for _, r := range l.rows {
		if !r.External {
			l.selected[r.Item.ID] = true
		}
	}
}

func (l *list) clearSelection() {
	l.selected = map[int]bool{}
	l.visual = -1
}

func (l *list) collapse() {
	n := l.currentNode()
	if n == nil {
		return
	}
	if len(n.Children) > 0 && !l.collapsed[n.Item.ID] {
		l.collapsed[n.Item.ID] = true
		l.rows = l.tree.Flatten(l.collapsed)
		return
	}
	// Already collapsed or a leaf: go to parent.
	if n.Item.ParentID != 0 {
		l.jumpTo(n.Item.ParentID)
	}
}

func (l *list) expand() {
	n := l.currentNode()
	if n == nil || len(n.Children) == 0 {
		return
	}
	delete(l.collapsed, n.Item.ID)
	l.rows = l.tree.Flatten(l.collapsed)
}

func (l *list) collapseAll() {
	l.tree.Walk(func(n *model.Node) {
		if len(n.Children) > 0 {
			l.collapsed[n.Item.ID] = true
		}
	})
	l.rows = l.tree.Flatten(l.collapsed)
	l.cursor = min(l.cursor, max(len(l.rows)-1, 0))
}

func (l *list) expandAll() {
	id := l.currentID()
	l.collapsed = map[int]bool{}
	l.rows = l.tree.Flatten(l.collapsed)
	l.jumpTo(id)
}

// apply replaces one item in place after a successful update.
func (l *list) apply(updated *model.WorkItem) {
	for i, it := range l.all {
		if it.ID == updated.ID {
			l.all[i] = updated
		}
	}
	for i, it := range l.external {
		if it.ID == updated.ID {
			l.external[i] = updated
		}
	}
	l.rebuild()
}

// remove drops items from the list (e.g. after moving them out of the sprint).
func (l *list) remove(ids []int) {
	drop := map[int]bool{}
	for _, id := range ids {
		drop[id] = true
	}
	var keep []*model.WorkItem
	for _, it := range l.all {
		if !drop[it.ID] {
			keep = append(keep, it)
		}
	}
	l.all = keep
	l.rebuild()
}

// view renders the list into a box of the given inner size.
func (l *list) view(width, height int) string {
	if len(l.rows) == 0 {
		msg := l.empty
		if l.filter.Value() != "" {
			msg = "no items match the filter"
		}
		return sMuted.Render(msg)
	}
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+height {
		l.offset = l.cursor - height + 1
	}
	// Column widths: marker(2) indent tag(5) id(6) title(*) state(12) who(3) effort(4)
	stateW, whoW, effW := 12, 3, 4
	iterW := 0
	if l.showIter {
		iterW = 12
	}
	var b strings.Builder
	for i := l.offset; i < len(l.rows) && i < l.offset+height; i++ {
		n := l.rows[i]
		it := n.Item
		marker := "  "
		if l.selected[it.ID] {
			marker = sSelected.Render("● ")
		}
		indent := strings.Repeat("  ", n.Depth)
		arrow := "  "
		if len(n.Children) > 0 {
			if l.collapsed[it.ID] {
				arrow = "▸ "
			} else {
				arrow = "▾ "
			}
		}
		tag := kindStyle(it.Kind).Render(pad(it.Kind.Tag(), 4))
		id := sMuted.Render(fmt.Sprintf("%5d", it.ID))
		titleW := width - 2 - len(indent) - 2 - 5 - 6 - stateW - whoW - effW - iterW - 4
		title := it.Title
		if pid := l.parents[it.ID]; l.flat && l.parentTitle != nil && pid != 0 {
			if pt := l.parentTitle(pid); pt != "" {
				title = trunc(title, titleW*2/3) + sMuted.Render("  ↑ "+pt)
			}
		}
		badge := ""
		if p, ok := l.progress[it.ID]; ok {
			badge = " " + p.badge()
		}
		title = trunc(title, titleW-lipgloss.Width(badge))
		if n.External {
			title = sExternal.Render(title)
		}
		title = pad(title+badge, titleW)
		state := stateStyle(it.State).Render(pad(trunc(it.State, stateW), stateW))
		who := sMuted.Render(initials(it.AssignedTo))
		effS := fmtEffort(it.Effort)
		if l.taskLevel != nil && l.taskLevel(it) {
			effS = ""
			if it.RemainingWork > 0 {
				effS = fmtEffort(it.RemainingWork) + "h"
			}
		}
		eff := sMuted.Render(padLeft(effS, effW))
		iter := ""
		if l.showIter {
			iter = " " + sMuted.Render(pad(trunc(lastSeg(it.IterationPath), iterW-1), iterW-1))
		}
		line := marker + indent + arrow + tag + " " + id + " " + title + " " + state + " " + who + " " + eff + iter
		line = pad(line, width)
		if i == l.cursor {
			line = sCursor.Render(line)
		}
		b.WriteString(line)
		if i < l.offset+height-1 && i < len(l.rows)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func lastSeg(path string) string {
	if i := strings.LastIndex(path, "\\"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// summary returns "n items · m selected" for the status line.
func (l *list) summary() string {
	s := fmt.Sprintf("%d/%d", min(l.cursor+1, len(l.rows)), len(l.rows))
	if len(l.selected) > 0 {
		s += sSelected.Render(fmt.Sprintf(" · %d selected", len(l.selected)))
	}
	if l.visual >= 0 {
		s += sSelected.Render(" · VISUAL")
	}
	if q := l.filter.Value(); q != "" {
		s += sMuted.Render(" · /" + q)
	}
	return s
}
