package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/sveinungoverland/devopstui/internal/markdown"
	"github.com/sveinungoverland/devopstui/internal/model"
)

// itemView is the full-screen view of one work item: its metadata and
// description on one side, a kanban of its children on the other.
type itemView struct {
	item     *model.WorkItem
	parent   *model.WorkItem
	children []*model.WorkItem

	states   []string // kanban column order
	cols     [][]*model.WorkItem
	col, row int

	desc     viewport.Model
	focusKan bool // kanban has focus; otherwise the description
	descOnly bool // z: hide the kanban and use the full width
	loading  bool

	// comments backs the C-toggled discussion pane, shown in the same slot
	// as the description (showComments picks which).
	comments        []model.Comment
	commentsLoading bool
	showComments    bool

	cfg      model.BacklogConfig
	lastDesc string
	lastW    int
}

func newItemView(it *model.WorkItem, cfg model.BacklogConfig) *itemView {
	return &itemView{item: it, cfg: cfg, desc: viewport.New(40, 10), loading: true, commentsLoading: true}
}

// setComments swaps in a loaded discussion.
func (v *itemView) setComments(comments []model.Comment) {
	v.comments = comments
	v.commentsLoading = false
}

// apply swaps in an updated copy of the item or one of its children.
func (v *itemView) apply(it *model.WorkItem) {
	if v.item != nil && v.item.ID == it.ID {
		v.item = it
	}
	for i, c := range v.children {
		if c.ID == it.ID {
			v.children[i] = it
		}
	}
}

// setChildren rebuilds the kanban, keeping the cursor on the same child.
func (v *itemView) setChildren(children []*model.WorkItem, states []string) {
	cur := v.currentChild()
	v.children = children
	if len(states) > 0 {
		v.states = states
	}
	v.buildColumns()
	if cur != nil {
		v.jumpTo(cur.ID)
	}
	v.clamp()
}

// buildColumns places every child in the column matching its state. States
// not in the known order get a column of their own at the end, so nothing
// is ever hidden.
func (v *itemView) buildColumns() {
	order := append([]string(nil), v.states...)
	index := map[string]int{}
	for i, s := range order {
		index[s] = i
	}
	for _, c := range v.children {
		if _, ok := index[c.State]; !ok {
			index[c.State] = len(order)
			order = append(order, c.State)
		}
	}
	v.states = order
	v.cols = make([][]*model.WorkItem, len(order))
	for _, c := range v.children {
		i := index[c.State]
		v.cols[i] = append(v.cols[i], c)
	}
}

func (v *itemView) clamp() {
	if len(v.cols) == 0 {
		v.col, v.row = 0, 0
		return
	}
	v.col = min(max(v.col, 0), len(v.cols)-1)
	v.row = min(max(v.row, 0), max(len(v.cols[v.col])-1, 0))
}

func (v *itemView) jumpTo(id int) {
	for c, col := range v.cols {
		for r, it := range col {
			if it.ID == id {
				v.col, v.row = c, r
				return
			}
		}
	}
}

// currentChild is the highlighted card, nil when the kanban is empty.
func (v *itemView) currentChild() *model.WorkItem {
	if v.col >= len(v.cols) || v.row >= len(v.cols[v.col]) {
		return nil
	}
	return v.cols[v.col][v.row]
}

// current is what actions apply to: the highlighted child when the kanban
// has focus, otherwise the item itself.
func (v *itemView) current() *model.WorkItem {
	if v.focusKan {
		if c := v.currentChild(); c != nil {
			return c
		}
	}
	return v.item
}

func (v *itemView) move(dc, dr int) {
	if dc != 0 && len(v.cols) > 0 {
		// Moving between columns keeps roughly the same row, but never
		// lands past the end of a shorter column.
		v.col = min(max(v.col+dc, 0), len(v.cols)-1)
	}
	v.row += dr
	v.clamp()
}

// adjacentState returns the state of the column dc away, if any.
func (v *itemView) adjacentState(dc int) (string, bool) {
	i := v.col + dc
	if i < 0 || i >= len(v.states) {
		return "", false
	}
	return v.states[i], true
}

func (v *itemView) progress() progress {
	var p progress
	for _, c := range v.children {
		if !v.cfg.TaskLevel(c) {
			continue
		}
		p.total++
		if isDone(c.State) {
			p.done++
		} else {
			p.remaining += c.RemainingWork
		}
	}
	return p
}

// ---------------------------------------------------------------- render

