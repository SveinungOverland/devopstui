package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/sveinungoverland/devopstui/internal/model"
)

// teamPerson is one assignee's group in the Team view. The tallies cover
// every one of their items in the sprint, shown or not, so hiding done work
// never changes what the heading says about them.
type teamPerson struct {
	name string // display name, "" for the unassigned group
	// items are the rows shown, in board column order; cols[i] is the
	// column index of items[i].
	items []*model.WorkItem
	cols  []int

	total, active, done int
	effort              float64
	// flagged counts the items with any health signal, which orders the
	// people; signals counts each signal across them for the heading.
	flagged int
	signals map[model.Signal]int
}

// label is the heading's name.
func (p teamPerson) label() string {
	if p.name == "" {
		return "Unassigned"
	}
	return p.name
}

// team is the stand-up view: the sprint's board items grouped by who they
// are assigned to, then by board column, one line each. The people who need
// the most attention come first; unassigned work comes last.
type team struct {
	cardMarks
	def    model.Board
	people []teamPerson
	// p is the cursor's person and row its index into that person's items;
	// a person with no rows still takes the cursor, on row 0.
	p, row int
	// focus shows only the cursor's person.
	focus bool
	// showDone shows done items; otherwise only flagged ones stay.
	showDone bool
	// attention shows only flagged items.
	attention bool
}

func newTeam() *team { return &team{cardMarks: cardMarks{selected: map[int]bool{}}} }

// setItems groups the sprint's requirement-level items by assignee. cfg
// decides whether bugs are rows or tasks; include (nil = all) is the team
// filter.
func (t *team) setItems(def model.Board, items []*model.WorkItem, cfg model.BacklogConfig, include func(*model.WorkItem) bool) {
	cur := t.current()
	var curPerson *string // nil before the first load
	if t.p < len(t.people) {
		curPerson = &t.people[t.p].name
	}
	t.def = def
	t.progress = computeProgress(items, cfg.TaskLevel)
	t.flags = t.health.assessAll(items, t.progress)
	colOf := columnIndex(def)

	byKey := map[string]*teamPerson{}
	var order []string
	for _, it := range items {
		if !boardItem(it, cfg) || (include != nil && !include(it)) {
			continue
		}
		k := personKey(it)
		p, ok := byKey[k]
		if !ok {
			p = &teamPerson{name: it.AssignedTo, signals: map[model.Signal]int{}}
			byKey[k] = p
			order = append(order, k)
		}
		p.total++
		p.effort += it.Effort
		done := isDone(it.State)
		switch {
		case done:
			p.done++
		case isActiveState(it.State):
			p.active++
		}
		f, flagged := t.flags[it.ID]
		if flagged {
			p.flagged++
			for _, s := range f.Signals.List() {
				p.signals[s]++
			}
		}
		// A flagged done item (open tasks left) stays: it is exactly what a
		// sync should bring up.
		if (t.attention && !flagged) || (done && !t.showDone && !flagged) {
			continue
		}
		p.items = append(p.items, it)
		p.cols = append(p.cols, colOf(it))
	}

	t.people = t.people[:0]
	for _, k := range order {
		p := byKey[k]
		if t.attention && len(p.items) == 0 {
			continue
		}
		p.sortByColumn()
		t.people = append(t.people, *p)
	}
	sort.SliceStable(t.people, func(i, j int) bool {
		a, b := t.people[i], t.people[j]
		if (a.name == "") != (b.name == "") {
			return b.name == "" // unassigned last
		}
		if a.flagged != b.flagged {
			return a.flagged > b.flagged
		}
		return strings.ToLower(a.name) < strings.ToLower(b.name)
	})

	switch {
	case cur != nil && t.jumpTo(cur.ID):
	case curPerson != nil && t.jumpToPerson(*curPerson):
		// The item left the view (done and hidden, say): stay with its
		// person rather than jumping to the top.
		t.clamp()
	default:
		t.clamp()
	}
}

// personKey groups items by the assignee's sign-in address when known, so
// two people who share a display name stay apart.
func personKey(it *model.WorkItem) string {
	if it.AssignedToUnique != "" {
		return strings.ToLower(it.AssignedToUnique)
	}
	return it.AssignedTo
}

