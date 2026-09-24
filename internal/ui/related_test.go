package ui

import (
	"strings"
	"testing"

	"github.com/sveinungoverland/devopstui/internal/model"
)

func relatedIDs(v *itemView) []int {
	var ids []int
	for _, r := range v.related() {
		ids = append(ids, r.Item.ID)
	}
	return ids
}

func TestItemRelatedPanel(t *testing.T) {
	h := newHarness(t, 160, 45)
	a := h.app
	a.sprint.jumpTo(1013) // demo links and a "#1015" comment, see ado.NewFake
	h.keys("D")
	// Links first, grouped by relation, then mentions in order of appearance.
	if got, want := relatedIDs(a.item), []int{1020, 1014, 1015}; !equalInts(got, want) {
		t.Fatalf("related rows = %v, want %v", got, want)
	}
	// The panel is always on screen, above the kanban, without a toggle.
	v := h.app.View()
	h.dump("item-related")
	for _, want := range []string{"Related (3)", "Successors", "Mentioned", "Trace dashboard in Grafana", "Spans lost when retry", "Description", "Children (3)"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
	if strings.Index(v, "Related (3)") > strings.Index(v, "Children (3)") {
		t.Error("Related should sit above the kanban")
	}

	h.keys("x")
	if !a.item.focusRel || a.item.focusKan {
		t.Fatalf("x should focus Related, got focusRel=%v focusKan=%v", a.item.focusRel, a.item.focusKan)
	}
	if !strings.Contains(h.app.View(), "related 1/3") {
		t.Error("header should say Related has focus")
	}
	if a.item.current().ID != 1020 {
		t.Errorf("actions should target the highlighted related item, got #%d", a.item.current().ID)
	}

	h.keys("j", "j", "enter")
	if a.item.item.ID != 1015 {
		t.Fatalf("enter should drill into the mention #1015, got #%d", a.item.item.ID)
	}
	if got, want := relatedIDs(a.item), []int{1016}; !equalInts(got, want) {
		t.Fatalf("#1015 related = %v, want %v", got, want)
	}
	if a.item.links[0].Kind != model.RelSuccessor {
		t.Errorf("#1016 should be a successor of #1015, got %v", a.item.links[0].Kind)
	}

	h.keys("esc")
	if a.item.item.ID != 1013 {
		t.Fatalf("esc should walk back to #1013, got #%d", a.item.item.ID)
	}
	if !a.item.focusRel || a.item.relRow != 2 {
		t.Errorf("esc should come back to the Related row drilled from, got focusRel=%v relRow=%d", a.item.focusRel, a.item.relRow)
	}

	h.keys("x")
	if !a.item.focusDesc() {
		t.Error("x on Related should hand focus back to the description")
	}
}

// x from the kanban must not be mistaken for a column move: it moves focus
// and writes nothing.
func TestItemRelatedFromKanban(t *testing.T) {
	h := newHarness(t, 160, 45)
	a := h.app
	a.sprint.jumpTo(1013)
	h.keys("D")
	if !a.item.focusKan {
		t.Fatal("#1013 has children, so the kanban should start focused")
	}
	h.keys("x", "j", "D")
	if len(h.fake.Updates) != 0 {
		t.Fatalf("no write expected, got %+v", h.fake.Updates)
	}
	if a.item.item.ID != 1014 {
		t.Fatalf("D on the second Related row should open #1014, got #%d", a.item.item.ID)
	}
}

// Mentions can live in comments, which may land after the item is shown;
// the Related panel has to pick them up then, resolving ids no loaded list
// has and dropping ones that do not exist.
func TestItemMentionsFromLateComments(t *testing.T) {
	h := newHarness(t, 160, 45)
	a := h.app
	a.sprint.jumpTo(1013)
	h.keys("D")
	h.send(commentsLoadedMsg{id: 1013, since: a.commentsGen[1013], comments: []model.Comment{
		{ID: 1, Author: "Alex Kim", Text: "Blocked on #1007, and #1020 is already linked. #99999 is gone."},
	}})
	if got, want := relatedIDs(a.item), []int{1020, 1014, 1007}; !equalInts(got, want) {
		t.Fatalf("related rows = %v, want %v", got, want)
	}
	if !strings.Contains(h.app.View(), "Choose workspace template") {
		t.Errorf("view should show the late mention #1007:\n%s", h.app.View())
	}
}

// With nothing related the panel shrinks to its title, and tab skips it.
func TestItemRelatedEmpty(t *testing.T) {
	h := newHarness(t, 160, 45)
	a := h.app
	a.sprint.jumpTo(1021)
	h.keys("D")
	if n := len(a.item.related()); n != 0 {
		t.Fatalf("#1021 should have nothing related, got %d", n)
	}
	if !strings.Contains(h.app.View(), "no linked or mentioned items") {
		t.Errorf("empty Related panel should say so:\n%s", h.app.View())
	}
	h.keys("tab")
	if a.item.focusRel || !a.item.focusKan {
		t.Error("tab should skip an empty Related panel")
	}
	h.keys("x", "enter")
	if a.item.item.ID != 1021 {
		t.Error("enter on an empty Related panel should stay put")
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
