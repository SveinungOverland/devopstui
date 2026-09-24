package ui

import (
	"strings"
	"testing"

	"github.com/sveinungoverland/devopstui/internal/model"
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

func TestItemViewHeaderFocusMatchesNarrativeForBug(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1022) // Bug with Repro Steps and Acceptance Criteria, no Description
	h.keys("D")

	lines := strings.Split(h.app.View(), "\n")
	tabBar := lines[1] // "1 Dashboard ... ▸ #1022 <focus summary>"
	if !strings.Contains(tabBar, "repro steps & acceptance criteria") {
		t.Errorf("tab bar should name both narrative sections for a Bug, got:\n%s", tabBar)
	}
	if strings.Contains(tabBar, "description") {
		t.Errorf("tab bar should not say description for a Bug with no Description, got:\n%s", tabBar)
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
	// tab walks the panes in screen order: Related sits above the kanban.
	h.keys("z", "tab")
	if !v.focusRel || v.current().ID != 1020 {
		t.Errorf("tab from the description should focus Related, got focusRel=%v cur=%d", v.focusRel, v.current().ID)
	}
	h.keys("tab")
	if !v.focusKan || v.focusRel {
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

func TestItemViewChildListWalksAcrossStates(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1013) // tasks 1015 To Do, 1017 To Do, 1016 Done
	h.keys("D")
	v := h.app.item
	if v.kanban {
		t.Fatal("children should default to the grouped list")
	}
	out := h.app.View()
	for _, want := range []string{"To Do 2", "Done 1", "Load test with tracing on"} {
		if !strings.Contains(out, want) {
			t.Errorf("child list missing %q", want)
		}
	}
	// In Progress has no children, so it gets no heading.
	if strings.Contains(out, "In Progress 0") {
		t.Error("an empty state should not get a heading in the list")
	}
	// j runs off the end of To Do into Done, and stops at the last row.
	h.keys("j", "j")
	if v.current().ID != 1016 {
		t.Fatalf("j should cross into the next state, got %d", v.current().ID)
	}
	if !strings.Contains(h.app.View(), "children 3/3") {
		t.Error("header should count the child's place across all states")
	}
	h.keys("j")
	if v.current().ID != 1016 {
		t.Errorf("j past the last row should stay put, got %d", v.current().ID)
	}
	// k crosses back to the last row of the previous state.
	h.keys("k")
	if v.current().ID != 1017 {
		t.Errorf("k should cross back, got %d", v.current().ID)
	}
	// h and l jump to the first row of the neighbouring state with children.
	h.keys("l")
	if v.current().ID != 1016 {
		t.Errorf("l should jump to Done, got %d", v.current().ID)
	}
	h.keys("h")
	if v.current().ID != 1015 {
		t.Errorf("h should jump to the top of To Do, got %d", v.current().ID)
	}
}

func TestItemViewPreviewsHighlightedChild(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1013)
	h.keys("D")

	out := h.app.View()
	if !strings.Contains(out, "Preview #1015") {
		t.Fatalf("the focused child should be previewed:\n%s", out)
	}
	// The item's own description keeps its place above the preview.
	if !strings.Contains(out, "Propagate trace id through queue workers.") {
		t.Error("description should stay in view beside the preview")
	}
	if !strings.Contains(out, "Parent     PBI 1013") {
		t.Error("preview should show the child's fields")
	}
	h.keys("j")
	if !strings.Contains(h.app.View(), "Preview #1017") {
		t.Error("preview should follow the cursor")
	}
	// With the description focused there is nothing to preview.
	h.keys("tab")
	if strings.Contains(h.app.View(), "Preview #") {
		t.Error("focusing the description should drop the preview")
	}
	// Related rows are previewed the same way.
	h.keys("tab")
	if !strings.Contains(h.app.View(), "Preview #1020") {
		t.Error("the highlighted related item should be previewed")
	}
}

func TestItemViewTogglesKanban(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1013)
	h.keys("D", "j", "f")
	v := h.app.item
	if !v.kanban || !h.app.cfg.ItemKanban {
		t.Fatal("f should switch the children to a kanban and remember it")
	}
	if v.current().ID != 1017 {
		t.Errorf("switching layout should keep the cursor, got %d", v.current().ID)
	}
	// Drilling into another item keeps the chosen layout.
	h.keys("esc")
	h.app.sprint.jumpTo(1002)
	h.keys("D")
	if !h.app.item.kanban {
		t.Error("the layout should carry over to the next drill-down")
	}
	h.keys("f")
	if h.app.item.kanban || h.app.cfg.ItemKanban {
		t.Error("f should switch back to the list")
	}
}

func TestKanbanNarrowsEmptyColumns(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1013) // To Do 2, In Progress 0, Done 1
	h.keys("D", "f")
	v := h.app.item
	start, end, widths := v.kanbanWidths(70)
	if start != 0 || end < 3 {
		t.Fatalf("all three columns should fit in 70, got %d-%d", start, end)
	}
	if widths[1] >= widths[0] || widths[1] >= widths[2] {
		t.Errorf("empty In Progress should be narrower than the others: %v", widths)
	}
	sum := 0
	for _, w := range widths {
		sum += w
	}
	if sum > 70 {
		t.Errorf("widths %v overflow 70", widths)
	}
}

func TestKanbanWhileChildrenLoad(t *testing.T) {
	v := newItemView(&model.WorkItem{ID: 1}, model.BacklogConfig{}, nil)
	v.kanban = true
	if start, end, widths := v.kanbanWidths(60); start != 0 || end != 0 || len(widths) != 0 {
		t.Fatalf("no columns yet should lay out nothing, got %d-%d %v", start, end, widths)
	}
	_ = v.view(160, 40, "")
}
