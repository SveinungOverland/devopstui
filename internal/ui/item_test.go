package ui

import (
	"strings"
	"testing"
)

// cardIDs returns the child ids per kanban column.
func cardIDs(v *itemView) map[string][]int {
	out := map[string][]int{}
	for i, col := range v.cols {
		for _, it := range col {
			out[v.states[i]] = append(out[v.states[i]], it.ID)
		}
	}
	return out
}

func TestItemViewOpensWithDescriptionAndKanban(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1013) // PBI with tasks 1015 To Do, 1016 Done, 1017 To Do
	h.keys("D")

	if h.app.view != viewItem || h.app.item == nil {
		t.Fatalf("view = %v, item = %v", h.app.view, h.app.item)
	}
	v := h.app.item
	if v.item.ID != 1013 {
		t.Fatalf("opened item %d", v.item.ID)
	}
	h.dump("27-item-view")
	out := h.app.View()

	// Metadata header.
	for _, want := range []string{"Product Backlog Item", "#1013", "Propagate trace id", "In Progress", "Sprint 42", "1/3 tasks"} {
		if !strings.Contains(out, want) {
			t.Errorf("header missing %q", want)
		}
	}
	// Description, rendered as markdown (the fake's is a plain paragraph).
	if !strings.Contains(out, "Description") || !strings.Contains(out, "Propagate trace id through queue workers.") {
		t.Error("description pane missing")
	}
	// Kanban columns in the task state order, cards in the right column.
	if got := v.states; len(got) < 3 || got[0] != "To Do" || got[2] != "Done" {
		t.Fatalf("column order = %v", got)
	}
	cards := cardIDs(v)
	if len(cards["To Do"]) != 2 || len(cards["Done"]) != 1 {
		t.Fatalf("cards = %v", cards)
	}
	if !strings.Contains(out, "Children (3)") {
		t.Error("kanban title missing")
	}
	if !strings.Contains(out, "Load test") || !strings.Contains(out, "1017") {
		t.Error("child card missing")
	}
}

func TestItemViewBugPanelTitleMatchesNarrative(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1022) // Bug with Repro Steps and Acceptance Criteria, no Description
	h.keys("D")

	out := h.app.View()
	if !strings.Contains(out, "Repro steps & Acceptance criteria") {
		t.Errorf("item view panel title should name both narrative sections, got:\n%s", out)
	}
	if strings.Contains(out, "Description") {
		t.Errorf("item view panel should not say Description for a Bug with no Description:\n%s", out)
	}
}

func TestItemViewFocusAndNavigation(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1013)
	h.keys("D")
	v := h.app.item

	if !v.focusKan {
		t.Fatal("an item with children should start on the kanban")
	}
	if v.current().ID != 1015 {
		t.Errorf("first card = %d, want 1015", v.current().ID)
	}
	h.keys("j")
	if v.current().ID != 1017 {
		t.Errorf("j should move down the column, got %d", v.current().ID)
	}
	h.keys("l", "l")
	if v.col != 2 || v.current().ID != 1016 {
		t.Errorf("l should move columns: col=%d cur=%v", v.col, v.current())
	}
	// tab focuses the description; actions then target the item itself.
	h.keys("tab")
	if v.focusKan || v.current().ID != 1013 {
		t.Errorf("tab should focus the description and target the PBI, got %d", v.current().ID)
	}
	if !strings.Contains(h.app.View(), "description") {
		t.Error("header should say which pane has focus")
	}
	// z gives the description the full width.
	h.keys("z")
	if !v.descOnly || strings.Contains(h.app.View(), "Children (") {
		t.Error("z should hide the kanban")
	}
	h.keys("z", "tab")
	if !v.focusKan {
		t.Error("tab should return to the kanban")
	}
}

