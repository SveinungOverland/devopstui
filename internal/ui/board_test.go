package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/sveinungoverland/devopstui/internal/config"
	"github.com/sveinungoverland/devopstui/internal/model"
)

// TestBoardViewFillsWidth guards against the columns stopping short of the
// requested width once colW hits an upper cap: with few enough columns that
// they all fit comfortably above the minimum width, the board must stretch
// them to use every column of the terminal rather than leaving a blank strip.
func TestBoardViewFillsWidth(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	b := newBoard()
	b.def = model.Board{Columns: []model.BoardColumn{
		{Name: "To Do"}, {Name: "Doing"}, {Name: "Done"},
	}}
	b.cols = [][]*model.WorkItem{
		{{ID: 1, Title: "one"}},
		{{ID: 2, Title: "two"}},
		{{ID: 3, Title: "three"}},
	}

	const width = 300
	got := lipgloss.Width(b.view(width, 20, true))
	if got != width {
		t.Errorf("board.view(%d, ...) rendered width %d, want %d (columns should stretch to fill the row, not cap at 44 cols each)", width, got, width)
	}
}

// TestBoardScrollHintMatchesBody guards the header's "columns a-b of n"
// against describing a different frame than the body below it. The header
// renders first, so the scroll window must be settled before it: on the
// very first render (nothing drawn yet) and right after the cursor moves
// past the visible columns.
func TestBoardScrollHintMatchesBody(t *testing.T) {
	h := newHarness(t, 140, 40)
	h.keys("3") // Board: 5 columns, 3 fit next to the preview at 140
	check := func(wantHint, wantFirstCol string) {
		t.Helper()
		lines := strings.Split(h.app.View(), "\n")
		if !strings.Contains(lines[1], wantHint) {
			t.Errorf("header = %q, want it to say %q", lines[1], wantHint)
		}
		if !strings.HasPrefix(strings.TrimLeft(lines[3], "│ "), wantFirstCol) {
			t.Errorf("first board column = %q, want %q", lines[3], wantFirstCol)
		}
	}
	check("columns 1-3 of 5", "New")
	h.keys("l", "l", "l") // onto In Progress, which scrolls the board by one
	check("columns 2-4 of 5", "Approved")

	// The Dashboard's kanban is the same board type behind the same header.
	h.keys("1")
	if header := strings.Split(h.app.View(), "\n")[1]; !strings.Contains(header, "kanban · columns 1-3 of 5") {
		t.Errorf("dashboard header = %q, want the kanban's scroll window", header)
	}
}

// TestBoardListNavigation walks the list layout: j and k cross column
// headings, h and l jump between columns with cards, and empty columns
// are skipped because the list has no row for them.
func TestBoardListNavigation(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	b := newBoard()
	b.def = model.Board{Columns: []model.BoardColumn{
		{Name: "To Do"}, {Name: "Doing"}, {Name: "Review"}, {Name: "Done"},
	}}
	b.cols = [][]*model.WorkItem{
		{{ID: 1, Title: "one"}, {ID: 2, Title: "two"}},
		nil,
		{{ID: 3, Title: "three"}},
		{{ID: 4, Title: "four"}},
	}
	b.list = true
	want := func(id int) {
		t.Helper()
		if got := b.currentID(); got != id {
			t.Fatalf("cursor on #%d, want #%d", got, id)
		}
	}
	want(1)
	b.move(0, 1)
	want(2)
	b.move(0, 1) // across the empty Doing column
	want(3)
	b.move(0, -1)
	want(2)
	b.move(1, 0) // l: first card of the next column with any
	want(3)
	b.move(1, 0)
	want(4)
	b.move(1, 0) // nothing past Done
	want(4)
	b.move(-1, 0)
	want(3)
	b.move(0, -1<<20)
	want(1)
	b.move(0, 1<<20)
	want(4)

	// A cursor left on an empty column (from the kanban) moves to a card.
	b.col, b.row = 1, 0
	b.clamp()
	want(3)

	view := b.view(60, 12, true)
	for _, s := range []string{"To Do 2", "Review 1", "Done 1", "three"} {
		if !strings.Contains(view, s) {
			t.Errorf("list should show %q:\n%s", s, view)
		}
	}
	if strings.Contains(view, "Doing") {
		t.Errorf("an empty column should be left out of the list:\n%s", view)
	}
	if got := lipgloss.Width(view); got != 60 {
		t.Errorf("list rendered width %d, want 60", got)
	}
	if b.scrollHint() != "" {
		t.Error("the list never scrolls columns, so it has no column hint")
	}
}

// TestBoardToggleList switches the Board to the list and back with f,
// keeping the cursor and remembering the choice.
func TestBoardToggleList(t *testing.T) {
	h := newHarness(t, 140, 40)
	a := h.app
	h.keys("3", "l")
	cur := a.board.currentID()
	h.keys("f")
	if !a.board.list || !a.cfg.BoardList {
		t.Fatal("f should switch the Board to a list and remember it")
	}
	if a.board.currentID() != cur {
		t.Errorf("switching layout should keep the cursor, got #%d want #%d", a.board.currentID(), cur)
	}
	view := h.app.View()
	h.dump("24-board-list")
	if !strings.Contains(strings.Split(view, "\n")[1], "list") {
		t.Errorf("header should say the Board is a list: %q", strings.Split(view, "\n")[1])
	}
	if strings.Contains(view, "columns 1-") {
		t.Error("the list shows every column, so no column scroll hint")
	}
	for _, c := range a.board.cols {
		for _, it := range c {
			if !strings.Contains(view, fmt.Sprintf(" %d ", it.ID)) {
				t.Errorf("list should show #%d", it.ID)
			}
		}
	}

	// H/L still move the card to the adjacent column.
	h.keys("L")
	if len(h.fake.Updates) != 1 {
		t.Fatalf("column move updates = %+v", h.fake.Updates)
	}
	if a.board.currentID() != cur {
		t.Errorf("cursor should follow the moved card, got #%d", a.board.currentID())
	}

	h.keys("f")
	if a.board.list || a.cfg.BoardList {
		t.Error("f should switch back to the kanban")
	}

	// The layout is restored from the config on start.
	h2 := newHarnessWithConfig(t, 140, 40, config.Config{Org: "https://dev.azure.com/demo", Project: "Platform", Team: "Team Blue", BoardList: true}, os.DevNull)
	if !h2.app.board.list {
		t.Error("board_list in the config should start the Board as a list")
	}
	if h2.app.dashBoard.list {
		t.Error("the Dashboard's kanban keeps its layout")
	}
}
