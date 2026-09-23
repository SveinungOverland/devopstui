package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

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
