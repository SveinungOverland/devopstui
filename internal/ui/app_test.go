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
	"github.com/sveinungoverland/devopstui/internal/markdown"
	"github.com/sveinungoverland/devopstui/internal/model"
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
	markdown.Style = "ascii"
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
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
	v := h.app.View()
	if !strings.Contains(v, "Committed") {
		t.Error("board missing column")
	}
	if !strings.Contains(v, "Rotate signing keys quarterly.") { // description of the highlighted card
		t.Error("board preview pane missing")
	}
	h.keys("z")
	h.dump("23-board-nopreview")
	if strings.Contains(h.app.View(), "Rotate signing keys quarterly.") {
		t.Error("z should hide the board preview")
	}
	h.keys("z", "l")
	if !strings.Contains(h.app.View(), "Accept invite and create account.") {
		t.Error("preview should follow the cursor after toggling back on")
	}
	h.keys("h") // back to the first column
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

func TestDescriptionInlineEditor(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1003)
	h.app.refreshDetail()
	h.dump("20-detail-markdown")
	if v := h.app.View(); !strings.Contains(v, "Steps to reproduce") || !strings.Contains(v, "token expired") {
		t.Fatal("rendered markdown missing from detail pane")
	}
	h.keys("d")
	ed, ok := h.app.popup.(*mdEditor)
	if !ok {
		t.Fatalf("expected built-in editor, got %T", h.app.popup)
	}
	h.dump("17-desc-editor")
	if !ed.showPrev || !strings.Contains(h.app.View(), "ctrl+p") {
		t.Error("preview should be on by default")
	}
	h.keys("ctrl+p")
	h.dump("18-desc-editor-noprev")
	h.keys("q")
	if h.app.popup != nil || len(h.fake.Updates) != 0 {
		t.Fatal("q must close without saving")
	}
	// Typing in normal mode must not change the text.
	h.keys("d", "x", "y", "z")
	if ed := h.app.popup.(*mdEditor); ed.changed() {
		t.Fatal("normal mode must not insert text")
	}
	// Enter insert mode, prepend a line, back to normal, save.
	h.keys("i", "N", "e", "w", " ", "l", "i", "n", "e", "enter", "esc")
	if ed := h.app.popup.(*mdEditor); ed.insert {
		t.Fatal("esc should leave insert mode")
	}
	h.dump("21-desc-editor-modified")
	h.keys("ctrl+s")
	if len(h.fake.Updates) != 1 || h.fake.Updates[0].Patches[0].Field != "System.Description" {
		t.Fatalf("updates = %+v", h.fake.Updates)
	}
	if v := h.fake.Updates[0].Patches[0].Value.(string); !strings.HasPrefix(v, "New line\n") {
		t.Fatalf("description = %q", v)
	}
	if it := h.app.lookup(1003); !strings.HasPrefix(it.Description, "New line") {
		t.Fatal("local item not updated")
	}
}

func TestEditorCheckboxToggle(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1002) // template with "- [ ] Happy path" on line 7
	h.keys("d")
	ed := h.app.popup.(*mdEditor)
	h.keys("j", "j", "j", "j", "j", "j")
	if ed.area.Line() != 6 {
		t.Fatalf("line = %d, want 6 (j must move by logical line)", ed.area.Line())
	}
	h.keys("space", "j", "j", "space") // tick first, untick third ([x] -> [ ])
	v := ed.area.Value()
	if !strings.Contains(v, "- [x] Happy path") || !strings.Contains(v, "- [ ] Telemetry") {
		t.Fatalf("checkboxes not toggled:\n%s", v)
	}
	h.keys("k", "k", "k", "k", "k", "k", "k", "k") // back to the heading line
	h.keys("space")
	if ed.area.Line() != 0 || strings.Contains(ed.area.Value(), "[ ] ## Goal") {
		t.Fatal("space on a non-list line must do nothing")
	}
	// Unsaved changes: first q warns, second q discards.
	h.keys("q")
	if h.app.popup == nil || !ed.quitArm {
		t.Fatal("first q should warn about unsaved changes")
	}
	h.dump("22-desc-editor-quit-warn")
	h.keys("q")
	if h.app.popup != nil || len(h.fake.Updates) != 0 {
		t.Fatal("second q should discard")
	}
	// Do it again and save this time.
	h.keys("d", "j", "j", "j", "j", "j", "j", "space", "ctrl+s")
	if len(h.fake.Updates) != 1 || !strings.Contains(h.fake.Updates[0].Patches[0].Value.(string), "- [x] Happy path") {
		t.Fatalf("updates = %+v", h.fake.Updates)
	}
}

