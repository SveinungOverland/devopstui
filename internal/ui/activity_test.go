package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/sveinungoverland/devopstui/internal/ado"
	"github.com/sveinungoverland/devopstui/internal/config"
	"github.com/sveinungoverland/devopstui/internal/model"
)

func TestActivityFeedNewestFirst(t *testing.T) {
	h := newHarness(t, 140, 40)
	if h.app.activity.loaded {
		t.Fatal("the feed should not be fetched before its tab is opened")
	}
	h.keys("5")
	h.dump("activity")
	if h.app.view != viewActivity {
		t.Fatalf("view = %v, want Activity", h.app.view)
	}
	rows := h.app.activity.rows
	if len(rows) == 0 {
		t.Fatal("feed is empty")
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].ChangedDate.After(rows[i-1].ChangedDate) {
			t.Fatalf("row %d (#%d) changed after row %d (#%d): feed must be newest first", i, rows[i].ID, i-1, rows[i-1].ID)
		}
	}
	v := h.app.View()
	for _, want := range []string{"5 Activity", "Localise the invite email", "created", "I involved only"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestActivityOpensDetails(t *testing.T) {
	for _, k := range []string{"enter", "D"} {
		h := newHarness(t, 140, 40)
		h.keys("5", "j")
		want := h.app.activity.current().ID
		h.keys(k)
		if h.app.view != viewItem || h.app.item == nil || h.app.item.item.ID != want {
			t.Fatalf("%s: want details of #%d, view = %v", k, want, h.app.view)
		}
		h.keys("esc")
		if h.app.view != viewActivity || h.app.activity.current().ID != want {
			t.Fatalf("%s: esc should land back on #%d in the feed", k, want)
		}
	}
}

// TestActivityMarker covers "since you last looked": the marker sits where
// the last visit began, entering the tab records the new visit, and
// drilling into an item and back out is not a new visit.
func TestActivityMarker(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	lastSeen := time.Now().Add(-12 * time.Hour)
	cfg := config.Config{Org: "https://dev.azure.com/demo", Project: "Platform", Team: "Team Blue", LastSeenActivity: lastSeen}
	h := newHarnessWithConfig(t, 140, 40, cfg, cfgPath)
	before := time.Now()
	h.keys("5")

	f := h.app.activity
	var want int
	for _, it := range f.rows {
		if it.ChangedDate.After(lastSeen) {
			want++
		}
	}
	if want == 0 || want == len(f.rows) {
		t.Fatalf("seed data should have both new and old entries; %d of %d are new", want, len(f.rows))
	}
	if got := f.newCount(); got != want {
		t.Errorf("newCount = %d, want %d", got, want)
	}
	v := h.app.View()
	h.dump("activity-marker")
	if !strings.Contains(v, "since you last looked") || !strings.Contains(v, " new") {
		t.Errorf("marker line or header count missing:\n%s", v)
	}

	saved, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.LastSeenActivity.Before(before) {
		t.Errorf("last_seen_activity = %v, want the visit (after %v)", saved.LastSeenActivity, before)
	}

	h.keys("enter", "esc")
	if !f.since.Equal(lastSeen) {
		t.Errorf("drilling in and out moved the marker to %v", f.since)
	}

	// A fresh visit puts the marker where this one began.
	visit := h.app.cfg.LastSeenActivity
	h.keys("1", "5")
	if !h.app.activity.since.Equal(visit) {
		t.Errorf("second visit marker = %v, want %v", h.app.activity.since, visit)
	}
}

func TestActivityNoMarkerOnFirstUse(t *testing.T) {
	h := newHarness(t, 140, 40)
	h.keys("5")
	if n := h.app.activity.newCount(); n != 0 {
		t.Errorf("first ever visit marked %d entries new", n)
	}
	if strings.Contains(h.app.View(), "since you last looked") {
		t.Error("first ever visit should have no marker line")
	}
}

func TestActivityInvolvedToggle(t *testing.T) {
	h := newHarness(t, 140, 40)
	h.keys("5")
	all := len(h.app.activity.rows)
	h.keys("I")
	rows := h.app.activity.rows
	if len(rows) == 0 || len(rows) >= all {
		t.Fatalf("involved-only kept %d of %d rows", len(rows), all)
	}
	me := h.app.me
	for _, it := range rows {
		if it.AssignedTo != me && it.CreatedBy != me && it.ChangedBy != me {
			t.Errorf("#%d is not assigned to, created by or last changed by %s", it.ID, me)
		}
	}
	if !strings.Contains(h.app.View(), "involved") {
		t.Error("header should say the feed is narrowed")
	}
	h.keys("I")
	if len(h.app.activity.rows) != all {
		t.Errorf("toggling back shows %d rows, want %d", len(h.app.activity.rows), all)
	}
}

func TestActivitySprintAndTeamScope(t *testing.T) {
	h := newHarness(t, 140, 40)
	h.keys("5")
	all := len(h.app.activity.rows)
	h.keys("A")
	sprint := h.app.ctx.Iteration.Path
	rows := h.app.activity.rows
	if len(rows) == 0 || len(rows) >= all {
		t.Fatalf("sprint-only kept %d of %d rows", len(rows), all)
	}
	for _, it := range rows {
		if it.IterationPath != sprint {
			t.Errorf("#%d is in %s, not %s", it.ID, it.IterationPath, sprint)
		}
	}
	// Moving to another sprint re-scopes the feed straight away.
	h.keys("]")
	for _, it := range h.app.activity.rows {
		if it.IterationPath != h.app.ctx.Iteration.Path {
			t.Errorf("after ], #%d is in %s", it.ID, it.IterationPath)
		}
	}
	h.keys("[", "A")

	h.run(h.app.setTeamFilter("Team Green"))
	rows = h.app.activity.rows
	if len(rows) == 0 || len(rows) >= all {
		t.Fatalf("team filter kept %d of %d rows", len(rows), all)
	}
	for _, it := range rows {
		if !model.InAreas(it.AreaPath, h.app.ctx.FilterAreas) {
			t.Errorf("#%d in %s is outside Team Green", it.ID, it.AreaPath)
		}
	}
}

// TestActivityEditMovesToTop: an edit made from the feed is now its newest
// change, so the item moves to the top and the cursor follows it.
func TestActivityEditMovesToTop(t *testing.T) {
	h := newHarness(t, 140, 40)
	h.keys("5", "j", "j")
	id := h.app.activity.current().ID
	if h.app.activity.rows[0].ID == id {
		t.Fatal("pick an item that is not already on top")
	}
	h.keys("s", "enter")
	if len(h.fake.Updates) == 0 {
		t.Fatal("no write recorded")
	}
	if got := h.app.activity.rows[0].ID; got != id {
		t.Errorf("top row = #%d, want the edited #%d", got, id)
	}
	if got := h.app.activity.current().ID; got != id {
		t.Errorf("cursor on #%d, want #%d", got, id)
	}
}

// TestActivityReloadsOnVisit: a stale "what changed" is no use, so every
// visit refetches, picking up changes the app itself didn't make.
func TestActivityReloadsOnVisit(t *testing.T) {
	h := newHarness(t, 140, 40)
	h.keys("5", "1")
	ctx := context.Background()
	it, err := h.fake.Get(ctx, 1005)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.fake.Update(ctx, it.ID, it.Rev, []model.Patch{{Field: model.FieldTitle, Value: "Expire invites after 3 days"}}); err != nil {
		t.Fatal(err)
	}
	h.keys("5")
	if got := h.app.activity.rows[0]; got.ID != 1005 || got.Title != "Expire invites after 3 days" {
		t.Errorf("top row = #%d %q, want the change made elsewhere", got.ID, got.Title)
	}
}

// feedClient serves a fixed Activity feed on top of the demo Fake, for
// data the demo seed doesn't have.
type feedClient struct {
	*ado.Fake
	feed []*model.WorkItem
}

func (c *feedClient) Activity(context.Context, string) ([]*model.WorkItem, error) {
	return c.feed, nil
}

// TestActivityFrameFitsRealData: real organisations have six- and
// seven-digit ids, long names and the odd tab or line break in a title.
// A row even one cell wider than the panel wraps inside it, which grew the
// panel past the terminal and scrolled the header and the cursor off the
// top. The frame must stay exactly the terminal's size, with the cursor on
// it, however far the cursor travels.
func TestActivityFrameFitsRealData(t *testing.T) {
	titles := []string{
		"Short",
		"A really rather long title that goes on and on about the work across several teams",
		"Title\twith\ttabs",
		"Title with a\r\nline break\n",
		"Emoji 🚀 title ✅ and 日本語のタイトル",
	}
	names := []string{"Alex Kim", "Dr. Maximiliana Konstantinopoulou-Wetherington (Contractor)", ""}
	now := time.Now()
	var feed []*model.WorkItem
	for i := range 60 {
		feed = append(feed, &model.WorkItem{
			ID: 99990 + i*40000, Rev: 1 + i%3, Type: "Product Backlog Item", Kind: model.KindRequirement,
			Title: titles[i%len(titles)], State: "Ready for Acceptance Testing",
			AssignedTo: names[i%len(names)], ChangedBy: names[(i+1)%len(names)],
			ChangedDate: now.Add(-time.Duration(i) * 37 * time.Minute),
		})
	}
	for _, sz := range [][2]int{{80, 24}, {140, 40}, {200, 50}} {
		w, hgt := sz[0], sz[1]
		h := newHarness(t, w, hgt)
		h.app.client = &feedClient{Fake: h.fake, feed: feed}
		h.app.cfg.LastSeenActivity = now.Add(-5 * time.Hour) // draw the marker too
		h.keys("5")
		check := func(step string) {
			t.Helper()
			lines := strings.Split(h.app.View(), "\n")
			if len(lines) != hgt {
				t.Fatalf("%dx%d %s: frame is %d lines, want %d", w, hgt, step, len(lines), hgt)
			}
			cursor := false
			for i, l := range lines {
				if lw := lipgloss.Width(l); lw > w {
					t.Fatalf("%dx%d %s: line %d is %d wide, want <= %d", w, hgt, step, i, lw, w)
				}
				cursor = cursor || strings.Contains(l, cursorMark)
			}
			if !cursor {
				t.Fatalf("%dx%d %s: cursor row is off screen", w, hgt, step)
			}
		}
		check("top")
		for i := range len(feed) {
			h.keys("j")
			check(fmt.Sprintf("down %d", i+1))
		}
		for i := range len(feed) {
			h.keys("k")
			check(fmt.Sprintf("up %d", i+1))
		}
	}
}
