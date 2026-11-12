package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/sveinungoverland/devopstui/internal/model"
)

// typeKeys sends text one key at a time, as the command bar and the
// pickers receive it.
func (h *harness) typeKeys(text string) {
	for _, r := range text {
		h.keys(string(r))
	}
}

// command runs a : command.
func (h *harness) command(c string) {
	h.keys(":")
	h.typeKeys(c)
	h.keys("enter")
}

func TestChangePeriodWindow(t *testing.T) {
	loc := time.FixedZone("CET", 3600)
	at := func(day, hour int) time.Time { return time.Date(2026, 3, day, hour, 0, 0, 0, loc) }
	sprint := model.Iteration{
		Start:  time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
		Finish: time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
	}
	cases := []struct {
		name         string
		p            changePeriod
		now          time.Time
		since, until time.Time
		label        string
	}{
		{"today", changePeriod{kind: periodToday}, at(10, 15), at(10, 0), time.Time{}, "today"},
		{"tuesday looks back to monday", changePeriod{kind: periodYesterday}, at(10, 9), at(9, 0), time.Time{}, "since yesterday"},
		// 9 March 2026 is a Monday: a stand-up then asks about Friday.
		{"monday looks back to friday", changePeriod{kind: periodYesterday}, at(9, 9), at(6, 0), time.Time{}, "since Friday"},
		{"week", changePeriod{kind: periodWeek}, at(10, 9), at(4, 0), time.Time{}, "last 7 days"},
		{"three days", changePeriod{kind: periodDays, days: 3}, at(10, 9), at(8, 0), time.Time{}, "last 3 days"},
		{"running sprint is open ended", changePeriod{kind: periodSprint}, at(10, 9), at(2, 0), time.Time{}, "this sprint"},
		// The last day counts in full: the window closes at the next midnight.
		{"finished sprint is closed", changePeriod{kind: periodSprint}, at(20, 9), at(2, 0), at(16, 0), "this sprint"},
	}
	for _, c := range cases {
		since, until := c.p.window(c.now, sprint)
		if !since.Equal(c.since) || !until.Equal(c.until) {
			t.Errorf("%s: window = %v … %v, want %v … %v", c.name, since, until, c.since, c.until)
		}
		if got := c.p.label(c.now); got != c.label {
			t.Errorf("%s: label = %q, want %q", c.name, got, c.label)
		}
	}
	// A sprint without dates still gets a window.
	if since, _ := (changePeriod{kind: periodSprint}).window(at(20, 9), model.Iteration{}); !since.Equal(at(7, 0)) {
		t.Errorf("undated sprint since = %v", since)
	}
}

func TestChangePeriodStep(t *testing.T) {
	kind := func(p changePeriod) periodKind { return p.kind }
	if got := kind(changePeriod{kind: periodToday}.step(-1)); got != periodToday {
		t.Errorf("shorter than today = %v", got)
	}
	if got := kind(changePeriod{kind: periodSprint}.step(1)); got != periodSprint {
		t.Errorf("longer than the sprint = %v", got)
	}
	if got := kind(changePeriod{kind: periodYesterday}.step(1)); got != periodWeek {
		t.Errorf("yesterday + 1 = %v", got)
	}
	// A custom number of days steps to the fixed period on either side.
	for _, c := range []struct {
		days, d int
		want    periodKind
	}{{3, 1, periodWeek}, {3, -1, periodYesterday}, {10, 1, periodSprint}, {10, -1, periodWeek}} {
		if got := kind(changePeriod{kind: periodDays, days: c.days}.step(c.d)); got != c.want {
			t.Errorf("%d days step %d = %v, want %v", c.days, c.d, got, c.want)
		}
	}
}

func changeIDs(c *changes) map[int]bool {
	out := map[int]bool{}
	for _, e := range c.entries {
		out[e.it.ID] = true
	}
	return out
}

