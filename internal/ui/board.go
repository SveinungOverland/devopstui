package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/sveinungoverland/devopstui/internal/model"
)

// board is the Kanban view: items from the current sprint bucketed by column.
type board struct {
	def      model.Board
	cols     [][]*model.WorkItem
	col, row int
	offsetC  int
	selected map[int]bool
	progress map[int]progress
}

func newBoard() *board { return &board{selected: map[int]bool{}} }

// setItems buckets the requirement-level items into columns. cfg decides
// whether bugs are cards or tasks; include (nil = all) is the team filter.
func (b *board) setItems(def model.Board, items []*model.WorkItem, cfg model.BacklogConfig, include func(*model.WorkItem) bool) {
	cur := b.current()
	b.def = def
	b.progress = computeProgress(items, cfg.TaskLevel)
	if include != nil {
		var kept []*model.WorkItem
		for _, it := range items {
			if include(it) {
				kept = append(kept, it)
			}
		}
		items = kept
	}
	b.cols = make([][]*model.WorkItem, len(def.Columns))
	stateToCol := map[string]int{}
	for i, c := range def.Columns {
		stateToCol[c.Name] = i
		for _, st := range c.States {
			if _, ok := stateToCol[st]; !ok {
				stateToCol[st] = i
			}
		}
	}
	for _, it := range items {
		if cfg.TaskLevel(it) || it.Kind == model.KindEpic || it.Kind == model.KindFeature {
			continue // board shows the requirement level
		}
		ci, ok := stateToCol[it.BoardColumn]
		if !ok {
			ci, ok = stateToCol[it.State]
		}
		if !ok {
			ci = 0
		}
		if ci < len(b.cols) {
			b.cols[ci] = append(b.cols[ci], it)
		}
	}
	b.clamp()
	switch {
	case cur != nil:
		b.jumpTo(cur.ID)
	case b.current() == nil:
		// Nothing was selected before (the first load, typically) and the
		// default column-0 landing spot is empty: land on the first card
		// there is, so the preview pane isn't blank just because column 0
		// happens to have nothing in it.
		b.jumpToFirstCard()
	}
}

func (b *board) jumpToFirstCard() {
	for i, col := range b.cols {
		if len(col) > 0 {
			b.col, b.row = i, 0
			return
		}
	}
}

func (b *board) clamp() {
	if len(b.cols) == 0 {
		b.col, b.row = 0, 0
		return
	}
	b.col = min(max(b.col, 0), len(b.cols)-1)
	b.row = min(max(b.row, 0), max(len(b.cols[b.col])-1, 0))
}

func (b *board) jumpTo(id int) {
	for c, items := range b.cols {
		for r, it := range items {
			if it.ID == id {
				b.col, b.row = c, r
				return
			}
		}
	}
}

func (b *board) current() *model.WorkItem {
	if b.col >= len(b.cols) || b.row >= len(b.cols[b.col]) {
		return nil
	}
	return b.cols[b.col][b.row]
}

func (b *board) currentID() int {
	if it := b.current(); it != nil {
		return it.ID
	}
	return 0
}

func (b *board) targets() []int {
	if len(b.selected) > 0 {
		var ids []int
		for _, col := range b.cols {
			for _, it := range col {
				if b.selected[it.ID] {
					ids = append(ids, it.ID)
				}
			}
		}
		return ids
	}
	if it := b.current(); it != nil {
		return []int{it.ID}
	}
	return nil
}

func (b *board) targetItems() []*model.WorkItem {
	want := map[int]bool{}
	for _, id := range b.targets() {
		want[id] = true
	}
	var out []*model.WorkItem
	for _, col := range b.cols {
		for _, it := range col {
			if want[it.ID] {
				out = append(out, it)
			}
		}
	}
	return out
}

func (b *board) toggleSelect() {
	if it := b.current(); it != nil {
		if b.selected[it.ID] {
			delete(b.selected, it.ID)
		} else {
			b.selected[it.ID] = true
		}
		b.move(0, 1)
	}
}

func (b *board) move(dc, dr int) {
	b.col += dc
	b.row += dr
	b.clamp()
}

// adjacentColumn returns the column index dc away, or -1.
func (b *board) adjacentColumn(dc int) (int, model.BoardColumn, bool) {
	ci := b.col + dc
	if ci < 0 || ci >= len(b.def.Columns) {
		return -1, model.BoardColumn{}, false
	}
	return ci, b.def.Columns[ci], true
}

func (b *board) view(width, height int, focused bool) string {
	if len(b.cols) == 0 {
		return sMuted.Render("no board columns")
	}
	colW := min(max(width/len(b.cols), 26), 44)
	visible := max(width/colW, 1)
	if b.col < b.offsetC {
		b.offsetC = b.col
	}
	if b.col >= b.offsetC+visible {
		b.offsetC = b.col - visible + 1
	}
	var rendered []string
	for ci := b.offsetC; ci < len(b.cols) && ci < b.offsetC+visible; ci++ {
		rendered = append(rendered, b.renderColumn(ci, colW, height, focused))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

func (b *board) renderColumn(ci, colW, height int, focused bool) string {
	col := b.def.Columns[ci]
	items := b.cols[ci]
	var effort float64
	for _, it := range items {
		effort += it.Effort
	}
	head := fmt.Sprintf("%s %s", col.Name, sMuted.Render(fmt.Sprintf("(%d", len(items))))
	if effort > 0 {
		head += sMuted.Render(fmt.Sprintf(" · %s pts", fmtEffort(effort)))
	}
	head += sMuted.Render(")")
	if ci == b.col {
		head = sHeader.Render(col.Name) + strings.TrimPrefix(head, col.Name)
	}
	lines := []string{pad(trunc(head, colW-2), colW-2), sMuted.Render(strings.Repeat("─", colW-2))}
	cardH := 3
	maxCards := max((height-2)/cardH, 1)
	start := 0
	if ci == b.col && b.row >= maxCards {
		start = b.row - maxCards + 1
	}
	for ri := start; ri < len(items) && ri < start+maxCards; ri++ {
		it := items[ri]
		cur := ci == b.col && ri == b.row
		lines = append(lines, b.renderCard(it, colW-2, cur && focused)...)
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	style := sPanel
	if ci == b.col && focused {
		style = sPanelFocus
	}
	return style.Width(colW - 2).Height(height).Render(strings.Join(lines[:min(len(lines), height)], "\n"))
}

func (b *board) renderCard(it *model.WorkItem, w int, cur bool) []string {
	st := rowStyler(cur)
	plain := st(lipgloss.NewStyle())
	muted := st(sMuted)
	mark := plain.Render(" ")
	switch {
	case cur && b.selected[it.ID]:
		mark = st(sSelected).Render("●")
	case cur:
		mark = st(sKey).Render(cursorMark)
	case b.selected[it.ID]:
		mark = sSelected.Render("●")
	}
	l1 := mark + st(kindStyle(it.Kind)).Render(it.Kind.Tag()) + plain.Render(" ") + muted.Render(fmt.Sprintf("%d", it.ID))
	if p, ok := b.progress[it.ID]; ok {
		l1 += plain.Render(" ") + st(p.style()).Render(p.text())
	}
	right := muted.Render(initials(it.AssignedTo))
	if it.Effort > 0 {
		right = muted.Render(fmtEffort(it.Effort)+" ") + right
	}
	l1 += fill(plain, w-lipgloss.Width(l1)-lipgloss.Width(right)) + right
	l2 := plain.Render(" " + trunc(it.Title, w-1))
	l2 += fill(plain, w-lipgloss.Width(l2))
	return []string{l1, l2, ""}
}
