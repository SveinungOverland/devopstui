package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/sveinungoverland/devopstui/internal/model"
)

var teamDef = model.Board{Columns: []model.BoardColumn{
	{Name: "New", States: []string{"New"}},
	{Name: "Committed", States: []string{"Committed"}},
	{Name: "Done", States: []string{"Done"}},
}}

func pbi(id int, who, state string) *model.WorkItem {
	return &model.WorkItem{ID: id, Kind: model.KindRequirement, Title: "item", State: state, AssignedTo: who,
		ParentID: 1, ChangedDate: time.Now()}
}

func teamNames(t *team) []string {
	var out []string
	for _, p := range t.people {
		out = append(out, p.label())
	}
	return out
}

func teamIDs(p teamPerson) []int {
	var out []int
	for _, it := range p.items {
		out = append(out, it.ID)
	}
	return out
}

// TestTeamOrdersByAttention puts the people with the most flagged items
// first, falls back to the alphabet on a tie, and keeps unassigned work
// last whatever its flags.
func TestTeamOrdersByAttention(t *testing.T) {
	old := time.Now().Add(-30 * 24 * time.Hour)
	stale := func(it *model.WorkItem) *model.WorkItem { it.ChangedDate = old; return it }
	items := []*model.WorkItem{
		pbi(1, "Bea", "New"),
		pbi(2, "Adam", "New"),
		stale(pbi(3, "Cleo", "Committed")),
		stale(pbi(4, "Dan", "Committed")),
		stale(pbi(5, "Dan", "Committed")),
		stale(pbi(6, "", "Committed")), // stale and unassigned: most flags of all
		stale(pbi(7, "", "Committed")),
		stale(pbi(8, "", "Committed")),
		{ID: 9, Kind: model.KindTask, State: "New", AssignedTo: "Eve", ParentID: 1}, // not a board item
	}
	tm := newTeam()
	tm.setItems(teamDef, items, model.BacklogConfig{}, nil)
	want := []string{"Dan", "Cleo", "Adam", "Bea", "Unassigned"}
	if got := teamNames(tm); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("people = %v, want %v", got, want)
	}
	if tm.people[0].signals[model.SignalStale] != 2 {
		t.Errorf("Dan's stale tally = %d, want 2", tm.people[0].signals[model.SignalStale])
	}
}

// TestTeamHidesDoneUnlessFlagged: done work is hidden by default, except a
// done item that still has a flag, and the person's tallies count it all.
func TestTeamHidesDoneUnlessFlagged(t *testing.T) {
	task := &model.WorkItem{ID: 30, Kind: model.KindTask, State: "New", ParentID: 3}
	items := []*model.WorkItem{
		pbi(1, "Ann", "Done"),
		pbi(2, "Ann", "Committed"),
		pbi(3, "Ann", "Done"), // done with an open task: flagged
		pbi(4, "Ann", "New"),
		task,
	}
	tm := newTeam()
	tm.setItems(teamDef, items, model.BacklogConfig{}, nil)
	p := tm.people[0]
	if got := teamIDs(p); len(got) != 3 || got[0] != 4 || got[1] != 2 || got[2] != 3 {
		t.Errorf("rows = %v, want [4 2 3] (column order, done #1 hidden, flagged #3 kept)", got)
	}
	if p.total != 4 || p.done != 2 || p.active != 1 {
		t.Errorf("tallies total/done/active = %d/%d/%d, want 4/2/1", p.total, p.done, p.active)
	}

	tm.showDone = true
	tm.setItems(teamDef, items, model.BacklogConfig{}, nil)
	if got := teamIDs(tm.people[0]); len(got) != 4 {
		t.Errorf("rows with done shown = %v, want all 4", got)
	}
}

// TestTeamNavigation: j/k walk every row across people, J/K jump between
// people, h/l between column groups, and focus keeps the cursor on one
// person.
func TestTeamNavigation(t *testing.T) {
	items := []*model.WorkItem{
		pbi(1, "Ann", "New"), pbi(2, "Ann", "New"), pbi(3, "Ann", "Committed"),
		pbi(4, "Bob", "Committed"),
	}
	tm := newTeam()
	tm.setItems(teamDef, items, model.BacklogConfig{}, nil)
	at := func(want int) {
		t.Helper()
		if it := tm.current(); it == nil || it.ID != want {
			t.Fatalf("cursor on %v, want #%d", it, want)
		}
	}
	at(1)
	tm.moveColumn(1)
	at(3)
	tm.moveColumn(1)
	at(4)
	tm.moveColumn(-1)
	at(3)
	tm.moveColumn(-1)
	at(1)
	tm.move(3)
	at(4)
	tm.movePerson(-1)
	at(1)

	tm.focus = true
	tm.move(10)
	at(3) // the last of Ann's rows, not Bob's
	tm.movePerson(1)
	at(4)
	if v := tm.view(80, 20, true); strings.Contains(v, "Ann") {
		t.Errorf("focused on Bob, but Ann still shows:\n%s", v)
	}
}

// TestTeamViewInApp drives the tab through the real app: 5 opens it, the
// preview follows the cursor, and L moves the item to the next column
// without losing it.
func TestTeamViewInApp(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	h := newHarness(t, 140, 40)
	h.keys("5")
	if h.app.view != viewTeam {
		t.Fatalf("view = %v, want Team", h.app.view)
	}
	screen := h.app.View()
	if !strings.Contains(strings.Split(screen, "\n")[1], "5 Team") {
		t.Errorf("tabs line lacks the Team tab:\n%s", screen)
	}
	it := h.app.currentItem()
	if it == nil {
		t.Fatal("no item under the cursor")
	}
	if got := h.app.team.people[0].label(); !strings.Contains(screen, got) {
		t.Errorf("screen lacks the first person %q", got)
	}
	id, state := it.ID, it.State
	h.keys("L")
	moved := h.app.lookup(id)
	if moved == nil || moved.State == state {
		t.Fatalf("#%d state after L = %v, want it changed from %q", id, moved, state)
	}
	if cur := h.app.currentItem(); cur == nil || cur.ID != id {
		t.Errorf("cursor left #%d after the move", id)
	}
}

// TestTeamKeepsPersonWhenOrderChanges: when the highlighted item leaves the
// view (done, so hidden) on the same refresh that reorders the people, the
// cursor stays with that item's person, not whoever now sits in their slot.
func TestTeamKeepsPersonWhenOrderChanges(t *testing.T) {
	tm := newTeam()
	// Three people from the start, so the rebuild reuses the same backing
	// array rather than growing a new one.
	tm.setItems(teamDef, []*model.WorkItem{pbi(1, "Ann", "New"), pbi(2, "Bob", "New"), pbi(3, "Cid", "New")}, model.BacklogConfig{}, nil)
	tm.jumpTo(2)

	stale := pbi(3, "Cid", "Committed")
	stale.ChangedDate = time.Now().Add(-30 * 24 * time.Hour) // flagged, so Cid sorts first
	tm.setItems(teamDef, []*model.WorkItem{pbi(1, "Ann", "New"), pbi(2, "Bob", "Done"), stale}, model.BacklogConfig{}, nil)
	if got := teamNames(tm); strings.Join(got, ",") != "Cid,Ann,Bob" {
		t.Fatalf("people = %v, want [Cid Ann Bob]", got)
	}
	if got := tm.people[tm.p].label(); got != "Bob" {
		t.Errorf("cursor on %s, want Bob, whose item was just hidden", got)
	}
}