func TestChangesView(t *testing.T) {
	h := newHarness(t, 140, 40)
	a := h.app
	h.keys("6")
	if a.view != viewChanges || !a.changes.loaded {
		t.Fatalf("view = %v, loaded = %v", a.view, a.changes.loaded)
	}
	h.dump("40-changes")
	v := a.View()
	for _, want := range []string{"6 Changes", "by day", "Today", "State To Do → In Progress", "Wire up magic link", "changes on", "Changed since"} {
		if !strings.Contains(v, want) {
			t.Errorf("changes view lacks %q:\n%s", want, v)
		}
	}
	// Newest first: the cursor starts on the latest change.
	if it := a.currentItem(); it == nil || it.ID != 1023 {
		t.Fatalf("current item = %+v, want 1023", it)
	}
	day := len(a.changes.entries)
	ids := changeIDs(a.changes)
	if ids[1011] || ids[1027] {
		t.Error("items outside the sprint must stay out until A widens the view")
	}

	// l looks further back; the newest change stays on top.
	h.keys("l")
	if a.changes.period.kind != periodWeek || len(a.changes.entries) <= day {
		t.Fatalf("after l: period %v, %d entries (had %d)", a.changes.period.kind, len(a.changes.entries), day)
	}
	if !strings.Contains(a.View(), "last 7 days") {
		t.Error("header should name the period")
	}
	h.keys("l")
	if a.changes.period.kind != periodSprint || !strings.Contains(a.View(), "this sprint") {
		t.Errorf("after l l: period %v", a.changes.period.kind)
	}
	h.keys("h")

	// A includes every sprint: last sprint's feature and the backlog show.
	h.keys("A")
	if ids := changeIDs(a.changes); !ids[1011] || !ids[1027] {
		t.Errorf("all sprints: %v", ids)
	}
	if !strings.Contains(a.View(), "all sprints") {
		t.Error("header should say the view covers all sprints")
	}
	h.keys("A")

	// The filter matches what changed as well as the item and the person.
	h.keys("/")
	h.typeKeys("comment")
	h.keys("enter")
	if len(a.changes.entries) == 0 {
		t.Fatal("no comments in the last week")
	}
	for _, e := range a.changes.entries {
		if !strings.Contains(e.c.Text(), "comment") {
			t.Errorf("filter kept %q", e.c.Text())
		}
	}
	h.keys("esc")
	if len(a.changes.entries) <= day {
		t.Error("esc should clear the filter")
	}
}

func TestChangesGrouping(t *testing.T) {
	h := newHarness(t, 140, 40)
	a := h.app
	h.keys("6", "l", "j", "j")
	cur, _ := a.changes.currentEntry()

	h.keys("f")
	h.dump("41-changes-by-item")
	if a.changes.group != groupItem || !strings.Contains(a.View(), "by item") {
		t.Fatalf("group = %v", a.changes.group)
	}
	if e, _ := a.changes.currentEntry(); e.c.ItemID != cur.c.ItemID || !e.c.At.Equal(cur.c.At) {
		t.Errorf("regrouping moved the cursor from %+v to %+v", cur.c, e.c)
	}
	// An item's changes sit together under its heading.
	seen, last := map[int]bool{}, 0
	for _, e := range a.changes.entries {
		if e.it.ID != last && seen[e.it.ID] {
			t.Fatalf("#%d's changes are split up", e.it.ID)
		}
		seen[e.it.ID], last = true, e.it.ID
	}

	h.keys("f")
	h.dump("42-changes-by-person")
	v := a.View()
	for _, want := range []string{"by person", "Alex Kim", "Priya Natarajan"} {
		if !strings.Contains(v, want) {
			t.Errorf("by person lacks %q", want)
		}
	}
	// J and K step between people.
	h.keys("g", "g")
	first, _ := a.changes.currentEntry()
	h.keys("J")
	second, _ := a.changes.currentEntry()
	if second.c.By == first.c.By {
		t.Errorf("J stayed on %s", first.c.By)
	}
	h.keys("K")
	if e, _ := a.changes.currentEntry(); e.c.By != first.c.By || a.changes.cursor != 0 {
		t.Errorf("K went to %s, row %d", e.c.By, a.changes.cursor)
	}
	h.keys("f")
	if a.changes.group != groupDay {
		t.Errorf("f should cycle back to days, got %v", a.changes.group)
	}
}

// A write made from the view is itself a change, and shows up at the top.
func TestChangesFollowsWrites(t *testing.T) {
	h := newHarness(t, 140, 40)
	a := h.app
	h.keys("6")
	before := h.fake.Calls["Changes"]
	h.keys("s")
	if _, ok := a.popup.(*picker); !ok {
		t.Fatalf("expected the state picker, got %T", a.popup)
	}
	h.typeKeys("done")
	h.keys("enter")
	if len(h.fake.Updates) != 1 || h.fake.Updates[0].ID != 1023 {
		t.Fatalf("updates = %+v", h.fake.Updates)
	}
	if h.fake.Calls["Changes"] == before {
		t.Fatal("the view was not reloaded after the write")
	}
	top := a.changes.entries[0]
	if top.c.By != a.me || top.c.Text() != "State In Progress → Done" {
		t.Fatalf("newest change = %s: %s", top.c.By, top.c.Text())
	}
	if top.it.State != "Done" {
		t.Errorf("row still shows the item as %s", top.it.State)
	}
}

