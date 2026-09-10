package ui

import (
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

// harnessWithConfig is newHarness but with a real config file, so the
// persisted settings can be read back.
func harnessWithConfig(t *testing.T, cfg config.Config) (*harness, string) {
	t.Helper()
	lipgloss.SetColorProfile(termenv.Ascii)
	markdown.Style = "ascii"
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	t.Setenv("AZURE_DEVOPS_ORG_URL", "")
	t.Setenv("AZURE_DEVOPS_EXT_PAT", "")
	t.Setenv("DEVOPSTUI_PAT", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	f := ado.NewFake()
	f.Latency = 0
	a := New(f, cfg, path, false)
	h := &harness{t: t, app: a, fake: f}
	h.send(tea.WindowSizeMsg{Width: 140, Height: 40})
	h.run(a.loadContext())
	return h, path
}

func TestAutoRefreshToggle(t *testing.T) {
	base := config.Config{Org: "https://dev.azure.com/demo", Project: "Platform", Team: "Team Blue"}
	h, path := harnessWithConfig(t, base)
	a := h.app

	if a.cfg.RefreshSeconds != 0 {
		t.Fatal("auto refresh should start off")
	}
	if strings.Contains(a.View(), "↻") {
		t.Error("no indicator while off")
	}

	// R turns it on with the default interval and says so.
	h.keys("R")
	if a.cfg.RefreshSeconds != defaultRefreshSeconds {
		t.Fatalf("interval = %d, want %d", a.cfg.RefreshSeconds, defaultRefreshSeconds)
	}
	if !strings.Contains(a.View(), "↻60s") {
		t.Error("header should show the interval")
	}
	if !strings.Contains(a.flash, "every 60s") {
		t.Errorf("flash = %q", a.flash)
	}
	h.dump("32-auto-refresh-on")

	// It is written to the config file, not just held in memory.
	saved, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RefreshSeconds != defaultRefreshSeconds {
		t.Fatalf("saved config = %+v", saved)
	}

	// R again turns it off, and that is saved too.
	h.keys("R")
	if a.cfg.RefreshSeconds != 0 || strings.Contains(a.View(), "↻") {
		t.Error("second R should turn it off")
	}
	if saved, _ := config.Load(path); saved.RefreshSeconds != 0 {
		t.Errorf("off state not saved: %+v", saved)
	}
	if !strings.Contains(a.flash, "off") {
		t.Errorf("flash = %q", a.flash)
	}
}

func TestAutoRefreshRemembersInterval(t *testing.T) {
	base := config.Config{Org: "x", Project: "Platform", Team: "Team Blue", RefreshSeconds: 120}
	h, _ := harnessWithConfig(t, base)
	a := h.app

	h.keys("R") // off
	if a.cfg.RefreshSeconds != 0 {
		t.Fatal("should turn off")
	}
	h.keys("R") // on again with the configured 120, not the default
	if a.cfg.RefreshSeconds != 120 {
		t.Fatalf("interval = %d, want the configured 120", a.cfg.RefreshSeconds)
	}
}

func TestAutoRefreshTickHygiene(t *testing.T) {
	h, _ := harnessWithConfig(t, config.Config{Org: "x", Project: "Platform", Team: "Team Blue"})
	a := h.app
	h.keys("R")
	gen := a.refreshGen

	// A tick from an abandoned chain does nothing.
	if _, cmd := a.Update(tickMsg{gen: gen - 1}); cmd != nil {
		t.Error("stale tick should be ignored")
	}
	// A tick while a dialog is open reschedules but does not reload.
	a.sprint.jumpTo(1004)
	h.keys("s") // opens the state picker
	if a.popup == nil {
		t.Fatal("expected a popup")
	}
	a.loading[viewSprint] = false
	if _, cmd := a.Update(tickMsg{gen: gen}); cmd == nil {
		t.Error("should keep ticking under a popup")
	}
	if a.loading[viewSprint] {
		t.Error("must not reload while a dialog is open")
	}
	h.keys("esc")

	// Turning it off stops the chain: the next tick of that generation is
	// ignored rather than rescheduling.
	h.keys("R")
	if _, cmd := a.Update(tickMsg{gen: gen}); cmd != nil {
		t.Error("ticks should stop once auto refresh is off")
	}
}

func TestAutoRefreshCommand(t *testing.T) {
	h, path := harnessWithConfig(t, config.Config{Org: "x", Project: "Platform", Team: "Team Blue"})
	a := h.app

	h.keys(":")
	h.keys("a", "u", "t", "o", " ", "3", "0", "enter")
	if a.cfg.RefreshSeconds != 30 {
		t.Fatalf("interval = %d, want 30", a.cfg.RefreshSeconds)
	}
	if saved, _ := config.Load(path); saved.RefreshSeconds != 30 {
		t.Errorf("not saved: %+v", saved)
	}

	h.keys(":")
	h.keys("a", "u", "t", "o", " ", "o", "f", "f", "enter")
	if a.cfg.RefreshSeconds != 0 {
		t.Fatalf("off failed: %d", a.cfg.RefreshSeconds)
	}

	h.keys(":")
	h.keys("a", "u", "t", "o", " ", "o", "n", "enter")
	if a.cfg.RefreshSeconds != 30 {
		t.Fatalf("on should restore 30, got %d", a.cfg.RefreshSeconds)
	}

	// A nonsense interval is refused and changes nothing.
	h.keys(":")
	h.keys("a", "u", "t", "o", " ", "x", "enter")
	if a.cfg.RefreshSeconds != 30 || !strings.Contains(a.flash, "seconds") {
		t.Errorf("bad input: interval=%d flash=%q", a.cfg.RefreshSeconds, a.flash)
	}
}
