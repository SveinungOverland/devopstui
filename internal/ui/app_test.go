package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/sveinungoverland/devopstui/internal/ado"
	"github.com/sveinungoverland/devopstui/internal/config"
)

// harness runs commands synchronously so flows can be asserted without a
// terminal. Commands that take longer than the deadline (tea.Tick) are
// dropped, which is what we want for flash timers and spinners.
type harness struct {
	t    *testing.T
	app  *App
	fake *ado.Fake
}

func newHarness(t *testing.T, w, h int) *harness {
	t.Helper()
	lipgloss.SetColorProfile(termenv.Ascii) // no escape codes in dumps
	f := ado.NewFake()
	f.Latency = 0
	cfg := config.Config{Org: "https://dev.azure.com/demo", Project: "Platform", Team: "Team Blue"}
	a := New(f, cfg, os.DevNull, false)
	hs := &harness{t: t, app: a, fake: f}
	hs.send(tea.WindowSizeMsg{Width: w, Height: h})
	hs.run(a.loadContext())
	return hs
}

func (h *harness) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		h.send(msg)
	case <-time.After(300 * time.Millisecond):
	}
}

func (h *harness) send(msg tea.Msg) {
	switch m := msg.(type) {
	case nil:
		return
	case tea.BatchMsg:
		for _, c := range m {
			h.run(c)
		}
		return
	}
	_, cmd := h.app.Update(msg)
	h.run(cmd)
}

func (h *harness) keys(ks ...string) {
	for _, k := range ks {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEscape}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "space":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		case "ctrl+s":
			msg = tea.KeyMsg{Type: tea.KeyCtrlS}
		case "ctrl+a":
			msg = tea.KeyMsg{Type: tea.KeyCtrlA}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		h.send(msg)
	}
}

func (h *harness) dump(name string) {
	if os.Getenv("DUMP_DIR") == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(os.Getenv("DUMP_DIR"), name+".txt"), []byte(h.app.View()), 0o644)
}

func TestSprintTreeRenders(t *testing.T) {
	h := newHarness(t, 140, 40)
	v := h.app.View()
	h.dump("01-sprint")
	for _, want := range []string{"Sprint 42", "Request tracing", "Propagate trace id", "Team Blue"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q", want)
		}
	}
	if h.app.sprint.current() == nil {
		t.Fatal("no current item")
	}
}

func TestNavigationAndSelection(t *testing.T) {
	h := newHarness(t, 140, 40)
	h.keys("j", "j", "space", "space")
	if got := len(h.app.sprint.selected); got != 2 {
		t.Fatalf("selected = %d, want 2", got)
	}
	h.dump("02-selected")
	h.keys("esc")
	if len(h.app.sprint.selected) != 0 {
		t.Fatal("esc should clear selection")
	}
	h.app.sprint.jumpTo(1002)
	h.keys("v", "j", "j")
	if got := len(h.app.sprint.selected); got != 3 {
		t.Fatalf("visual selected = %d, want 3", got)
	}
	h.dump("03-visual")
}

func TestBulkMoveToNextSprint(t *testing.T) {
	h := newHarness(t, 140, 40)
	// Jump to a PBI without children and select it plus the next row.
	h.app.sprint.jumpTo(1003)
	h.keys("space", "space") // selects 1003 and 1004
	h.keys("M")
	if h.app.popup == nil {
		t.Fatal("expected confirm popup")
	}
	h.dump("04-confirm-move")
	h.keys("y")
	if len(h.fake.Updates) != 2 {
		t.Fatalf("updates = %d, want 2: %+v", len(h.fake.Updates), h.fake.Updates)
	}
	for _, u := range h.fake.Updates {
		if u.Patches[0].Value != "Platform\\Sprint 43" {
			t.Errorf("patch = %+v", u.Patches[0])
		}
	}
	if _, ok := h.app.sprint.tree.Get(1003); ok {
		t.Error("moved item should have left the sprint after reload")
	}
	if len(h.app.sprint.selected) != 0 {
		t.Error("selection should clear after a bulk action")
	}
	h.dump("05-after-move")
}