// sortByColumn orders the rows by board column, keeping the sprint's own
// order within a column.
func (p *teamPerson) sortByColumn() {
	idx := make([]int, len(p.items))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool { return p.cols[idx[i]] < p.cols[idx[j]] })
	items, cols := make([]*model.WorkItem, len(idx)), make([]int, len(idx))
	for i, k := range idx {
		items[i], cols[i] = p.items[k], p.cols[k]
	}
	p.items, p.cols = items, cols
}

func (t *team) clamp() {
	t.p = min(max(t.p, 0), max(len(t.people)-1, 0))
	n := 0
	if t.p < len(t.people) {
		n = len(t.people[t.p].items)
	}
	t.row = min(max(t.row, 0), max(n-1, 0))
}

func (t *team) jumpTo(id int) bool {
	for pi, p := range t.people {
		for ri, it := range p.items {
			if it.ID == id {
				t.p, t.row = pi, ri
				return true
			}
		}
	}
	return false
}

func (t *team) jumpToPerson(name string) bool {
	for pi, p := range t.people {
		if p.name == name {
			t.p = pi
			return true
		}
	}
	return false
}

func (t *team) current() *model.WorkItem {
	if t.p >= len(t.people) || t.row >= len(t.people[t.p].items) {
		return nil
	}
	return t.people[t.p].items[t.row]
}

func (t *team) targets() []int {
	var ids []int
	for _, it := range t.targetItems() {
		ids = append(ids, it.ID)
	}
	return ids
}

func (t *team) targetItems() []*model.WorkItem {
	if len(t.selected) == 0 {
		if it := t.current(); it != nil {
			return []*model.WorkItem{it}
		}
		return nil
	}
	var out []*model.WorkItem
	for _, p := range t.people {
		for _, it := range p.items {
			if t.selected[it.ID] {
				out = append(out, it)
			}
		}
	}
	return out
}

func (t *team) toggleSelect() {
	if it := t.current(); it != nil {
		if t.selected[it.ID] {
			delete(t.selected, it.ID)
		} else {
			t.selected[it.ID] = true
		}
		t.move(1)
	}
}

// stop is one cursor position: a row, or a person with no rows.
type stop struct{ p, row int }

// stops lists the cursor positions in screen order, only the cursor's
// person's when focused.
func (t *team) stops() []stop {
	var out []stop
	for pi, p := range t.people {
		if t.focus && pi != t.p {
			continue
		}
		if len(p.items) == 0 {
			out = append(out, stop{pi, 0})
		}
		for ri := range p.items {
			out = append(out, stop{pi, ri})
		}
	}
	return out
}

// move walks dr rows down (up when negative) through every person's rows
// in order, stopping at the ends.
func (t *team) move(dr int) {
	st := t.stops()
	if len(st) == 0 {
		return
	}
	i := t.stopIndex(st)
	i = min(max(i+dr, 0), len(st)-1)
	t.p, t.row = st[i].p, st[i].row
}

func (t *team) stopIndex(st []stop) int {
	for i, s := range st {
		if s.p == t.p && s.row == t.row {
			return i
		}
	}
	return 0
}

// movePerson jumps to the first row of the next (d > 0) or previous
// person; in focus mode that person then becomes the one shown.
func (t *team) movePerson(d int) {
	if len(t.people) == 0 {
		return
	}
	t.p = min(max(t.p+d, 0), len(t.people)-1)
	t.row = 0
}

// moveColumn jumps to the first row of the next (d > 0) or previous
// column group, across people unless focused.
func (t *team) moveColumn(d int) {
	st := t.stops()
	if len(st) == 0 {
		return
	}
	group := func(s stop) [2]int {
		p := t.people[s.p]
		if len(p.items) == 0 {
			return [2]int{s.p, -1}
		}
		return [2]int{s.p, p.cols[s.row]}
	}
	i := t.stopIndex(st)
	g := group(st[i])
	// Step to the first stop of the group; stepping back from there
	// lands in the previous group, whose first stop is then found.
	for i > 0 && group(st[i-1]) == g {
		i--
	}
	if d > 0 {
		for i < len(st) && group(st[i]) == g {
			i++
		}
		if i == len(st) {
			return
		}
	} else {
		if i == 0 {
			return
		}
		i--
		g = group(st[i])
		for i > 0 && group(st[i-1]) == g {
			i--
		}
	}
	t.p, t.row = st[i].p, st[i].row
}

