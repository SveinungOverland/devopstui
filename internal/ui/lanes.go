package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/sveinungoverland/devopstui/internal/model"
)

// lane is one PBI's children, bucketed into lanes.states columns.
type lane struct {
	parent *model.WorkItem
	cols   [][]*model.WorkItem // same index space as lanes.states
}

// lanes is the Dashboard's bottom row: a kanban of "my PBIs" children with
// state as the columns (shared across every lane, like a normal board) and
// the parent PBI as the swimlane. A PBI with no children at all has no
// lane; everything else shows, not just active work, so the columns read
// as real progress (To Do / In Progress / Done, …) per PBI.
type lanes struct {
	states []string
	ls     []lane

	lane, col, row int // cursor
	offsetLane     int // first visible lane, vertical scroll
	offsetCol      int // first visible column, horizontal scroll

	selected map[int]bool
}

func newLanes() *lanes { return &lanes{selected: map[int]bool{}} }

// setLanes rebuilds the lanes from parents (the caller's pre-filtered PBIs —
// the Dashboard passes only those currently in progress, see
// App.myActivePBIs — in display order) and childrenOf (every fetched child,
// keyed by parent id). states
// sets the column order; pass nil to keep whatever was set before (e.g.
// after a purely local update that didn't refetch). Any state seen on a
// child but missing from states gets its own column at the end, so nothing
// is ever hidden.
func (ln *lanes) setLanes(parents []*model.WorkItem, childrenOf map[int][]*model.WorkItem, states []string) {
	cur := ln.currentID()
	if states != nil {
		ln.states = states
	}

	// Bugs are dropped: they carry a different set of states than the
	// Task-level columns here, and when they're at the requirement level
	// (the usual case) they already show as their own card on the kanban
	// above, not as a PBI's child.
	relevant := map[int][]*model.WorkItem{}
	for id, children := range childrenOf {
		for _, c := range children {
			if c.Kind != model.KindBug {
				relevant[id] = append(relevant[id], c)
			}
		}
	}

	order := append([]string(nil), ln.states...)
	index := map[string]int{}
	for i, s := range order {
		index[s] = i
	}
	for _, p := range parents {
		for _, c := range relevant[p.ID] {
			if _, ok := index[c.State]; !ok {
				index[c.State] = len(order)
				order = append(order, c.State)
			}
		}
	}
	ln.states = order

	ln.ls = nil
	for _, p := range parents {
		children := relevant[p.ID]
		if len(children) == 0 {
			continue
		}
		cols := make([][]*model.WorkItem, len(order))
		for _, c := range children {
			i := index[c.State]
			cols[i] = append(cols[i], c)
		}
		ln.ls = append(ln.ls, lane{parent: p, cols: cols})
	}
	for id := range ln.selected {
		if ln.find(id) == nil {
			delete(ln.selected, id)
		}
	}
	ln.clamp()
	switch {
	case cur != 0:
		ln.jumpTo(cur)
	case ln.current() == nil:
		// Nothing was selected before and the default (lane 0, column 0)
		// cell is empty: land on the first card there is.
		ln.jumpToFirstCard()
	}
}

func (ln *lanes) jumpToFirstCard() {
	for li, l := range ln.ls {
		for ci, col := range l.cols {
			if len(col) > 0 {
				ln.lane, ln.col, ln.row = li, ci, 0
				return
			}
		}
	}
}

func (ln *lanes) find(id int) *model.WorkItem {
	for _, l := range ln.ls {
		for _, col := range l.cols {
			for _, it := range col {
				if it.ID == id {
					return it
				}
			}
		}
	}
	return nil
}

// cell is the cards at (lane, col), nil when out of range.
func (ln *lanes) cell(lane, col int) []*model.WorkItem {
	if lane < 0 || lane >= len(ln.ls) {
		return nil
	}
	if col < 0 || col >= len(ln.ls[lane].cols) {
		return nil
	}
	return ln.ls[lane].cols[col]
}

func (ln *lanes) clamp() {
	if len(ln.ls) == 0 || len(ln.states) == 0 {
		ln.lane, ln.col, ln.row = 0, 0, 0
		return
	}
	ln.lane = min(max(ln.lane, 0), len(ln.ls)-1)
	ln.col = min(max(ln.col, 0), len(ln.states)-1)
	ln.row = min(max(ln.row, 0), max(len(ln.cell(ln.lane, ln.col))-1, 0))
}

// adjacentState is the state dc columns away from the current one, or ""
// when out of range.
func (ln *lanes) adjacentState(dc int) (string, bool) {
	i := ln.col + dc
	if i < 0 || i >= len(ln.states) {
		return "", false
	}
	return ln.states[i], true
}

