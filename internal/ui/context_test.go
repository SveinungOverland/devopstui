package ui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/sveinungoverland/devopstui/internal/ado"
	"github.com/sveinungoverland/devopstui/internal/config"
	"github.com/sveinungoverland/devopstui/internal/markdown"
)

var errDown = errors.New("connection reset")

// fakeAndPath is the setup of harnessWithConfig without the first load,
// for tests that need the backend to fail during it.
func fakeAndPath(t *testing.T) (*ado.Fake, string) {
	t.Helper()
	lipgloss.SetColorProfile(termenv.Ascii)
	markdown.Style = "ascii"
	for _, env := range []string{"VISUAL", "EDITOR", "AZURE_DEVOPS_ORG_URL", "AZURE_DEVOPS_EXT_PAT", "DEVOPSTUI_PAT"} {
		t.Setenv(env, "")
	}
	f := ado.NewFake()
	f.Latency = 0
	return f, filepath.Join(t.TempDir(), "config.yaml")
}

// Picking a filter and then another (or turning it off) must end on the
// last choice, whichever answer arrives last.
func TestTeamFilterLastChoiceWins(t *testing.T) {
	h := newHarness(t, 140, 40)
	a := h.app
	slow := a.setTeamFilter("Team Green")
	h.run(a.setTeamFilter(""))
	h.send(slow())
	if a.ctx.FilterTeam != "" || a.include() != nil {
		t.Errorf("a superseded filter was applied: %q", a.ctx.FilterTeam)
	}
	if a.isLoading() {
		t.Error("spinner left running")
	}
}

// A failure reading the filter team's areas at startup must not quietly
// turn the filter off and save the config without it.
func TestTeamFilterSurvivesStartupFailure(t *testing.T) {
	cfg := config.Config{Org: "https://dev.azure.com/demo", Project: "Platform", Team: "Team Blue", FilterTeam: "Team Green"}
	f, path := fakeAndPath(t)
	f.FailOn = map[string]error{"TeamAreas": errDown}
	a := New(f, cfg, path, false)
	h := &harness{t: t, app: a, fake: f}
	h.send(tea.WindowSizeMsg{Width: 140, Height: 40})
	h.run(a.loadContext())

	if a.ctx.FilterTeam != "Team Green" || !a.filterUnresolved() {
		t.Fatalf("filter = %q, unresolved = %v", a.ctx.FilterTeam, a.filterUnresolved())
	}
	if len(a.sprint.rows) == 0 {
		t.Fatal("the sprint must still load")
	}
	if v := a.View(); !strings.Contains(v, "not applied") {
		t.Errorf("header should say the filter is not applied:\n%s", v)
	}
	// Something else persists the config: the filter must still be in it.
	h.keys("c")
	saved, err := config.Load(path)
	if err != nil || saved.FilterTeam != "Team Green" {
		t.Fatalf("saved filter_team = %q, %v", saved.FilterTeam, err)
	}
	// r retries, and this time it gets through.
	f.FailOn = nil
	h.keys("r")
	if a.filterUnresolved() || a.include() == nil {
		t.Fatal("r did not apply the filter")
	}
	for _, r := range a.sprint.rows {
		if !r.External && r.Item.AreaPath != "Platform\\Green" {
			t.Errorf("row %d outside the filter", r.Item.ID)
		}
	}
}

// A switch that fails leaves the App on the team it was on, whole.
func TestFailedTeamSwitchStaysPut(t *testing.T) {
	f, path := fakeAndPath(t)
	cfg := config.Config{Org: "https://dev.azure.com/demo", Project: "Platform", Team: "Team Blue"}
	a := New(f, cfg, path, false)
	h := &harness{t: t, app: a, fake: f}
	h.send(tea.WindowSizeMsg{Width: 140, Height: 40})
	h.run(a.loadContext())
	rows := len(a.sprint.rows)

	f.FailOn = map[string]error{"Boards": errDown}
	h.command("team green")
	if a.ctx.Team != "Team Blue" {
		t.Fatalf("team = %q after a failed switch", a.ctx.Team)
	}
	if !a.flashErr || !strings.Contains(a.flash, "could not switch to Team Green") {
		t.Errorf("flash = %q", a.flash)
	}
	if len(a.sprint.rows) != rows || a.isLoading() {
		t.Errorf("rows %d (had %d), loading %v", len(a.sprint.rows), rows, a.isLoading())
	}
	if saved, _ := config.Load(path); saved.Team != "Team Blue" {
		t.Errorf("saved team = %q", saved.Team)
	}

	f.FailOn = nil
	h.command("team green")
	if a.ctx.Team != "Team Green" || !strings.Contains(a.View(), "Team Green") {
		t.Fatalf("team = %q", a.ctx.Team)
	}
	if saved, _ := config.Load(path); saved.Team != "Team Green" {
		t.Errorf("saved team = %q", saved.Team)
	}
}