func TestMoveFeatureOffersChildren(t *testing.T) {
	h := newHarness(t, 140, 40)
	h.app.sprint.jumpTo(1012) // feature with 2 PBIs + bug + tasks below
	h.keys("m")
	h.keys("enter") // first entry = Backlog
	if _, ok := h.app.popup.(*choice); !ok {
		t.Fatalf("expected choice popup, got %T", h.app.popup)
	}
	h.dump("06-choice-children")
	h.keys("y")
	if len(h.fake.Updates) < 4 {
		t.Fatalf("expected feature + descendants moved, got %d", len(h.fake.Updates))
	}
}

func TestQuickStateChange(t *testing.T) {
	h := newHarness(t, 140, 40)
	h.app.sprint.jumpTo(1004)
	h.keys("s")
	if _, ok := h.app.popup.(*picker); !ok {
		t.Fatalf("expected picker, got %T", h.app.popup)
	}
	h.keys("d", "o", "n", "e", "enter")
	if len(h.fake.Updates) != 1 || h.fake.Updates[0].Patches[0].Value != "Done" {
		t.Fatalf("updates = %+v", h.fake.Updates)
	}
	if it := h.app.lookup(1004); it != nil && it.State != "Done" {
		t.Fatalf("local state not applied: %s", it.State)
	}
}

func TestFormEdit(t *testing.T) {
	h := newHarness(t, 140, 40)
	h.app.sprint.jumpTo(1004)
	h.keys("e")
	f, ok := h.app.popup.(*form)
	if !ok {
		t.Fatalf("expected form, got %T", h.app.popup)
	}
	h.dump("07-form")
	h.keys("enter") // title prompt
	h.keys(" ", "v", "2", "enter")
	if f.values["System.Title"] != "Accept invite and create account v2" {
		t.Fatalf("title value = %v", f.values["System.Title"])
	}
	h.keys("j", "j", "j", "j", "enter", "8", "enter") // effort
	h.dump("08-form-changed")
	h.keys("ctrl+s")
	if len(h.fake.Updates) != 1 || len(h.fake.Updates[0].Patches) != 2 {
		t.Fatalf("updates = %+v", h.fake.Updates)
	}
}

func TestViewsAndPopups(t *testing.T) {
	h := newHarness(t, 140, 40)
	h.keys("3")
	h.dump("09-board")
	if !strings.Contains(h.app.View(), "Committed") {
		t.Error("board missing column")
	}
	h.keys("l", "L") // move column right: single write, no confirm
	if len(h.fake.Updates) != 1 || h.fake.Updates[0].Patches[0].Value != "Committed" {
		t.Fatalf("column move updates = %+v", h.fake.Updates)
	}
	if h.app.popup != nil {
		t.Fatalf("unexpected popup %T", h.app.popup)
	}
	h.keys("1")
	h.dump("10-dash")
	if !strings.Contains(h.app.View(), "elsewhere") {
		t.Error("dash missing summary")
	}
	h.keys("4")
	h.dump("11-backlog")
	h.keys("2", "?")
	h.dump("12-help")
	h.keys("esc", ":")
	h.keys("s", "p", "r", "i", "n", "t", "enter")
	if _, ok := h.app.popup.(*picker); !ok {
		t.Fatalf("expected sprint picker, got %T", h.app.popup)
	}
	h.dump("13-sprint-picker")
	h.keys("esc", "/")
	h.keys("t", "r", "a", "c", "e")
	h.dump("14-filter")
	if n := len(h.app.sprint.rows); n == 0 || n > 6 {
		t.Errorf("filter rows = %d", n)
	}
}

// Data can arrive before the terminal reports its size; that must not panic.
func TestDataBeforeWindowSize(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	f := ado.NewFake()
	f.Latency = 0
	a := New(f, config.Config{Org: "x", Project: "Platform", Team: "Team Blue"}, os.DevNull, false)
	h := &harness{t: t, app: a, fake: f}
	h.run(a.loadContext())
	if a.View() != "loading…" {
		t.Fatalf("unexpected view before size: %q", a.View())
	}
	h.send(tea.WindowSizeMsg{Width: 120, Height: 30})
	if !strings.Contains(a.View(), "Invite flow") {
		t.Fatal("items not rendered after window size arrived")
	}
}

func TestNarrowLayout(t *testing.T) {
	h := newHarness(t, 90, 28)
	h.dump("15-narrow")
	h.keys("tab")
	h.dump("16-narrow-detail")
	if !strings.Contains(h.app.View(), "Assigned") {
		t.Error("narrow detail not shown")
	}
}