func TestChangesDrillAndGoto(t *testing.T) {
	h := newHarness(t, 140, 40)
	a := h.app
	// gc from the sprint lands on the item's newest change, loading the
	// view on the way.
	a.sprint.jumpTo(1013)
	h.keys("g", "c")
	if a.view != viewChanges {
		t.Fatalf("view = %v", a.view)
	}
	if it := a.currentItem(); it == nil || it.ID != 1013 {
		t.Fatalf("gc landed on %+v, want 1013", it)
	}
	// enter opens the item; esc comes back to the same change.
	row := a.changes.cursor
	h.keys("enter")
	if a.view != viewItem || a.item.item.ID != 1013 {
		t.Fatalf("enter: view %v", a.view)
	}
	h.keys("esc")
	if a.view != viewChanges || a.changes.cursor != row {
		t.Errorf("esc: view %v, row %d (was %d)", a.view, a.changes.cursor, row)
	}
	// ctrl+o walks back to the sprint.
	h.keys("ctrl+o", "ctrl+o", "ctrl+o")
	if a.view != viewSprint {
		t.Errorf("ctrl+o ended on %v", a.view)
	}
}

// The view follows the sprint on screen and the team filter, and says so
// when there is nothing to show.
func TestChangesFollowsSprintAndFilter(t *testing.T) {
	h := newHarness(t, 140, 40)
	a := h.app
	h.keys("6", "l")
	h.command("filter green")
	if a.ctx.FilterTeam != "Team Green" {
		t.Fatalf("filter = %q", a.ctx.FilterTeam)
	}
	if len(a.changes.entries) == 0 {
		t.Fatal("no changes for Team Green in the last week")
	}
	for _, e := range a.changes.entries {
		if e.it.AreaPath != "Platform\\Green" {
			t.Errorf("#%d (%s) is outside the filter", e.it.ID, e.it.AreaPath)
		}
	}
	h.command("filter off")

	h.keys("[") // Sprint 41: only the feature finished there changed
	if ids := changeIDs(a.changes); len(ids) != 1 || !ids[1011] {
		t.Errorf("previous sprint: %v", ids)
	}
	h.keys("h", "h") // today: nothing in that sprint
	v := a.View()
	if len(a.changes.entries) != 0 || !strings.Contains(v, "nothing changed today") || !strings.Contains(v, "A includes every sprint") {
		t.Errorf("empty state:\n%s", v)
	}
}

func TestChangesCommand(t *testing.T) {
	h := newHarness(t, 140, 40)
	a := h.app
	h.command("changes sprint")
	if a.view != viewChanges || a.changes.period.kind != periodSprint || !a.changes.loaded {
		t.Fatalf("view %v, period %v, loaded %v", a.view, a.changes.period.kind, a.changes.loaded)
	}
	h.command("changes 3")
	if p := a.changes.period; p.kind != periodDays || p.days != 3 || !strings.Contains(a.View(), "last 3 days") {
		t.Errorf("period = %+v", p)
	}
	h.command("changes nonsense")
	if !a.flashErr || a.changes.period.days != 3 {
		t.Errorf("bad argument: flash %q, period %+v", a.flash, a.changes.period)
	}
	// w offers the fixed periods, with the cursor on none of them now.
	h.keys("w")
	p, ok := a.popup.(*picker)
	if !ok || len(p.items) != len(periodCycle) {
		t.Fatalf("period picker = %T", a.popup)
	}
	h.typeKeys("today")
	h.keys("enter")
	if a.changes.period.kind != periodToday {
		t.Errorf("picked %v", a.changes.period.kind)
	}
}

func TestChangesFitsNarrowTerminal(t *testing.T) {
	h := newHarness(t, 80, 24)
	h.keys("6", "l")
	for _, g := range []string{"", "f", "f"} {
		if g != "" {
			h.keys(g)
		}
		v := h.app.View()
		lines := strings.Split(v, "\n")
		if len(lines) != 24 {
			t.Errorf("%v: %d lines at 24 rows", h.app.changes.group, len(lines))
		}
		for _, l := range lines {
			if w := lipgloss.Width(l); w > 80 {
				t.Errorf("%v: line is %d wide at 80 columns: %q", h.app.changes.group, w, l)
			}
		}
	}
	h.dump("43-changes-narrow")
}
