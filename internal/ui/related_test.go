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

func TestItemRelatedPane(t *testing.T) {
	h := newHarness(t, 160, 45)
	a := h.app
	a.sprint.jumpTo(1013) // demo links and a "#1015" comment, see ado.NewFake
	h.keys("D")
	if a.item.showRelated {
		t.Fatal("drill-down should start on the description")
	}

	h.keys("x")
	if !a.item.showRelated || a.item.focusKan {
		t.Fatalf("x should show Related with focus on it, got showRelated=%v focusKan=%v", a.item.showRelated, a.item.focusKan)
	}
	// Links first, grouped by relation, then mentions in order of appearance.
	if got, want := relatedIDs(a.item), []int{1020, 1014, 1015}; !equalInts(got, want) {
		t.Fatalf("related rows = %v, want %v", got, want)
	}
	v := h.app.View()
	h.dump("item-related")
	for _, want := range []string{"Related (3)", "Successors", "Mentioned", "Trace dashboard in Grafana", "Spans lost when retry", "related 1/3"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
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
	if !a.item.showRelated || a.item.relRow != 2 {
		t.Errorf("esc should come back to the Related row drilled from, got showRelated=%v relRow=%d", a.item.showRelated, a.item.relRow)
	}

	h.keys("C")
	if a.item.showRelated || !a.item.showComments {
		t.Error("C should swap Related for the discussion")
	}
	h.keys("x", "x")
	if a.item.showRelated || a.item.showComments {
		t.Error("x twice should land back on the description")
	}
}

// x from the kanban must not be mistaken for a column move: it switches
// the left pane and takes focus, and writes nothing.
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
// the Related pane has to pick them up then, resolving ids no loaded list
// has and dropping ones that do not exist.
func TestItemMentionsFromLateComments(t *testing.T) {
	h := newHarness(t, 160, 45)
	a := h.app
	a.sprint.jumpTo(1013)
	h.keys("D", "x")
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

func TestItemRelatedEmpty(t *testing.T) {
	h := newHarness(t, 160, 45)
	a := h.app
	a.sprint.jumpTo(1021)
	h.keys("D", "x")
	if n := len(a.item.related()); n != 0 {
		t.Fatalf("#1021 should have nothing related, got %d", n)
	}
	if !strings.Contains(h.app.View(), "no linked or mentioned items") {
		t.Errorf("empty Related pane should say so:\n%s", h.app.View())
	}
	h.keys("enter")
	if a.item.item.ID != 1021 {
		t.Error("enter on an empty Related pane should stay put")
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
