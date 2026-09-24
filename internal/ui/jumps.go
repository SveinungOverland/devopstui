package ui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sveinungoverland/devopstui/internal/model"
)

// maxJumps caps the ctrl+o history, like vim's 100-entry jump list.
const maxJumps = 100

// jump is one position in the ctrl+o/ctrl+n history. For the drill-down it
// carries the whole trail, so jumping back into it leaves esc working.
type jump struct {
	view viewID
	// id is the highlighted item on a tab, or the item drilled into.
	id    int
	stack []*model.WorkItem
	panes []itemPane
	pane  itemPane
	ret   viewID
}

// same reports whether two jumps are the same place, so recording one
// drops the older copy instead of stacking duplicates.
func (j jump) same(o jump) bool { return j.view == o.view && j.id == o.id }

// here is the current position as a jump.
func (a *App) here() jump {
	if a.view == viewItem && a.item != nil {
		return jump{
			view:  viewItem,
			id:    a.item.item.ID,
			stack: append([]*model.WorkItem(nil), a.itemStack...),
			panes: append([]itemPane(nil), a.itemPanes...),
			pane:  a.item.pane(),
			ret:   a.itemReturn,
		}
	}
	j := jump{view: a.view}
	if it := a.currentItem(); it != nil {
		j.id = it.ID
	}
	return j
}

// pushJump records the current position before a jump away from it. Any
// forward history (positions ctrl+o walked back past) is dropped.
func (a *App) pushJump() {
	a.jumps = a.jumps[:min(a.jumpIdx, len(a.jumps))]
	a.record(a.here())
	a.jumpIdx = len(a.jumps)
}

func (a *App) record(j jump) {
	out := a.jumps[:0]
	for _, e := range a.jumps {
		if !e.same(j) {
			out = append(out, e)
		}
	}
	out = append(out, j)
	if len(out) > maxJumps {
		out = out[len(out)-maxJumps:]
	}
	a.jumps = out
}

// jumpBack is ctrl+o. From the newest position it first records where you
// are, so ctrl+n can come back to it.
func (a *App) jumpBack() tea.Cmd {
	if a.jumpIdx >= len(a.jumps) {
		a.record(a.here())
		a.jumpIdx = len(a.jumps) - 1
	} else {
		a.jumps[a.jumpIdx] = a.here()
	}
	if a.jumpIdx == 0 {
		return a.setFlash("no earlier jump", false)
	}
	a.jumpIdx--
	return a.restoreJump(a.jumps[a.jumpIdx])
}

// jumpForward is ctrl+n: the way back after ctrl+o.
func (a *App) jumpForward() tea.Cmd {
	if a.jumpIdx+1 >= len(a.jumps) {
		return a.setFlash("no later jump", false)
	}
	a.jumps[a.jumpIdx] = a.here()
	a.jumpIdx++
	return a.restoreJump(a.jumps[a.jumpIdx])
}

func (a *App) restoreJump(j jump) tea.Cmd {
	if j.view == viewItem && len(j.stack) > 0 {
		a.itemStack = make([]*model.WorkItem, len(j.stack))
		for i, it := range j.stack {
			a.itemStack[i] = a.fresh(it)
		}
		a.itemPanes = append([]itemPane(nil), j.panes...)
		a.itemReturn = j.ret
		a.view = viewItem
		a.focusDetail = false
		cmd := a.showItemView(a.itemStack[len(a.itemStack)-1])
		a.item.restore(j.pane)
		return cmd
	}
	return a.showIn(j.view, j.id, false)
}

// jumpView is a tab switch recorded in the jump list.
func (a *App) jumpView(v viewID) tea.Cmd {
	a.pushJump()
	return a.switchView(v)
}

// showIn switches to view v with item id highlighted. An item the view
// doesn't hold leaves the cursor where it was; report says so in a flash.
func (a *App) showIn(v viewID, id int, report bool) tea.Cmd {
	loaded := a.hasItems(v)
	cmd := a.switchView(v)
	if id == 0 {
		return cmd
	}
	if !loaded {
		a.pendingJump = id // selected once the view's load lands
		return cmd
	}
	if !a.selectIn(v, id) {
		if report {
			return tea.Batch(cmd, a.setFlash(fmt.Sprintf("#%d is not in the %s view", id, v), false))
		}
		return cmd
	}
	a.refreshDetail()
	return tea.Batch(cmd, a.loadPreviewComments(false))
}

// hasItems reports whether view v has data to select in yet.
func (a *App) hasItems(v viewID) bool {
	switch v {
	case viewSprint, viewBoard:
		return a.sprint.all != nil
	case viewBacklog:
		return a.backlog.all != nil
	case viewDash:
		return a.myItems != nil
	}
	return true
}

// selectIn moves view v's cursor to id, reporting whether it is there.
func (a *App) selectIn(v viewID, id int) bool {
	switch v {
	case viewSprint, viewBacklog:
		l := a.sprint
		if v == viewBacklog {
			l = a.backlog
		}
		l.jumpTo(id)
		if it := l.current(); it != nil && it.ID == id {
			return true
		}
		if _, ok := l.tree.Get(id); ok { // hidden under a collapsed parent
			l.expandAll()
			l.jumpTo(id)
		}
		it := l.current()
		return it != nil && it.ID == id
	case viewBoard:
		a.board.jumpTo(id)
		it := a.board.current()
		return it != nil && it.ID == id
	case viewDash:
		a.dashBoard.jumpTo(id)
		if it := a.dashBoard.current(); it != nil && it.ID == id {
			a.dashFocusLanes = false
			return true
		}
		a.dashLanes.jumpTo(id)
		if it := a.dashLanes.current(); it != nil && it.ID == id {
			a.dashFocusLanes = true
			return true
		}
	}
	return false
}

// onGotoKey completes a g sequence. Anything unrecognised cancels it.
func (a *App) onGotoKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case msg.String() == "g": // gg: every view handles top on home
		return a.onKey(tea.KeyMsg{Type: tea.KeyHome})
	case key.Matches(msg, keys.GotoDash):
		return a.gotoView(viewDash)
	case key.Matches(msg, keys.GotoSprint):
		return a.gotoView(viewSprint)
	case key.Matches(msg, keys.GotoBoard):
		return a.gotoView(viewBoard)
	case key.Matches(msg, keys.GotoBacklog):
		return a.gotoView(viewBacklog)
	case key.Matches(msg, keys.GotoParent):
		return a.gotoParent()
	}
	return nil
}

// gotoView shows the highlighted item in view v, selected.
func (a *App) gotoView(v viewID) tea.Cmd {
	id := 0
	if it := a.currentItem(); it != nil {
		id = it.ID
	}
	a.pushJump()
	return a.showIn(v, id, true)
}

type openItemMsg struct{ item *model.WorkItem }

// gotoParent drills into the parent: of the item drilled into when in the
// drill-down, otherwise of the highlighted item. A parent no view has
// loaded is fetched first.
func (a *App) gotoParent() tea.Cmd {
	it := a.currentItem()
	if a.view == viewItem && a.item != nil {
		it = a.item.item
	}
	if it == nil {
		return nil
	}
	if it.ParentID == 0 {
		return a.setFlash(fmt.Sprintf("#%d has no parent", it.ID), false)
	}
	if p := a.lookup(it.ParentID); p != nil {
		return a.openItem(p)
	}
	id := it.ParentID
	return func() tea.Msg {
		p, err := a.client.Get(context.Background(), id)
		if err != nil {
			return errMsg{err}
		}
		return openItemMsg{p}
	}
}
