package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/sveinungoverland/devopstui/internal/model"
)

// TestLaneCardDimsOtherSprint checks the rendering side of issue #46: a
// card outside the selected sprint has its title drawn in the muted
// style, a card inside it does not, and nothing is dimmed when no sprint
// is selected.
func TestLaneCardDimsOtherSprint(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	here := &model.WorkItem{ID: 1, Kind: model.KindTask, Title: "planned here", IterationPath: `P\Sprint 1`}
	there := &model.WorkItem{ID: 2, Kind: model.KindTask, Title: "planned there", IterationPath: `P\Sprint 2`}
	dimmed := func(ln *lanes, it *model.WorkItem) bool {
		return strings.Contains(ln.renderCard(it, 60, false), sMuted.Render(it.Title))
	}

	ln := newLanes()
	ln.sprint = `P\Sprint 1`
	if dimmed(ln, here) {
		t.Error("a card in the selected sprint should not have a muted title")
	}
	if !dimmed(ln, there) {
		t.Error("a card planned for another sprint should have a muted title")
	}

	ln.sprint = ""
	if dimmed(ln, there) {
		t.Error("with no sprint selected, nothing should be dimmed")
	}
}