// adjacentColumn returns the board column dc away from the current item's.
func (t *team) adjacentColumn(dc int) (int, model.BoardColumn, bool) {
	if t.current() == nil {
		return -1, model.BoardColumn{}, false
	}
	ci := t.people[t.p].cols[t.row] + dc
	if ci < 0 || ci >= len(t.def.Columns) {
		return -1, model.BoardColumn{}, false
	}
	return ci, t.def.Columns[ci], true
}

// summary is the header's note on what the view shows.
func (t *team) summary() string {
	n := 0
	for _, p := range t.people {
		if p.name != "" {
			n++
		}
	}
	s := fmt.Sprintf("%d people", n)
	if n == 1 {
		s = "1 person"
	}
	if t.focus && t.p < len(t.people) {
		s += " · " + t.people[t.p].label()
	}
	if !t.showDone {
		s += " · done hidden"
	}
	return s
}

func (t *team) view(width, height int, focused bool) string {
	style := sPanel
	if focused {
		style = sPanelFocus
	}
	w := max(width-4, 20)
	innerH := max(height-2, 3)
	var lines []string
	curLine, headLine := 0, 0
	for pi, p := range t.people {
		if t.focus && pi != t.p {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		if pi == t.p {
			headLine = len(lines)
		}
		lines = append(lines, t.personHeading(p, pi == t.p, w))
		if len(p.items) == 0 {
			cur := pi == t.p && focused
			if pi == t.p {
				curLine = len(lines)
			}
			mark := " "
			if cur {
				mark = sKey.Render(cursorMark)
			}
			empty := "nothing open"
			if p.total == 0 {
				empty = "nothing here"
			}
			lines = append(lines, "  "+mark+sMuted.Render(empty))
			continue
		}
		for ri, it := range p.items {
			if ri == 0 || p.cols[ri] != p.cols[ri-1] {
				lines = append(lines, "  "+t.columnHeading(p, ri))
			}
			cur := pi == t.p && ri == t.row
			if cur {
				curLine = len(lines)
			}
			lines = append(lines, "  "+t.renderRow(it, w-2, cur && focused, false))
		}
	}
	if len(lines) == 0 {
		msg := "nobody has work in this sprint"
		if t.attention {
			msg = "nothing needs attention"
		}
		lines = []string{sMuted.Render(msg)}
	}
	// Scrolled, the cursor's person starts at the top when their block
	// fits, so each person comes into view whole as you move to them.
	start := 0
	if len(lines) > innerH {
		start = curLine - innerH + 1
		if curLine-headLine < innerH {
			start = headLine
		}
		start = max(min(start, len(lines)-innerH), 0)
	}
	end := min(start+innerH, len(lines))
	return style.Width(width - 2).Height(innerH).Render(strings.Join(lines[start:end], "\n"))
}

// personHeading is a person's name and what they have in the sprint:
// count, points, how much is active or done, and a tally of each flag.
func (t *team) personHeading(p teamPerson, cur bool, w int) string {
	name := lipgloss.NewStyle().Bold(true).Render(p.label())
	if cur {
		name = sHeader.Render(p.label())
	}
	facts := []string{plural(p.total, "item")}
	if p.effort > 0 {
		facts = append(facts, fmtEffort(p.effort)+" pts")
	}
	if p.active > 0 {
		facts = append(facts, fmt.Sprintf("%d active", p.active))
	}
	if p.done > 0 {
		facts = append(facts, fmt.Sprintf("%d done", p.done))
	}
	head := name + sMuted.Render("  "+strings.Join(facts, " · "))
	var flags []string
	for _, s := range model.AllSignals() {
		if n := p.signals[s]; n > 0 {
			flags = append(flags, signalStyle(s).Render(signalGlyphs[s])+sMuted.Render(fmt.Sprint(n)))
		}
	}
	if len(flags) > 0 {
		head += "   " + strings.Join(flags, " ")
	}
	return trunc(head, w)
}

// columnHeading heads one board column within a person's rows: its name
// and how many of their rows are in it.
func (t *team) columnHeading(p teamPerson, ri int) string {
	ci := p.cols[ri]
	name := ""
	if ci < len(t.def.Columns) {
		name = t.def.Columns[ci].Name
	}
	n := 0
	for _, c := range p.cols {
		if c == ci {
			n++
		}
	}
	return stateStyle(name).Render(name) + sMuted.Render(fmt.Sprintf(" %d", n))
}