func (ln *lanes) jumpTo(id int) {
	for li, l := range ln.ls {
		for ci, col := range l.cols {
			for ri, it := range col {
				if it.ID == id {
					ln.lane, ln.col, ln.row = li, ci, ri
					return
				}
			}
		}
	}
}

// current is the highlighted card, nil when its cell is empty.
func (ln *lanes) current() *model.WorkItem {
	cell := ln.cell(ln.lane, ln.col)
	if ln.row >= len(cell) {
		return nil
	}
	return cell[ln.row]
}

func (ln *lanes) currentID() int {
	if it := ln.current(); it != nil {
		return it.ID
	}
	return 0
}

func (ln *lanes) targets() []int {
	if len(ln.selected) > 0 {
		var ids []int
		for _, l := range ln.ls {
			for _, col := range l.cols {
				for _, it := range col {
					if ln.selected[it.ID] {
						ids = append(ids, it.ID)
					}
				}
			}
		}
		return ids
	}
	if it := ln.current(); it != nil {
		return []int{it.ID}
	}
	return nil
}

func (ln *lanes) targetItems() []*model.WorkItem {
	want := map[int]bool{}
	for _, id := range ln.targets() {
		want[id] = true
	}
	var out []*model.WorkItem
	for _, l := range ln.ls {
		for _, col := range l.cols {
			for _, it := range col {
				if want[it.ID] {
					out = append(out, it)
				}
			}
		}
	}
	return out
}

func (ln *lanes) toggleSelect() {
	if it := ln.current(); it != nil {
		if ln.selected[it.ID] {
			delete(ln.selected, it.ID)
		} else {
			ln.selected[it.ID] = true
		}
		ln.move(1, 0)
	}
}

// move: dCol switches the state column (left/right, shared across every
// lane, like a normal board). dRow scans down (or up) the current
// column's cards, one at a time — within the current lane's cell first,
// then spilling into the next/previous lane's cell for that same column —
// so up/down reads as one continuous scroll through a column instead of
// jumping straight to another lane regardless of how many cards are here.
func (ln *lanes) move(dRow, dCol int) {
	if dCol != 0 {
		ln.col = min(max(ln.col+dCol, 0), len(ln.states)-1)
		ln.row = 0
		return
	}
	dir := 1
	if dRow < 0 {
		dir = -1
	}
	for n := dRow * dir; n > 0; n-- {
		if !ln.step(dir) {
			break
		}
	}
}

// step moves one card forward (dir=1) or backward (dir=-1) through the
// current column: first within the current lane's cell, then into the
// next/previous lane that has anything in this column. Reports whether it
// moved at all (false at either end of the column).
func (ln *lanes) step(dir int) bool {
	if r := ln.row + dir; r >= 0 && r < len(ln.cell(ln.lane, ln.col)) {
		ln.row = r
		return true
	}
	for l := ln.lane + dir; l >= 0 && l < len(ln.ls); l += dir {
		cell := ln.cell(l, ln.col)
		if len(cell) == 0 {
			continue
		}
		ln.lane = l
		if dir > 0 {
			ln.row = 0
		} else {
			ln.row = len(cell) - 1
		}
		return true
	}
	return false
}

// ---------------------------------------------------------------- render

const laneColMinW = 16

func (ln *lanes) view(width, height int, focused bool) string {
	style := sPanel
	if focused {
		style = sPanelFocus
	}
	inner := max(width-4, 20)
	innerH := max(height-2, 3)
	if len(ln.ls) == 0 || len(ln.states) == 0 {
		msg := sMuted.Render("no children on your PBIs yet")
		return style.Width(width - 2).Height(height - 2).Render(msg)
	}

	// Columns stretch to fill the full width when they all fit; only once
	// there are enough of them that they'd drop below the minimum does
	// scrolling kick in, same idea as the kanban above but without an
	// upper cap, so a handful of columns never leaves the rest of the row
	// blank.
	colW := max(inner/max(len(ln.states), 1), laneColMinW)
	visibleCols := max(inner/colW, 1)
	if ln.col < ln.offsetCol {
		ln.offsetCol = ln.col
	}
	if ln.col >= ln.offsetCol+visibleCols {
		ln.offsetCol = ln.col - visibleCols + 1
	}
	startCol := ln.offsetCol
	endCol := min(startCol+visibleCols, len(ln.states))

	// Every card shows (no more "+N" collapsing), so a lane's height is
	// however many rows its busiest visible column needs, plus its title
	// line. Scroll just enough lanes into view to keep the cursor's lane
	// visible, rather than assuming a fixed height per lane.
	heights := make([]int, len(ln.ls))
	for i, l := range ln.ls {
		rows := 1
		for ci := startCol; ci < endCol; ci++ {
			rows = max(rows, len(l.cols[ci]))
		}
		heights[i] = 1 + rows // title line + card rows
	}
	bodyH := max(innerH-1, 2) // -1 for the shared header row
	if ln.lane < ln.offsetLane {
		ln.offsetLane = ln.lane
	}
	for ln.offsetLane < ln.lane {
		sum := 0
		for i := ln.offsetLane; i <= ln.lane; i++ {
			sum += heights[i] + 1 // +1 separator between lanes
		}
		if sum <= bodyH {
			break
		}
		ln.offsetLane++
	}

	lines := []string{ln.renderHeader(colW, startCol, endCol)}
	for li := ln.offsetLane; li < len(ln.ls); li++ {
		block := ln.renderLane(li, colW, startCol, endCol, focused)
		if li > ln.offsetLane && len(lines)-1+len(block) > bodyH {
			break // the next lane doesn't fit; stop rather than clip it
		}
		lines = append(lines, block...)
		if li < len(ln.ls)-1 {
			lines = append(lines, "")
		}
	}
	for len(lines) < innerH {
		lines = append(lines, "")
	}
	return style.Width(width - 2).Height(height - 2).Render(strings.Join(lines[:min(len(lines), innerH)], "\n"))
}