func TestItemViewMovesChildBetweenColumns(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1013)
	h.keys("D")
	v := h.app.item

	// L moves the highlighted card one column right, which sets its state.
	h.keys("L")
	if len(h.fake.Updates) != 1 || h.fake.Updates[0].ID != 1015 || h.fake.Updates[0].Patches[0].Value != "In Progress" {
		t.Fatalf("updates = %+v", h.fake.Updates)
	}
	if cards := cardIDs(v); len(cards["In Progress"]) != 1 || cards["In Progress"][0] != 1015 {
		t.Fatalf("card did not move: %v", cards)
	}
	h.dump("28-item-view-moved")
	if v.current().ID != 1015 {
		t.Errorf("cursor should follow the moved card, got %v", v.current())
	}
	// The parent's progress line updates with it.
	if p := v.progress(); p.done != 1 || p.total != 3 {
		t.Errorf("progress = %+v", p)
	}
	// H moves it back.
	h.keys("H")
	if cards := cardIDs(v); len(cards["To Do"]) != 2 {
		t.Fatalf("card did not move back: %v", cards)
	}
}

func TestItemViewCreatesChild(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1013)
	h.keys("D")
	v := h.app.item

	h.keys("tab") // focus the description, so n targets the PBI
	h.keys("n")
	p, ok := h.app.popup.(*prompt)
	if !ok {
		t.Fatalf("expected a title prompt, got %T", h.app.popup)
	}
	if !strings.Contains(p.title, "New Task under #1013") {
		t.Errorf("prompt = %q", p.title)
	}
	h.keys("D", "o", "c", "s", "enter")

	if v.item.ID != 1013 {
		t.Fatal("creating a child must not leave the item view")
	}
	created := h.fake.Updates[len(h.fake.Updates)-1].ID
	found := false
	for _, c := range v.children {
		if c.ID == created {
			found = true
		}
	}
	if !found {
		t.Fatalf("new child %d not in the kanban: %+v", created, cardIDs(v))
	}
	if !v.focusKan || v.current().ID != created {
		t.Errorf("cursor should land on the new card, got %v", v.current())
	}
	if !strings.Contains(h.app.View(), "Docs") {
		t.Error("new card not rendered")
	}
	// It lands in the first column, since new tasks start at To Do.
	if got := cardIDs(v)["To Do"]; len(got) != 3 {
		t.Errorf("To Do column = %v", got)
	}
}

func TestItemViewDrillAndBack(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1013)
	h.keys("D")

	// Drill from the PBI into one of its tasks and back out again.
	h.keys("enter")
	if h.app.item.item.ID != 1015 || len(h.app.itemStack) != 2 {
		t.Fatalf("drill failed: item=%d stack=%d", h.app.item.item.ID, len(h.app.itemStack))
	}
	if h.app.item.parent == nil || h.app.item.parent.ID != 1013 {
		t.Error("child view should show its parent")
	}
	h.keys("esc")
	if h.app.view != viewItem || h.app.item.item.ID != 1013 {
		t.Fatalf("esc should walk back to the PBI, got %v %d", h.app.view, h.app.item.item.ID)
	}
	h.keys("esc")
	if h.app.view != viewSprint || h.app.item != nil {
		t.Fatalf("esc should return to the sprint, got %v", h.app.view)
	}
	// A tab key also leaves the drill-down.
	h.keys("D")
	h.keys("3")
	if h.app.view != viewBoard || h.app.item != nil {
		t.Fatalf("switching tabs should leave the item view, got %v", h.app.view)
	}
	// enter drills in from a board card too.
	h.keys("enter")
	if h.app.view != viewItem {
		t.Fatalf("enter on a card should drill in, got %v", h.app.view)
	}
	if h.app.itemReturn != viewBoard {
		t.Error("esc should return to the board")
	}
}

func TestItemViewWithoutChildren(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1021) // PBI with no children
	h.keys("D")
	v := h.app.item
	if v.focusKan {
		t.Error("an item without children should focus the description")
	}
	out := h.app.View()
	h.dump("29-item-view-empty")
	if !strings.Contains(out, "no children yet") || !strings.Contains(out, "to add a Task") {
		t.Error("empty kanban should invite creating a child")
	}
	if v.current().ID != 1021 {
		t.Error("actions should target the item itself")
	}
}