// Switching team reloads the sprint, which is the team's, and leaves the
// Backlog and Dashboard, which are the project's, alone.
func TestTeamSwitchReloadsOnlyTheSprint(t *testing.T) {
	h := newHarness(t, 140, 40)
	a := h.app
	backlog, sprint := h.fake.Calls["Backlog"], a.loadGen[viewSprint]
	dash := a.loadGen[viewDash]
	h.keys(":")
	h.typeKeys("team")
	h.keys("enter")
	p, ok := a.popup.(*picker)
	if !ok {
		t.Fatalf("expected the team picker, got %T", a.popup)
	}
	if it := p.items[p.shown[p.cursor]]; it.Label != "Team Blue" {
		t.Errorf("picker opens on %q, want the current team", it.Label)
	}
	h.typeKeys("green")
	h.keys("enter")
	if a.ctx.Team != "Team Green" {
		t.Fatalf("team = %q", a.ctx.Team)
	}
	if a.loadGen[viewSprint] == sprint {
		t.Error("the sprint was not reloaded")
	}
	if h.fake.Calls["Backlog"] != backlog || a.loadGen[viewDash] != dash {
		t.Errorf("project data refetched: backlog %d→%d, dashboard %d→%d",
			backlog, h.fake.Calls["Backlog"], dash, a.loadGen[viewDash])
	}
	if a.isLoading() {
		t.Error("spinner left running")
	}
}

// Of two context switches in flight, the one asked for last wins.
func TestContextLastSwitchWins(t *testing.T) {
	h := newHarness(t, 140, 40)
	a := h.app
	first := a.switchContext("Platform", "Team Green")
	second := a.switchContext("Platform", "Team Blue")
	h.send(second())
	h.send(first())
	if a.ctx.Team != "Team Blue" {
		t.Errorf("team = %q: a superseded switch was applied", a.ctx.Team)
	}
}

// Of two loads of the same data, only the newer is applied: stepping two
// sprints on must not end on the first one's items.
func TestSupersededLoadDropped(t *testing.T) {
	h := newHarness(t, 140, 40)
	a := h.app
	slow := a.loadView(viewSprint) // Sprint 42, not delivered yet
	h.keys("]")
	if a.ctx.Iteration.Name != "Sprint 43" {
		t.Fatalf("sprint = %q", a.ctx.Iteration.Name)
	}
	want := len(a.sprint.all)
	h.send(slow())
	if len(a.sprint.all) != want {
		t.Errorf("the older load replaced Sprint 43's %d items with %d", want, len(a.sprint.all))
	}
	for _, it := range a.sprint.all {
		if it.IterationPath != "Platform\\Sprint 43" {
			t.Errorf("#%d from %s is showing under Sprint 43", it.ID, it.IterationPath)
		}
	}
}

// r on the Board or Team view reloads the sprint; the spinner must stop
// when that lands.
func TestRefreshClearsLoading(t *testing.T) {
	h := newHarness(t, 140, 40)
	for _, k := range []string{"3", "5", "6", "1", "4"} {
		h.keys(k, "r")
		if h.app.isLoading() {
			t.Errorf("still loading after r in view %s: %v", k, h.app.loading)
		}
	}
}

// With a team filter the backend reads only that team's backlog, plus
// the parents it hangs under; clearing the filter reads the rest again.
func TestBacklogFilteredByBackend(t *testing.T) {
	h := newHarness(t, 140, 40)
	a := h.app
	all := len(a.backlog.all)
	h.keys("4")
	h.command("filter green")
	if len(a.backlog.all) == 0 || len(a.backlog.all) >= all {
		t.Fatalf("filtered backlog holds %d of %d items", len(a.backlog.all), all)
	}
	for _, it := range a.backlog.all {
		if it.AreaPath != "Platform\\Green" {
			t.Errorf("#%d fetched from outside the filter", it.ID)
		}
	}
	// 1014's feature belongs to another team: it shows, dimmed, as context.
	found := false
	for _, r := range a.backlog.rows {
		if r.Item.ID == 1012 {
			found = r.External
		}
	}
	if !found {
		t.Error("parent feature 1012 should show as an external row")
	}
	h.command("filter off")
	if len(a.backlog.all) != all {
		t.Errorf("after filter off the backlog holds %d of %d items", len(a.backlog.all), all)
	}
}

func TestRefreshCommandReloadsContext(t *testing.T) {
	h := newHarness(t, 140, 40)
	a := h.app
	h.keys("]")
	teams := h.fake.Calls["Teams"]
	h.command("refresh")
	if h.fake.Calls["Teams"] == teams {
		t.Error(":refresh should reload the settings too")
	}
	if a.flashErr || a.ctx.Iteration.Name != "Sprint 43" {
		t.Errorf("flash %q, sprint %q: a refresh keeps the sprint on screen", a.flash, a.ctx.Iteration.Name)
	}
	if a.isLoading() || len(a.sprint.rows) == 0 {
		t.Errorf("loading %v, rows %d", a.isLoading(), len(a.sprint.rows))
	}
}
