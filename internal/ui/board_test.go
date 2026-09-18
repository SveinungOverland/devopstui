package ui

import (
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
