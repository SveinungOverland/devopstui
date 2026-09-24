package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// cursorID is the highlighted item's id, 0 when there is none.
func cursorID(a *App) int {
	if it := a.currentItem(); it != nil {
		return it.ID
	}
	return 0
}

func TestQuitAlwaysQuits(t *testing.T) {
	q := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}
	quits := func(h *harness, where string) {
		t.Helper()
		cmd := h.app.onKey(q)
		if cmd == nil {
			t.Fatalf("%s: q returned no command", where)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s: q should quit", where)
		}
	}

	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1013)
	h.keys("D", "enter")
	quits(h, "drill-down")

	h = newHarness(t, 160, 45)
	h.keys("space")
	quits(h, "with a selection")

	h = newHarness(t, 160, 45)
	h.keys("tab")
	if !h.app.focusDetail {
		t.Fatal("tab should focus the preview")
	}
	quits(h, "preview focused")
}

func TestJumpListWalksDrillDown(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1013)
	h.keys("D", "enter") // PBI 1013, then its task 1015
	if h.app.item.item.ID != 1015 {
		t.Fatalf("drill failed: %d", h.app.item.item.ID)
	}

	h.keys("ctrl+o")
	if h.app.view != viewItem || h.app.item.item.ID != 1013 || len(h.app.itemStack) != 1 {
		t.Fatalf("ctrl+o should walk back to the PBI, got %v stack=%d", h.app.view, len(h.app.itemStack))
	}
	if !h.app.item.focusKan || cursorID(h.app) != 1015 {
		t.Errorf("the PBI's kanban cursor should be back on 1015, got %d", cursorID(h.app))
	}
	h.keys("ctrl+o")
	if h.app.view != viewSprint || cursorID(h.app) != 1013 {
		t.Fatalf("ctrl+o should land on the sprint row, got %v #%d", h.app.view, cursorID(h.app))
	}
	h.keys("ctrl+o")
	if h.app.view != viewSprint || h.app.flash != "no earlier jump" {
		t.Fatalf("ctrl+o past the start: %v %q", h.app.view, h.app.flash)
	}

	h.keys("ctrl+n")
	if h.app.view != viewItem || h.app.item.item.ID != 1013 {
		t.Fatalf("ctrl+n should re-enter the PBI, got %v", h.app.view)
	}
	h.keys("ctrl+n")
	if h.app.item.item.ID != 1015 || len(h.app.itemStack) != 2 {
		t.Fatalf("ctrl+n should re-enter the task, got #%d stack=%d", h.app.item.item.ID, len(h.app.itemStack))
	}
	h.keys("ctrl+n")
	if h.app.flash != "no later jump" {
		t.Fatalf("ctrl+n past the end: %q", h.app.flash)
	}
	// The restored trail still walks out with esc.
	h.keys("esc", "esc")
	if h.app.view != viewSprint {
		t.Fatalf("esc should walk out to the sprint, got %v", h.app.view)
	}
	// And ctrl+o undoes that esc.
	h.keys("ctrl+o")
	if h.app.view != viewItem || h.app.item.item.ID != 1013 {
		t.Fatalf("ctrl+o after esc should reopen the PBI, got %v", h.app.view)
	}
}

func TestJumpListTabSwitches(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1013)
	h.keys("3")
	if h.app.view != viewBoard {
		t.Fatalf("view = %v", h.app.view)
	}
	h.keys("ctrl+o")
	if h.app.view != viewSprint || cursorID(h.app) != 1013 {
		t.Fatalf("ctrl+o should return to the sprint row, got %v #%d", h.app.view, cursorID(h.app))
	}
	h.keys("ctrl+n")
	if h.app.view != viewBoard {
		t.Fatalf("ctrl+n should return to the board, got %v", h.app.view)
	}
	// Plain cursor moves are not jumps.
	h.keys("2", "j", "j", "ctrl+o")
	if h.app.view != viewBoard {
		t.Fatalf("ctrl+o after j/k should skip to the board, got %v", h.app.view)
	}
}

func TestGotoMotions(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1013)

	h.keys("g", "b")
	if h.app.view != viewBoard || cursorID(h.app) != 1013 {
		t.Fatalf("gb: %v #%d", h.app.view, cursorID(h.app))
	}
	h.keys("g", "s")
	if h.app.view != viewSprint || cursorID(h.app) != 1013 {
		t.Fatalf("gs: %v #%d", h.app.view, cursorID(h.app))
	}
	h.keys("g", "d")
	if h.app.view != viewDash || cursorID(h.app) != 1013 {
		t.Fatalf("gd: %v #%d (%q)", h.app.view, cursorID(h.app), h.app.flash)
	}
	h.keys("g", "B")
	if h.app.view != viewBacklog {
		t.Fatalf("gB: %v", h.app.view)
	}
	if cursorID(h.app) != 1013 && h.app.flash != "#1013 is not in the Backlog view" {
		t.Fatalf("gB: #%d %q", cursorID(h.app), h.app.flash)
	}
	// The goto trail unwinds with ctrl+o.
	h.keys("ctrl+o", "ctrl+o")
	if h.app.view != viewSprint || cursorID(h.app) != 1013 {
		t.Fatalf("ctrl+o ctrl+o: %v #%d", h.app.view, cursorID(h.app))
	}

	// gp drills into the parent, and again from inside the drill-down.
	h.keys("g", "p")
	if h.app.view != viewItem || h.app.item.item.ID != 1012 {
		t.Fatalf("gp: %v", h.app.view)
	}
	h.keys("g", "p")
	if h.app.item.item.ID != 1010 {
		t.Fatalf("gp from the drill-down: #%d", h.app.item.item.ID)
	}
	h.keys("g", "p")
	if h.app.item.item.ID != 1010 || h.app.flash != "#1010 has no parent" {
		t.Fatalf("gp on an Epic: #%d %q", h.app.item.item.ID, h.app.flash)
	}
	h.keys("ctrl+o", "ctrl+o")
	if h.app.view != viewSprint || cursorID(h.app) != 1013 {
		t.Fatalf("ctrl+o back from gp: %v #%d", h.app.view, cursorID(h.app))
	}

	// gg goes to the top; an unknown second key cancels g silently.
	h.keys("G")
	h.keys("g", "g")
	if h.app.sprint.cursor != 0 {
		t.Fatalf("gg: cursor = %d", h.app.sprint.cursor)
	}
	h.keys("g", "z", "j")
	if h.app.pendingGoto || h.app.sprint.cursor != 1 {
		t.Fatalf("g then an unknown key should cancel: pending=%v cursor=%d", h.app.pendingGoto, h.app.sprint.cursor)
	}
	h.keys("3", "G", "g", "g")
	if h.app.board.row != 0 {
		t.Fatalf("gg on the board: row = %d", h.app.board.row)
	}
}

func TestGotoParentFetchesUnloaded(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1013)
	h.keys("D")
	// Forget every loaded copy of the parent so gp has to fetch it.
	h.app.sprint, h.app.backlog = newList(""), newList("")
	h.app.myItems, h.app.dashChildren = nil, nil
	if h.app.lookup(1012) != nil {
		t.Fatal("parent should be unknown")
	}
	h.keys("g", "p")
	if h.app.view != viewItem || h.app.item.item.ID != 1012 {
		t.Fatalf("gp should fetch and open #1012, got %v %q", h.app.view, h.app.flash)
	}
}