func TestEditorLineOps(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1002) // "## Goal", "", "<text>", "", "### Acceptance criteria", "", "- [ ] Happy…", …
	h.keys("d")
	ed := h.app.popup.(*mdEditor)
	before := strings.Split(ed.area.Value(), "\n")

	// yy on line 1, p pastes it below and moves the cursor onto the copy.
	h.keys("y", "y", "p")
	got := strings.Split(ed.area.Value(), "\n")
	if len(got) != len(before)+1 || got[1] != "## Goal" || ed.area.Line() != 1 {
		t.Fatalf("yy/p: line=%d got=%q", ed.area.Line(), got[:3])
	}
	// dd removes the copy again; buffer is back to the original.
	h.keys("d", "d")
	if ed.area.Value() != strings.Join(before, "\n") || ed.register != "## Goal" {
		t.Fatalf("dd: value differs or register=%q", ed.register)
	}
	// P pastes above the current line (cursor is on line 1 after dd, so j j → 3).
	h.keys("j", "j", "P")
	got = strings.Split(ed.area.Value(), "\n")
	if got[3] != "## Goal" || ed.area.Line() != 3 {
		t.Fatalf("P: line=%d got=%q", ed.area.Line(), got[:4])
	}
	// A pending d followed by something else cancels, and is not typed.
	h.keys("d", "j")
	if strings.Contains(ed.area.Value(), "j") && strings.Count(ed.area.Value(), "\n") != len(got)-1 {
		t.Fatal("cancelled command changed the buffer")
	}
	if ed.pending != "" {
		t.Fatal("pending should clear")
	}
	// dd on every line leaves a single empty line, never panics.
	for i := 0; i < len(got)+2; i++ {
		h.keys("d", "d")
	}
	if ed.area.Value() != "" {
		t.Fatalf("expected empty buffer, got %q", ed.area.Value())
	}
	h.keys("p")
	if ed.area.Value() != "\n"+ed.register {
		t.Fatalf("paste into empty buffer = %q", ed.area.Value())
	}
}

func TestFormDescriptionUsesEditor(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1004)
	h.keys("e", "G") // G is not bound in the form; move to last field with j
	h.keys("j", "j", "j", "j", "j", "j", "enter")
	f, ok := h.app.popup.(*form)
	if !ok {
		t.Fatalf("expected form, got %T", h.app.popup)
	}
	if _, ok := f.child.(*mdEditor); !ok {
		t.Fatalf("expected editor child, got %T", f.child)
	}
	h.dump("19-form-desc-editor")
	h.keys("i", "X", "esc", "ctrl+s")
	if v, ok := f.values["System.Description"].(string); !ok || !strings.HasPrefix(v, "X") {
		t.Fatalf("form value = %v", f.values["System.Description"])
	}
	h.keys("ctrl+s")
	if len(h.fake.Updates) != 1 {
		t.Fatalf("updates = %+v", h.fake.Updates)
	}
}

func TestExternalEditorSelection(t *testing.T) {
	h := newHarness(t, 160, 45)
	t.Setenv("EDITOR", "true")
	h.app.sprint.jumpTo(1003)
	cmd := h.app.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if h.app.popup != nil {
		t.Fatalf("external editor should not open a popup, got %T", h.app.popup)
	}
	if cmd == nil {
		t.Fatal("expected an exec command")
	}
}

func TestChildProgress(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1013) // PBI with tasks 1015 (To Do), 1016 (Done), 1017 (To Do)
	h.app.refreshDetail()
	v := h.app.View()
	h.dump("24-progress")
	if !strings.Contains(v, "1/3") {
		t.Error("tree row should show 1/3 task progress")
	}
	if !strings.Contains(v, "1/3 tasks done") {
		t.Error("detail should summarise tasks")
	}
	if !strings.Contains(v, "Load test with tracing on") {
		t.Error("detail should list child tasks")
	}
	if !strings.Contains(v, "h remaining") {
		t.Error("detail should sum remaining work")
	}
	// Hidden done tasks still count: toggle closed off (default) and check badge unchanged.
	h.keys("c")
	if !strings.Contains(h.app.View(), "1/3") {
		t.Error("badge must count hidden done tasks")
	}
	// Board card shows the badge too.
	h.keys("3")
	h.app.board.jumpTo(1013)
	if !strings.Contains(h.app.View(), "1013 1/3") {
		t.Errorf("board card missing progress badge")
	}
}

