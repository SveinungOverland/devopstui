package ui

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/sveinungoverland/devopstui/internal/model"
)

func childIDs(v *itemView) []int {
	var ids []int
	for _, c := range v.children {
		ids = append(ids, c.ID)
	}
	slices.Sort(ids)
	return ids
}

func TestTaskViewShowsSiblingsAndParent(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1015) // Task under PBI 1013, beside 1016 (Done) and 1017
	h.keys("D")
	v := h.app.item
	if v.item.ID != 1015 || !v.siblings {
		t.Fatalf("item = %d, siblings = %v", v.item.ID, v.siblings)
	}
	h.dump("30-task-view-siblings")

	if got := childIDs(v); !slices.Equal(got, []int{1015, 1016, 1017}) {
		t.Fatalf("siblings = %v", got)
	}
	if c := v.currentChild(); c == nil || c.ID != 1015 {
		t.Errorf("the cursor should start on the task itself, got %v", c)
	}
	if !v.focusDesc() || v.current().ID != 1015 {
		t.Error("the description should keep focus, so actions target the task")
	}
	// The siblings are the parent's work, not the task's own.
	if p := v.progress(); p.total != 0 {
		t.Errorf("own progress = %+v", p)
	}
	if p := v.listProgress(); p.done != 1 || p.total != 3 {
		t.Errorf("sibling progress = %+v", p)
	}

	out := h.app.View()
	for _, want := range []string{"Siblings of #1013 (3)", "1/3 done", "Parent", "gp to open",
		"PBI 1013 Propagate trace id", "Propagate trace id through queue workers.", selfMark} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q", want)
		}
	}
	if !strings.Contains(out, "n new sibling") {
		t.Error("footer should say n adds a sibling")
	}
	if strings.Contains(out, "Children (") || strings.Contains(out, "no children yet") || strings.Contains(out, "new child") {
		t.Error("a task should not offer children")
	}
}

func TestTaskViewSiblingNavigation(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1015)
	h.keys("D")
	v := h.app.item

	h.keys("tab")
	if len(v.related()) > 0 {
		h.keys("tab") // past Related to the siblings
	}
	if !v.focusKan {
		t.Fatal("tab should reach the siblings")
	}
	if v.previewed() != nil {
		t.Error("the task's own row should not preview it a second time")
	}
	if !strings.Contains(h.app.View(), "siblings 1/3") {
		t.Error("tab bar should count siblings")
	}
	h.keys("enter")
	if len(h.app.itemStack) != 1 || h.app.item != v {
		t.Fatal("enter on the task's own row should not drill into it again")
	}

	h.keys("j") // 1017, the other To Do task
	if c := v.currentChild(); c == nil || c.ID != 1017 {
		t.Fatalf("j should move to the next sibling, got %v", c)
	}
	if p := v.previewed(); p == nil || p.ID != 1017 {
		t.Error("a sibling should preview")
	}
	h.keys("L")
	if n := len(h.fake.Updates); n == 0 || h.fake.Updates[n-1].ID != 1017 {
		t.Fatalf("L should move the sibling's state, updates = %+v", h.fake.Updates)
	}
	h.keys("enter")
	if h.app.item.item.ID != 1017 || len(h.app.itemStack) != 2 {
		t.Fatalf("enter should drill into the sibling, got %d", h.app.item.item.ID)
	}
	if c := h.app.item.currentChild(); c == nil || c.ID != 1017 {
		t.Errorf("the sibling's own view should start on it, got %v", c)
	}
}

func TestTaskViewCreatesSibling(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1015)
	h.keys("D")
	v := h.app.item

	h.keys("n")
	p, ok := h.app.popup.(*prompt)
	if !ok {
		t.Fatalf("expected a title prompt, got %T", h.app.popup)
	}
	if !strings.Contains(p.title, "New Task under #1013") {
		t.Errorf("prompt = %q", p.title)
	}
	h.keys("D", "o", "c", "s", "enter")

	created := h.fake.Updates[len(h.fake.Updates)-1].ID
	if v.item.ID != 1015 || !slices.Contains(childIDs(v), created) {
		t.Fatalf("new sibling %d not listed: %v", created, childIDs(v))
	}
	if v.current().ID != created {
		t.Errorf("cursor should land on the new sibling, got %v", v.current())
	}
}

// forgetLoaded drops every list's copy of the items, as when the drill-down
// is reached through a link into items no view has loaded.
func forgetLoaded(h *harness) {
	h.app.sprint, h.app.backlog = newList(""), newList("")
	h.app.myItems, h.app.dashChildren = nil, nil
}

func TestTaskViewFetchesUnloadedParent(t *testing.T) {
	h := newHarness(t, 160, 45)
	task, err := h.fake.Get(context.Background(), 1015)
	if err != nil {
		t.Fatal(err)
	}
	forgetLoaded(h)
	h.run(h.app.openItem(task))
	v := h.app.item

	if v.parent == nil || v.parent.ID != 1013 {
		t.Fatalf("the parent should be fetched, got %v", v.parent)
	}
	if got := childIDs(v); !slices.Equal(got, []int{1015, 1016, 1017}) {
		t.Fatalf("siblings = %v", got)
	}
	if !strings.Contains(h.app.View(), "PBI 1013 Propagate trace id") {
		t.Error("parent card should show the fetched parent")
	}
	// n still adds a sibling, not an unparented requirement.
	h.keys("n")
	if p, ok := h.app.popup.(*prompt); !ok || !strings.Contains(p.title, "New Task under #1013") {
		t.Errorf("popup = %#v", h.app.popup)
	}
}

func TestTaskViewWithoutParent(t *testing.T) {
	h := newHarness(t, 160, 45)
	task, err := h.fake.Create(context.Background(), "Platform", model.NewItem{Type: "Task", Title: "Orphan task"})
	if err != nil {
		t.Fatal(err)
	}
	h.run(h.app.openItem(task))
	v := h.app.item
	if !v.siblings || len(v.children) != 0 || v.loading {
		t.Fatalf("siblings = %v, children = %v, loading = %v", v.siblings, childIDs(v), v.loading)
	}
	out := h.app.View()
	if !strings.Contains(out, "no parent, so no siblings") || strings.Contains(out, "gp to open") {
		t.Error("an orphan task should say it has no parent, without a parent card")
	}
}

func TestParentCardPrefersAcceptanceCriteria(t *testing.T) {
	p := &model.WorkItem{Description: "Why we are doing it.", AcceptanceCriteria: "- It works"}
	if heading, lines := parentCardBody(p, 40); heading != "Acceptance criteria" || !strings.Contains(strings.Join(lines, "\n"), "It works") {
		t.Errorf("heading = %q, lines = %q", heading, lines)
	}
	p.AcceptanceCriteria = " "
	if heading, lines := parentCardBody(p, 40); heading != "Description" || !strings.Contains(strings.Join(lines, "\n"), "Why we are") {
		t.Errorf("heading = %q, lines = %q", heading, lines)
	}
	p.Description = ""
	if heading, lines := parentCardBody(p, 40); heading != "" || lines != nil {
		t.Errorf("an empty parent should have no body, got %q %q", heading, lines)
	}
}

func TestPBIViewStillShowsChildren(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1013)
	h.keys("D")
	v := h.app.item
	if v.siblings {
		t.Fatal("a PBI lists its children, not its siblings")
	}
	out := h.app.View()
	if !strings.Contains(out, "Children (3)") || strings.Contains(out, "Siblings") || strings.Contains(out, "gp to open") {
		t.Error("PBI view should be unchanged")
	}
}