func (v *itemView) view(w, h int, spin string) string {
	head := v.renderHead(w)
	headH := lipgloss.Height(head)
	paneH := max(h-headH, 3)

	descW, kanW := w, 0
	stacked := false
	switch {
	case v.descOnly:
		descW = w
	case w >= 110:
		descW = w * 11 / 20
		kanW = w - descW
	default:
		stacked = true
	}

	if v.descOnly {
		return head + "\n" + v.renderLeft(descW, paneH)
	}
	if stacked {
		dh := max(paneH*3/5, 5)
		return head + "\n" + v.renderLeft(w, dh) + "\n" + v.renderKanban(w, paneH-dh, spin)
	}
	return head + "\n" + lipgloss.JoinHorizontal(lipgloss.Top,
		v.renderLeft(descW, paneH), v.renderKanban(kanW, paneH, spin))
}

// renderLeft is the left-hand pane: the description, or (C) the discussion.
func (v *itemView) renderLeft(w, h int) string {
	if v.showComments {
		return v.renderDiscussion(w, h)
	}
	return v.renderDesc(w, h)
}

func (v *itemView) renderHead(w int) string {
	it := v.item
	title := kindStyle(it.Kind).Render(it.Type) + sMuted.Render(fmt.Sprintf(" #%d  ", it.ID)) +
		sTitle.Render(trunc(it.Title, w-len(it.Type)-14))

	var facts []string
	facts = append(facts, stateStyle(it.State).Render(it.State))
	facts = append(facts, it.Assignee())
	if it.IterationPath != "" {
		facts = append(facts, lastSeg(it.IterationPath))
	}
	if it.Effort > 0 {
		facts = append(facts, fmtEffort(it.Effort)+" pts")
	}
	if v.cfg.TaskLevel(it) && it.RemainingWork > 0 {
		facts = append(facts, fmtEffort(it.RemainingWork)+"h left")
	}
	if it.Priority > 0 {
		facts = append(facts, fmt.Sprintf("P%d", it.Priority))
	}
	if p := v.progress(); p.total > 0 {
		f := p.style().Render(fmt.Sprintf("%d/%d tasks", p.done, p.total))
		if p.remaining > 0 {
			f += sMuted.Render(fmt.Sprintf(" · %sh left", fmtEffort(p.remaining)))
		}
		facts = append(facts, f)
	}
	if len(it.Tags) > 0 {
		facts = append(facts, strings.Join(it.Tags, ", "))
	}
	meta := strings.Join(facts, sMuted.Render(" · "))

	crumb := ""
	if v.parent != nil {
		crumb = sMuted.Render("↑ ") + kindStyle(v.parent.Kind).Render(v.parent.Kind.Tag()) +
			sMuted.Render(fmt.Sprintf(" %d %s", v.parent.ID, trunc(v.parent.Title, 40)))
	}
	updated := sMuted.Render(ago(it.ChangedDate) + " by " + it.ChangedBy)
	line3 := crumb
	if line3 == "" {
		line3 = updated
	} else {
		line3 = pad(line3, w-lipgloss.Width(updated)-1) + updated
	}
	return " " + trunc(title, w-1) + "\n " + trunc(meta, w-1) + "\n " + trunc(line3, w-1)
}

func (v *itemView) renderDesc(w, h int) string {
	style := sPanel
	if !v.focusKan {
		style = sPanelFocus
	}
	inner := max(w-4, 20)
	sections := narrativeSections(v.item)
	if len(sections) == 0 {
		noun := "description"
		if v.item.Kind == model.KindBug {
			noun = "repro steps"
		}
		v.desc.SetContent(sMuted.Render("no " + noun + " — press " + sKey.Render("d") + sMuted.Render(" to write one")))
		v.lastDesc, v.lastW = "", inner
	} else if key := narrativeKey(sections); key != v.lastDesc || inner != v.lastW {
		v.lastDesc, v.lastW = key, inner
		v.desc.SetContent(renderNarrative(sections, inner))
		v.desc.GotoTop()
	}
	v.desc.Width = inner
	v.desc.Height = max(h-3, 1)
	title := sMuted.Render(narrativeTitle(v.item, sections))
	if v.desc.TotalLineCount() > v.desc.Height {
		title += sMuted.Render(fmt.Sprintf("  %d%%", int(v.desc.ScrollPercent()*100)))
	}
	return style.Width(w - 2).Height(h - 2).Render(title + "\n" + v.desc.View())
}