func TestBugsAsTasksLeaveTheBoard(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.ctx.Backlog.BugsBehavior = "asTasks"
	h.app.board.setItems(h.app.currentBoard(), h.app.sprint.all, h.app.ctx.Backlog, nil)
	h.keys("3")
	for _, col := range h.app.board.cols {
		for _, it := range col {
			if it.Kind == 5 { // KindBug
				t.Fatalf("bug %d should not be a card when bugs are tasks", it.ID)
			}
		}
	}
	// And it counts towards its parent's progress.
	h.app.sprint.rebuild()
	if p := h.app.sprint.progress[1012]; p.total != 1 { // bug 1020 under feature 1012
		t.Errorf("feature progress = %+v, want 1 bug counted", p)
	}
}

func TestCreateChild(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1013) // PBI → new task
	h.keys("n")
	p, ok := h.app.popup.(*prompt)
	if !ok {
		t.Fatalf("expected title prompt, got %T", h.app.popup)
	}
	if !strings.Contains(p.title, "New Task under #1013") {
		t.Errorf("prompt title = %q", p.title)
	}
	h.keys("W", "r", "i", "t", "e", " ", "d", "o", "c", "s", "enter")
	if len(h.fake.Updates) != 1 || h.fake.Updates[0].Parent != 1013 {
		t.Fatalf("create not recorded: %+v", h.fake.Updates)
	}
	created := h.fake.Updates[0].ID
	if got := h.app.sprint.currentID(); got != created {
		t.Errorf("cursor should land on the new item %d, got %d", created, got)
	}
	if it := h.app.lookup(created); it == nil || it.Type != "Task" || it.Title != "Write docs" || it.IterationPath != "Platform\\Sprint 42" {
		t.Fatalf("created item = %+v", it)
	}
	// On a task, n creates a sibling under the same PBI.
	h.keys("n")
	if p := h.app.popup.(*prompt); !strings.Contains(p.title, "under #1013") {
		t.Errorf("sibling prompt title = %q", p.title)
	}
	h.keys("esc")
	// Feature → PBI, Epic → Feature.
	h.app.sprint.jumpTo(1012)
	h.keys("n")
	if p := h.app.popup.(*prompt); !strings.HasPrefix(p.title, "New Product Backlog Item") {
		t.Errorf("feature child title = %q", p.title)
	}
	h.keys("esc")
	h.app.sprint.jumpTo(1010)
	h.keys("n")
	if p := h.app.popup.(*prompt); !strings.HasPrefix(p.title, "New Feature") {
		t.Errorf("epic child title = %q", p.title)
	}
	h.keys("esc")
}

func TestDashboardShowsParent(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.keys("1")
	h.dump("25-dash-parents")
	v := h.app.View()
	if !strings.Contains(v, "Add trace header to publisher  ↑ Propagate") {
		t.Error("task row on the dashboard should keep its title and show its parent PBI")
	}
	if !strings.Contains(v, "1/3") {
		t.Error("dashboard badge should count all loaded tasks, not only mine")
	}
}

func TestIterationFallbacks(t *testing.T) {
	h := newHarness(t, 140, 40)
	a := h.app
	now := time.Now()
	mk := func(name, tf string, startOff int) model.Iteration {
		return model.Iteration{Name: name, Path: "P\\" + name, Timeframe: tf, Start: now.AddDate(0, 0, startOff), Finish: now.AddDate(0, 0, startOff+13)}
	}
	// No "current" flag: pick by dates.
	a.iterations = []model.Iteration{mk("S1", "past", -30), mk("S2", "", -3), mk("S3", "future", 11)}
	if got := a.currentIteration().Name; got != "S2" {
		t.Errorf("by dates = %s, want S2", got)
	}
	// Nothing covers today: first future.
	a.iterations = []model.Iteration{mk("S1", "past", -30), mk("S3", "future", 11)}
	if got := a.currentIteration().Name; got != "S3" {
		t.Errorf("first future = %s, want S3", got)
	}
	// Entries without a path are skipped.
	a.iterations = []model.Iteration{{Name: "broken", Timeframe: "current"}, mk("S1", "past", -30)}
	if got := a.currentIteration().Name; got != "S1" {
		t.Errorf("skip empty path = %s, want S1", got)
	}
	// No iterations at all: no query is issued, the user gets a message.
	a.iterations = nil
	a.ctx.Iteration = model.Iteration{}
	h.run(a.loadView(viewSprint))
	if a.loading[viewSprint] {
		t.Error("must not start a sprint load with an empty path")
	}
	if !strings.Contains(a.flash, "no sprints") {
		t.Errorf("flash = %q", a.flash)
	}
}

