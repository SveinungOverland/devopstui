package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// bgCode is the SGR sequence for the dark cursor background (#264f78).
const bgCode = "48;2;38;79;120"

// segmentsWithBG counts styled segments on a line that carry the cursor
// background. Every visible run of text on a highlighted row must have it,
// or the highlight breaks at the first coloured piece (the bug this guards).
func segmentsWithBG(line string) (withBG, total int) {
	for _, seg := range strings.Split(line, "\x1b[0m") {
		if strings.TrimSpace(ansi.Strip(seg)) == "" {
			continue
		}
		total++
		if strings.Contains(seg, bgCode) {
			withBG++
		}
	}
	return
}

func TestCursorRowFullyHighlighted(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	h := newHarness(t, 160, 45)
	lipgloss.SetColorProfile(termenv.TrueColor) // harness resets it
	lipgloss.SetHasDarkBackground(true)
	a := h.app
	a.sprint.jumpTo(1013) // row with tag, id, badge, state, assignee, effort

	lines := strings.Split(a.sprint.view(90, 30), "\n")
	cur := lines[a.sprint.cursor]
	withBG, total := segmentsWithBG(cur)
	if total < 5 || withBG != total {
		t.Fatalf("cursor row: %d of %d segments carry the background:\n%q", withBG, total, cur)
	}
	if w := ansi.StringWidth(cur); w != 90 {
		t.Errorf("cursor row width = %d, want 90 (padding must be highlighted too)", w)
	}
	if !strings.Contains(ansi.Strip(cur), cursorMark) {
		t.Error("cursor row should start with the edge marker")
	}
	// A non-cursor row must not carry the background.
	other := lines[a.sprint.cursor+1]
	if strings.Contains(other, bgCode) {
		t.Error("non-cursor row must not be highlighted")
	}

	// Board cards.
	h.keys("3")
	a.board.jumpTo(1014) // card with a progress badge
	board := a.board.view(150, 30, true)
	found := false
	for _, l := range strings.Split(board, "\n") {
		if strings.Contains(l, bgCode) && strings.Contains(ansi.Strip(l), "1014") {
			withBG, total := segmentsWithBG(l)
			// The line also holds other columns' cards; only demand that the
			// highlighted card's pieces are consistent.
			if withBG < 4 {
				t.Errorf("board card row has %d/%d highlighted segments:\n%q", withBG, total, l)
			}
			found = true
		}
	}
	if !found {
		t.Error("no highlighted board card found")
	}

	// Picker rows.
	h.keys("2", "s")
	pk := a.popup.(*picker)
	pv := pk.View(120, 40)
	for _, l := range strings.Split(pv, "\n") {
		if strings.Contains(ansi.Strip(l), cursorMark) {
			if withBG, total := segmentsWithBG(l); withBG < total-2 { // the two popup border glyphs are unstyled
				t.Errorf("picker row %d/%d highlighted:\n%q", withBG, total, l)
			}
		}
	}
}