func (ln *lanes) renderHeader(colW, start, end int) string {
	var b strings.Builder
	for ci := start; ci < end; ci++ {
		count := 0
		for _, l := range ln.ls {
			count += len(l.cols[ci])
		}
		style := sMuted
		if ci == ln.col {
			style = sHeader
		}
		text := style.Render(fmt.Sprintf("%s (%d)", ln.states[ci], count))
		b.WriteString(pad(trunc(text, colW-1), colW))
	}
	head := b.String()
	if start > 0 || end < len(ln.states) {
		head += sMuted.Render(fmt.Sprintf("  %d-%d of %d", start+1, end, len(ln.states)))
	}
	return head
}

func (ln *lanes) renderLane(li, colW, start, end int, focused bool) []string {
	l := ln.ls[li]
	curLane := li == ln.lane
	totalW := colW * (end - start)

	tag := kindStyle(l.parent.Kind).Render(l.parent.Kind.Tag())
	id := sMuted.Render(fmt.Sprintf(" #%d ", l.parent.ID))
	titleStyle := sText
	if curLane && focused {
		titleStyle = sHeader
	}
	total := 0
	for _, c := range l.cols {
		total += len(c)
	}
	title := titleStyle.Render(trunc(l.parent.Title, max(totalW-28, 4)))
	head := tag + id + title + sMuted.Render(fmt.Sprintf(" (%d)", total))
	head = pad(trunc(head, totalW), totalW)

	rows := 1
	for ci := start; ci < end; ci++ {
		rows = max(rows, len(l.cols[ci]))
	}
	lines := []string{head}
	for r := 0; r < rows; r++ {
		var row strings.Builder
		for ci := start; ci < end; ci++ {
			cellFocused := curLane && ci == ln.col && focused && r == ln.row
			row.WriteString(ln.renderCellRow(li, ci, r, colW, cellFocused))
		}
		lines = append(lines, row.String())
	}
	return lines
}

// renderCellRow is the card at row r of column ci in lane li, or a blank
// slot when that cell has fewer than r+1 cards. Every card shows — cells
// no longer collapse extras into a "+N" badge.
func (ln *lanes) renderCellRow(li, ci, r, w int, cur bool) string {
	cell := ln.ls[li].cols[ci]
	if r >= len(cell) {
		return pad("", w)
	}
	return pad(ln.renderCard(cell[r], w, cur), w)
}

func (ln *lanes) renderCard(it *model.WorkItem, w int, cur bool) string {
	st := rowStyler(cur)
	plain := st(lipgloss.NewStyle())
	muted := st(sMuted)
	mark := plain.Render(" ")
	switch {
	case cur && ln.selected[it.ID]:
		mark = st(sSelected).Render("●")
	case cur:
		mark = st(sKey).Render(cursorMark)
	case ln.selected[it.ID]:
		mark = sSelected.Render("●")
	}
	tag := st(kindStyle(it.Kind)).Render(it.Kind.Tag())
	idS := muted.Render(fmt.Sprintf("%d", it.ID))
	who := muted.Render(initials(it.AssignedTo))
	line := mark + tag + plain.Render(" ") + idS + plain.Render(" ")
	titleW := w - lipgloss.Width(line) - lipgloss.Width(who) - 1
	line += plain.Render(trunc(it.Title, max(titleW, 1)))
	line += fill(plain, w-lipgloss.Width(line)-lipgloss.Width(who)) + who
	return line
}