func (v *itemView) renderDiscussion(w, h int) string {
	style := sPanel
	if !v.focusKan {
		style = sPanelFocus
	}
	inner := max(w-4, 20)
	switch {
	case v.commentsLoading:
		v.desc.SetContent(sMuted.Render("loading discussion…"))
		v.lastDesc, v.lastW = "", inner
	case len(v.comments) == 0:
		v.desc.SetContent(sMuted.Render("no comments yet"))
		v.lastDesc, v.lastW = "", inner
	default:
		if body := formatComments(v.comments); body != v.lastDesc || inner != v.lastW {
			v.lastDesc, v.lastW = body, inner
			v.desc.SetContent(markdown.Render(body, inner))
			v.desc.GotoTop()
		}
	}
	v.desc.Width = inner
	v.desc.Height = max(h-3, 1)
	title := sMuted.Render(fmt.Sprintf("Discussion (%d)", len(v.comments)))
	if v.desc.TotalLineCount() > v.desc.Height {
		title += sMuted.Render(fmt.Sprintf("  %d%%", int(v.desc.ScrollPercent()*100)))
	}
	return style.Width(w - 2).Height(h - 2).Render(title + "\n" + v.desc.View())
}

// formatComments renders a discussion as one Markdown document, oldest
// first, separated by rules.
func formatComments(comments []model.Comment) string {
	var b strings.Builder
	for i, c := range comments {
		if i > 0 {
			b.WriteString("\n\n---\n\n")
		}
		fmt.Fprintf(&b, "**%s** · %s\n\n%s", c.Author, ago(c.CreatedDate), c.Text)
	}
	return b.String()
}

func (v *itemView) renderKanban(w, h int, spin string) string {
	style := sPanel
	if v.focusKan {
		style = sPanelFocus
	}
	inner := max(w-4, 16)
	head := sMuted.Render(fmt.Sprintf("Children (%d)", len(v.children)))
	if v.loading {
		head += "  " + spin
	}

	if len(v.children) == 0 && !v.loading {
		child := v.cfg.ChildType(v.item)
		msg := sMuted.Render("no children yet")
		if child != "" {
			msg += "\n\n" + sMuted.Render("press ") + sKey.Render("n") + sMuted.Render(" to add a "+child)
		}
		return style.Width(w - 2).Height(h - 2).Render(head + "\n\n" + msg)
	}

	colW := max(inner/max(len(v.cols), 1), 14)
	visible := max(inner/colW, 1)
	start := 0
	if v.col >= visible {
		start = v.col - visible + 1
	}
	var cols []string
	for i := start; i < len(v.cols) && i < start+visible; i++ {
		cols = append(cols, v.renderColumn(i, colW, h-3))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	if start > 0 || start+visible < len(v.cols) {
		head += sMuted.Render(fmt.Sprintf("   %d-%d of %d columns", start+1, min(start+visible, len(v.cols)), len(v.cols)))
	}
	return style.Width(w - 2).Height(h - 2).Render(head + "\n" + body)
}

func (v *itemView) renderColumn(ci, colW, h int) string {
	name := v.states[ci]
	items := v.cols[ci]
	title := trunc(name, colW-4)
	if ci == v.col && v.focusKan {
		title = sHeader.Render(title)
	} else {
		title = sText.Render(title)
	}
	lines := []string{title + sMuted.Render(fmt.Sprintf(" %d", len(items))),
		sMuted.Render(strings.Repeat("─", colW-2))}

	perCard := 2
	maxCards := max((h-2)/perCard, 1)
	start := 0
	if ci == v.col && v.row >= maxCards {
		start = v.row - maxCards + 1
	}
	for ri := start; ri < len(items) && ri < start+maxCards; ri++ {
		cur := v.focusKan && ci == v.col && ri == v.row
		lines = append(lines, v.renderCard(items[ri], colW-2, cur)...)
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lipgloss.NewStyle().Width(colW).MaxHeight(h).Render(strings.Join(lines[:min(len(lines), h)], "\n"))
}

func (v *itemView) renderCard(it *model.WorkItem, w int, cur bool) []string {
	st := rowStyler(cur)
	plain := st(lipgloss.NewStyle())
	muted := st(sMuted)
	mark := plain.Render(" ")
	if cur {
		mark = st(sKey).Render(cursorMark)
	}
	right := muted.Render(initials(it.AssignedTo))
	if v.cfg.TaskLevel(it) && it.RemainingWork > 0 && !isDone(it.State) {
		right = muted.Render(fmtEffort(it.RemainingWork)+"h ") + right
	} else if it.Effort > 0 {
		right = muted.Render(fmtEffort(it.Effort)+" ") + right
	}
	l1 := mark + st(kindStyle(it.Kind)).Render(it.Kind.Tag()) + plain.Render(" ") + muted.Render(fmt.Sprintf("%d", it.ID))
	l1 += fill(plain, w-lipgloss.Width(l1)-lipgloss.Width(right)) + right
	l2 := plain.Render(" " + trunc(it.Title, w-1))
	l2 += fill(plain, w-lipgloss.Width(l2))
	return []string{l1, l2}
}