func TestTeamFilter(t *testing.T) {
	h := newHarness(t, 160, 45)
	a := h.app
	total := len(a.sprint.rows)
	h.keys("T")
	if _, ok := a.popup.(*picker); !ok {
		t.Fatalf("expected team picker, got %T", a.popup)
	}
	h.keys("g", "r", "e", "e", "n", "enter")
	if a.ctx.FilterTeam != "Team Green" || len(a.ctx.FilterAreas) == 0 {
		t.Fatalf("filter not applied: %+v", a.ctx.FilterTeam)
	}
	h.dump("26-team-filter")
	v := h.app.View()
	if !strings.Contains(v, "⌕ Team Green") {
		t.Error("header should show the filter")
	}
	// Green owns 1014 (+tasks 1018/1019), 1021, 1022. Parents 1012/1010 show dimmed.
	ids := map[int]bool{}
	for _, r := range a.sprint.rows {
		ids[r.Item.ID] = true
		if r.Item.ID == 1013 || r.Item.ID == 1003 {
			t.Errorf("item %d belongs to another team and must be hidden", r.Item.ID)
		}
	}
	for _, want := range []int{1014, 1018, 1021, 1022, 1012, 1010} {
		if !ids[want] {
			t.Errorf("item %d missing from filtered sprint", want)
		}
	}
	if n, ok := a.sprint.tree.Get(1012); !ok || !n.External {
		t.Error("parent from another team should be shown dimmed")
	}
	if len(a.sprint.rows) >= total {
		t.Error("filter should reduce the row count")
	}
	// Board and dashboard are filtered too.
	h.keys("3")
	for _, col := range a.board.cols {
		for _, it := range col {
			if it.AreaPath != "Platform\\Green" {
				t.Errorf("board card %d outside the filter", it.ID)
			}
		}
	}
	h.keys("1")
	for _, r := range a.dash.rows {
		if r.Item.AreaPath != "Platform\\Green" {
			t.Errorf("dashboard row %d outside the filter", r.Item.ID)
		}
	}
	// New items land in the filtered team's area.
	h.keys("2")
	a.sprint.jumpTo(1014)
	h.keys("n", "x", "enter")
	if it := a.lookup(h.fake.Updates[len(h.fake.Updates)-1].ID); it == nil || it.AreaPath != "Platform\\Green" {
		t.Errorf("created item area = %+v", it)
	}
	// :filter off clears it and the config remembers the state.
	h.keys(":")
	h.keys("f", "i", "l", "t", "e", "r", " ", "o", "f", "f", "enter")
	if a.ctx.FilterTeam != "" || len(a.sprint.rows) < total {
		t.Errorf("filter not cleared: %q rows=%d", a.ctx.FilterTeam, len(a.sprint.rows))
	}
}

func TestTeamFilterFromConfig(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	f := ado.NewFake()
	f.Latency = 0
	a := New(f, config.Config{Org: "x", Project: "Platform", Team: "Team Blue", FilterTeam: "Team Green"}, os.DevNull, false)
	h := &harness{t: t, app: a, fake: f}
	h.send(tea.WindowSizeMsg{Width: 140, Height: 40})
	h.run(a.loadContext())
	if a.ctx.FilterTeam != "Team Green" || a.include() == nil {
		t.Fatal("filter from config not active after context load")
	}
	for _, r := range a.sprint.rows {
		if !r.External && r.Item.AreaPath != "Platform\\Green" {
			t.Errorf("row %d outside filter", r.Item.ID)
		}
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
