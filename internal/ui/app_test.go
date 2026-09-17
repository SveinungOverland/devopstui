package ui

import (
	"errors"
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
		case "ctrl+w":
			msg = tea.KeyMsg{Type: tea.KeyCtrlW}
		case "ctrl+u":
			msg = tea.KeyMsg{Type: tea.KeyCtrlU}
		case "ctrl+d":
			msg = tea.KeyMsg{Type: tea.KeyCtrlD}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		h.send(msg)
	}
}

var errTest = errors.New("identity service unavailable")

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

func TestAssigneeSearch(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1004)
	h.keys("a")
	p, ok := h.app.popup.(*picker)
	if !ok {
		t.Fatalf("expected a people picker, got %T", h.app.popup)
	}
	// Opens with Unassigned plus people already on screen, before any search.
	labels := func() []string {
		var out []string
		for _, i := range p.shown {
			out = append(out, p.items[i].Label)
		}
		return out
	}
	if got := labels(); len(got) < 2 || got[0] != "Unassigned" {
		t.Fatalf("initial list = %v", got)
	}
	h.dump("30-assignee-picker")

	// Someone who is on no loaded item and no team is still findable.
	h.keys("g", "r", "a", "c", "e")
	got := labels()
	if len(got) != 1 || got[0] != "Grace Hopper" {
		t.Fatalf("search for grace = %v", got)
	}
	if !strings.Contains(h.app.View(), "grace.hopper@contoso.com") {
		t.Error("picker should show the sign-in address")
	}
	h.keys("enter")

	// The write uses the sign-in address, and the item shows the name.
	last := h.fake.Updates[len(h.fake.Updates)-1]
	if last.Patches[0].Field != "System.AssignedTo" || last.Patches[0].Value != "grace.hopper@contoso.com" {
		t.Fatalf("patch = %+v", last.Patches[0])
	}
	if it := h.app.lookup(1004); it == nil || it.AssignedTo != "Grace Hopper" {
		t.Fatalf("assignee = %+v", it)
	}
}

func TestAssigneeSearchIgnoresStaleResults(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1004)
	h.keys("a")
	p := h.app.popup.(*picker)
	h.keys("o", "l", "a")
	before := len(p.items)
	// A response for an older query must not replace the current list.
	p.applyResults(pickerItemsMsg{token: p.token, query: "gra", items: peopleItems([]model.Person{{DisplayName: "Grace Hopper"}})})
	if len(p.items) != before {
		t.Error("stale results should be ignored")
	}
	// So must a response addressed to a different picker.
	p.applyResults(pickerItemsMsg{token: p.token + 99, query: "ola", items: nil})
	if len(p.items) != before {
		t.Error("results for another picker should be ignored")
	}
	// A matching response is applied, keeping the fixed Unassigned entry.
	p.applyResults(pickerItemsMsg{token: p.token, query: "ola", items: peopleItems([]model.Person{{DisplayName: "Ola Nordmann"}})})
	if len(p.items) != 2 || p.items[0].Label != "Unassigned" || p.items[1].Label != "Ola Nordmann" {
		t.Fatalf("items = %+v", p.items)
	}
}

func TestAssigneeSearchError(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1004)
	h.keys("a")
	p := h.app.popup.(*picker)
	h.fake.FailNext = errTest
	h.keys("x")
	if p.searchErr == "" {
		t.Fatal("search failure should be shown in the picker")
	}
	if h.app.popup == nil {
		t.Error("a failed search must not close the picker")
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
	h.keys("h")      // back to the first column
	h.keys("l", "L") // move column right: single write, no confirm
	if len(h.fake.Updates) != 1 || h.fake.Updates[0].Patches[0].Value != "Committed" {
		t.Fatalf("column move updates = %+v", h.fake.Updates)
	}
	if h.app.popup != nil {
		t.Fatalf("unexpected popup %T", h.app.popup)
	}
	h.keys("1")
	h.dump("10-dash")
	if !strings.Contains(h.app.View(), "PBI(s) assigned to you") {
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

func TestEditorSaveWithoutClosing(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1003)
	h.keys("d")
	ed, ok := h.app.popup.(*mdEditor)
	if !ok {
		t.Fatalf("expected built-in editor, got %T", h.app.popup)
	}
	// Nothing typed yet: ctrl+w must not write.
	h.keys("ctrl+w")
	if len(h.fake.Updates) != 0 {
		t.Fatalf("unchanged buffer wrote %+v", h.fake.Updates)
	}

	h.keys("i", "A", "esc", "ctrl+w")
	if h.app.popup != ed {
		t.Fatalf("ctrl+w must keep the editor open, popup = %T", h.app.popup)
	}
	if len(h.fake.Updates) != 1 || !strings.HasPrefix(h.fake.Updates[0].Patches[0].Value.(string), "A") {
		t.Fatalf("updates = %+v", h.fake.Updates)
	}
	if ed.changed() || !ed.saved || ed.inflight != "" {
		t.Fatalf("after a landed write: changed=%v saved=%v inflight=%q", ed.changed(), ed.saved, ed.inflight)
	}
	h.dump("23-desc-editor-saved")

	// A second write from the same editor must carry the new revision
	// rather than the one captured when it opened.
	h.keys("i", "B", "esc", "ctrl+w")
	if len(h.fake.Updates) != 2 {
		t.Fatalf("second write did not land: %+v (flash %q)", h.fake.Updates, h.app.flash)
	}
	if v := h.fake.Updates[1].Patches[0].Value.(string); !strings.HasPrefix(v, "AB") {
		t.Fatalf("second description = %q", v)
	}
	if h.app.flashErr {
		t.Fatalf("second write flashed an error: %q", h.app.flash)
	}
	if it := h.app.lookup(1003); !strings.HasPrefix(it.Description, "AB") {
		t.Fatalf("local item = %q", it.Description)
	}

	// Everything is saved, so q closes without arming the discard warning.
	h.keys("q")
	if h.app.popup != nil {
		t.Fatalf("q should close a saved editor, popup = %T", h.app.popup)
	}
}

func TestEditorFailedSaveKeepsChanges(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1003)
	h.keys("d")
	ed := h.app.popup.(*mdEditor)
	h.fake.FailNext = errTest
	h.keys("i", "A", "esc", "ctrl+w")
	if !ed.changed() || ed.saved || ed.inflight != "" {
		t.Fatalf("a failed write must stay unsaved: changed=%v saved=%v inflight=%q", ed.changed(), ed.saved, ed.inflight)
	}
	if !h.app.flashErr {
		t.Fatalf("expected an error flash, got %q", h.app.flash)
	}
	// The text is still unsaved, so q warns before discarding it.
	h.keys("q")
	if h.app.popup == nil || !ed.quitArm {
		t.Fatal("q should warn after a failed save")
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
	// ctrl+w hands the text to the form without closing the editor; nothing
	// reaches the server until the form itself saves.
	h.keys("i", "X", "esc", "ctrl+w")
	if _, ok := f.child.(*mdEditor); !ok {
		t.Fatalf("ctrl+w must keep the editor open, child = %T", f.child)
	}
	if v, _ := f.values["System.Description"].(string); !strings.HasPrefix(v, "X") {
		t.Fatalf("form value after ctrl+w = %v", f.values["System.Description"])
	}
	if len(h.fake.Updates) != 0 {
		t.Fatalf("ctrl+w in the form must not write: %+v", h.fake.Updates)
	}
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

func TestNewTaskInheritsParentAssignee(t *testing.T) {
	h := newHarness(t, 160, 45)

	// PBI 1013 is assigned to Sveinung Øverland; a new task under it goes
	// to the same person, written by sign-in address.
	h.app.sprint.jumpTo(1013)
	h.keys("n")
	if p := h.app.popup.(*prompt); !strings.Contains(p.title, "Sveinung Øverland") {
		t.Errorf("prompt should name the inherited assignee, got %q", p.title)
	}
	h.dump("31-new-task-inherits")
	h.keys("W", "r", "i", "t", "e", "enter")
	last := h.fake.Updates[len(h.fake.Updates)-1]
	created := h.app.lookup(last.ID)
	if created == nil || created.AssignedTo != "Sveinung Øverland" {
		t.Fatalf("created task = %+v", created)
	}
	var assigned any
	for _, p := range last.Patches {
		if p.Field == "System.AssignedTo" {
			assigned = p.Value
		}
	}
	if assigned != "sveinung@contoso.com" {
		t.Errorf("assignment should use the sign-in address, got %v", assigned)
	}

	// A sibling created from a task inherits from the PBI, not the task.
	h.app.sprint.jumpTo(1014) // PBI assigned to Alex Kim, task 1018 below it
	h.keys("n")
	if p := h.app.popup.(*prompt); !strings.Contains(p.title, "Alex Kim") {
		t.Errorf("sibling prompt = %q", p.title)
	}
	h.keys("esc")
}

func TestNewChildAssigneeInheritanceLimits(t *testing.T) {
	h := newHarness(t, 160, 45)

	// An unassigned PBI leaves the new task unassigned.
	h.app.sprint.jumpTo(1021) // PBI with no assignee
	h.keys("n")
	if p := h.app.popup.(*prompt); strings.Contains(p.title, "→") {
		t.Errorf("nothing to inherit, got %q", p.title)
	}
	h.keys("x", "enter")
	created := h.app.lookup(h.fake.Updates[len(h.fake.Updates)-1].ID)
	if created == nil || created.AssignedTo != "" {
		t.Fatalf("task should be unassigned, got %+v", created)
	}

	// Above the task level nothing is inherited: a PBI created under a
	// Feature does not pick up the Feature's assignee.
	h.app.sprint.jumpTo(1012) // Feature assigned to Sveinung Øverland
	h.keys("n")
	p := h.app.popup.(*prompt)
	if !strings.HasPrefix(p.title, "New Product Backlog Item") {
		t.Fatalf("prompt = %q", p.title)
	}
	if strings.Contains(p.title, "→") {
		t.Errorf("a requirement should not inherit its Feature's assignee, got %q", p.title)
	}
	h.keys("y", "enter")
	created = h.app.lookup(h.fake.Updates[len(h.fake.Updates)-1].ID)
	if created == nil || created.AssignedTo != "" {
		t.Fatalf("new PBI should be unassigned, got %+v", created)
	}
}

func TestDashboardKanbanAndLanes(t *testing.T) {
	h := newHarness(t, 160, 45)
	a := h.app
	h.keys("1")
	h.dump("25-dash-kanban-lanes")

	// The kanban only holds requirement-level items assigned to @Me: PBIs
	// 1003/1013 and bug 1020, never the Epics/Features/Tasks also assigned
	// to Sveinung (1002, 1012, 1015).
	got := map[int]bool{}
	for _, col := range a.dashBoard.cols {
		for _, it := range col {
			got[it.ID] = true
		}
	}
	for _, want := range []int{1003, 1013, 1020} {
		if !got[want] {
			t.Errorf("kanban missing my PBI #%d", want)
		}
	}
	for _, unwanted := range []int{1002, 1012, 1015} {
		if got[unwanted] {
			t.Errorf("kanban should not show #%d (Epic/Feature/Task)", unwanted)
		}
	}

	// The lanes are a kanban with state as columns and the parent PBI as
	// the swimlane: a PBI with children gets a lane showing ALL of them,
	// bucketed by state, not just the active ones. 1003 has one child
	// (1023, In Progress); 1013 has three (1015/1017 To Do, 1016 Done).
	// 1020's only child is a bug (1024), which lanes drop entirely (see
	// TestDashboardLanesExcludeBugs), so it also gets no lane.
	if len(a.dashLanes.ls) != 2 {
		t.Fatalf("lanes = %+v, want exactly 2 (#1003 and #1013)", a.dashLanes.ls)
	}
	byParent := map[int]lane{}
	for _, l := range a.dashLanes.ls {
		byParent[l.parent.ID] = l
	}
	colOf := func(state string) int {
		for i, s := range a.dashLanes.states {
			if s == state {
				return i
			}
		}
		t.Fatalf("state %q missing from lane columns %v", state, a.dashLanes.states)
		return -1
	}
	toDo, inProgress, done := colOf("To Do"), colOf("In Progress"), colOf("Done")

	l1003 := byParent[1003]
	if got := l1003.cols[inProgress]; len(got) != 1 || got[0].ID != 1023 {
		t.Fatalf("#1003 In Progress column = %+v, want just #1023", got)
	}
	l1013 := byParent[1013]
	if got := l1013.cols[toDo]; len(got) != 2 {
		t.Fatalf("#1013 To Do column = %+v, want 2 items (1015, 1017)", got)
	}
	if got := l1013.cols[done]; len(got) != 1 || got[0].ID != 1016 {
		t.Fatalf("#1013 Done column = %+v, want just #1016", got)
	}

	v := h.app.View()
	if !strings.Contains(v, "Send invite email with magic link") { // #1003's title, its lane header
		t.Error("lane header should show the parent PBI's title")
	}
	if !strings.Contains(v, "TASK 1023") || !strings.Contains(v, "Wire up") {
		t.Error("lane should show the child's card")
	}
	if !strings.Contains(v, "To Do") || !strings.Contains(v, "In Progress") || !strings.Contains(v, "Done") {
		t.Error("lanes should show the shared state columns")
	}
	// #1013's To Do column has two cards (1015, 1017): both must render in
	// full, not collapse the second one into a "+1" badge.
	if !strings.Contains(v, "TASK 1015") || !strings.Contains(v, "TASK 1017") {
		t.Error("every card in a cell should render, not just the first with a +N badge")
	}
	if strings.Contains(v, "+1") || strings.Contains(v, "+2") {
		t.Error("lanes should not collapse extra cards behind a +N badge")
	}
}

func TestDashboardFocusAndActions(t *testing.T) {
	h := newHarness(t, 160, 45)
	a := h.app
	h.keys("1")

	if a.dashFocusLanes {
		t.Fatal("dashboard should start focused on the kanban")
	}
	a.dashBoard.jumpTo(1020)

	h.keys("tab")
	if !a.dashFocusLanes {
		t.Fatal("tab should move focus to the lanes")
	}
	// The lanes' default cursor (lane 0, "To Do") is an empty cell for
	// #1003 (its only child is In Progress); jump straight to it.
	a.dashLanes.jumpTo(1023)
	if it := a.currentItem(); it == nil || it.ID != 1023 {
		t.Fatalf("lane cursor = %v, want #1023", it)
	}

	h.keys("D")
	if a.view != viewItem || a.item == nil || a.item.item.ID != 1023 {
		t.Fatalf("D should drill into the lane card, got view=%v item=%v", a.view, a.item)
	}
	h.keys("esc")
	if a.view != viewDash || !a.dashFocusLanes {
		t.Fatalf("esc should return to the dashboard with lanes still focused")
	}

	// The preview pane shows #1023 (the lane cursor); it is never itself a
	// focusable pane, so tab only ever toggles between the kanban and the
	// lanes (see TestPreviewScroll for ctrl+u/ctrl+d scrolling it while
	// the lanes have focus).
	if !strings.Contains(h.app.View(), "Wire up magic link email template") { // #1023's title, in the preview pane
		t.Error("preview pane should show the highlighted lane card")
	}
	if a.focusDetail {
		t.Fatal("the preview pane must never become focusable on the Dashboard")
	}

	h.keys("tab") // lanes -> kanban (a 2-way toggle; the preview is not in the cycle)
	if a.focusDetail || a.dashFocusLanes {
		t.Fatalf("tab from lanes should move focus back to the kanban, focusDetail=%v dashFocusLanes=%v", a.focusDetail, a.dashFocusLanes)
	}
	if it := a.currentItem(); it == nil || it.ID != 1020 {
		t.Fatalf("kanban cursor after refocus = %v, want #1020", it)
	}

	// A state change from the kanban updates the card's column.
	h.keys("s")
	if _, ok := a.popup.(*picker); !ok {
		t.Fatalf("expected state picker, got %T", a.popup)
	}
	h.keys("enter") // first state in the list
	if len(h.fake.Updates) != 1 || h.fake.Updates[0].ID != 1020 {
		t.Fatalf("updates = %+v", h.fake.Updates)
	}
}

func TestDashboardColumnMove(t *testing.T) {
	h := newHarness(t, 160, 45)
	a := h.app
	h.keys("1")
	a.dashBoard.jumpTo(1003) // Committed

	h.keys("L") // move column right: single write, no confirm
	if len(h.fake.Updates) != 1 || h.fake.Updates[0].ID != 1003 || h.fake.Updates[0].Patches[0].Value != "In Progress" {
		t.Fatalf("column move updates = %+v", h.fake.Updates)
	}
	if h.app.popup != nil {
		t.Fatalf("unexpected popup %T", h.app.popup)
	}
	if it := a.currentItem(); it == nil || it.ID != 1003 || it.State != "In Progress" {
		t.Fatalf("cursor should follow the moved card, got %v", it)
	}

	h.keys("H") // move back
	if len(h.fake.Updates) != 2 || h.fake.Updates[1].Patches[0].Value != "Committed" {
		t.Fatalf("column move back = %+v", h.fake.Updates)
	}

	// A column move must not touch the lanes or their focus.
	if a.dashFocusLanes {
		t.Error("column move should not change focus")
	}
}

func TestDashboardLanesExcludeBugs(t *testing.T) {
	h := newHarness(t, 160, 45)
	a := h.app
	h.keys("1")

	// #1024 is a Bug filed under #1020 (fake.go), #1020's only child. It
	// must not appear anywhere in the lanes' columns, and shouldn't even
	// earn #1020 a lane of its own...
	for _, l := range a.dashLanes.ls {
		if l.parent.ID == 1020 {
			t.Error("a PBI whose only child is a bug should still get no lane")
		}
		for _, col := range l.cols {
			for _, it := range col {
				if it.ID == 1024 {
					t.Fatalf("bug #1024 should not appear in the lanes, found in #%d's lane", l.parent.ID)
				}
			}
		}
	}
	for _, s := range a.dashLanes.states {
		if s == "New" { // #1024's state; only meaningful if it leaked a column
			t.Error("lanes should not have grown a column for a bug's state")
		}
	}
	// ...but it's still reachable as a child for the preview pane / drill-down.
	found := false
	for _, c := range a.dashChildren[1020] {
		found = found || c.ID == 1024
	}
	if !found {
		t.Error("bug #1024 should still be cached as #1020's child for the preview pane")
	}
}

func TestDashboardLanesExcludeInactivePBIs(t *testing.T) {
	h := newHarness(t, 160, 45)
	a := h.app
	h.keys("1")

	// #1025 is a PBI assigned to Sveinung sitting in "Approved" (fake.go)
	// with a real Task child, #1026 (not a bug, unlike TestDashboardLanesExcludeBugs's
	// #1024) — proving lanes are gated on the parent's own state, not just
	// on whether it has non-bug children.
	found := false
	for _, col := range a.dashBoard.cols {
		for _, it := range col {
			found = found || it.ID == 1025
		}
	}
	if !found {
		t.Error("kanban should still show #1025 (all my PBIs show there, regardless of state)")
	}
	for _, l := range a.dashLanes.ls {
		if l.parent.ID == 1025 {
			t.Error("a PBI that isn't in progress should get no lane, even with a real task child")
		}
		for _, col := range l.cols {
			for _, it := range col {
				if it.ID == 1026 {
					t.Fatalf("task #1026 should not appear in the lanes while its parent #1025 isn't in progress")
				}
			}
		}
	}
}

func TestDashboardFiltersBySelectedSprint(t *testing.T) {
	h := newHarness(t, 160, 45)
	a := h.app
	h.keys("1")

	// All of Sveinung's PBIs (1003, 1013, 1020, 1025) sit in the current
	// sprint, which is selected by default.
	if pbis := len(a.myPBIs()); pbis == 0 {
		t.Fatal("expected PBIs in the current sprint before switching")
	}

	// Switch to a sprint none of "my PBIs" belong to.
	h.keys(":")
	h.keys("s", "p", "r", "i", "n", "t", " ", "S", "p", "r", "i", "n", "t", " ", "4", "3", "enter")
	if a.ctx.Iteration.Name != "Sprint 43" {
		t.Fatalf("iteration = %q, want Sprint 43", a.ctx.Iteration.Name)
	}
	if pbis := len(a.myPBIs()); pbis != 0 {
		t.Errorf("myPBIs after switching sprint = %d, want 0", pbis)
	}
	for _, col := range a.dashBoard.cols {
		if len(col) != 0 {
			t.Errorf("dashboard kanban should be empty outside the selected sprint, got %+v", col)
		}
	}
	if len(a.dashLanes.ls) != 0 {
		t.Errorf("dashboard lanes should be empty outside the selected sprint, got %+v", a.dashLanes.ls)
	}

	// Switching back to the current sprint restores them.
	h.keys("S")
	if pbis := len(a.myPBIs()); pbis == 0 {
		t.Error("myPBIs should be restored after switching back to the current sprint")
	}
	found := false
	for _, col := range a.dashBoard.cols {
		for _, it := range col {
			found = found || it.ID == 1003
		}
	}
	if !found {
		t.Error("#1003 should be back on the kanban after returning to its sprint")
	}
}

// Down/Up must scan through a column's stacked cards before moving to
// another lane — #1013's "To Do" cell has two cards (1015, 1017).
func TestDashboardLaneNavigationScansColumn(t *testing.T) {
	h := newHarness(t, 160, 45)
	a := h.app
	h.keys("1")
	h.keys("tab") // focus lanes
	a.dashLanes.jumpTo(1015)

	h.keys("j") // down: same lane, same column, next row
	if it := a.currentItem(); it == nil || it.ID != 1017 {
		t.Fatalf("down should move to the next card in the cell, got %v", it)
	}
	if !a.dashFocusLanes || a.view != viewDash {
		t.Fatal("navigating within a cell must not change focus or leave the dashboard")
	}

	h.keys("j") // down again: no more cards below in this column anywhere
	if it := a.currentItem(); it == nil || it.ID != 1017 {
		t.Fatalf("down at the column's last card should stay put, got %v", it)
	}

	h.keys("k") // up: back to the first card
	if it := a.currentItem(); it == nil || it.ID != 1015 {
		t.Fatalf("up should move to the previous card in the cell, got %v", it)
	}
	h.keys("k") // up again: no more cards above in this column anywhere
	if it := a.currentItem(); it == nil || it.ID != 1015 {
		t.Fatalf("up at the column's first card should stay put, got %v", it)
	}
}

func TestDashboardLaneColumnMove(t *testing.T) {
	h := newHarness(t, 160, 45)
	a := h.app
	h.keys("1")
	h.keys("tab")            // focus lanes
	a.dashLanes.jumpTo(1015) // Task under #1013, To Do

	h.keys("L") // move right: To Do -> In Progress
	if len(h.fake.Updates) != 1 || h.fake.Updates[0].ID != 1015 || h.fake.Updates[0].Patches[0].Value != "In Progress" {
		t.Fatalf("lane column move updates = %+v", h.fake.Updates)
	}
	if it := a.currentItem(); it == nil || it.ID != 1015 || it.State != "In Progress" {
		t.Fatalf("cursor should follow the moved card, got %v", it)
	}
	col := -1
	for i, s := range a.dashLanes.states {
		if s == "In Progress" {
			col = i
		}
	}
	if col < 0 {
		t.Fatal("In Progress column missing")
	}
	var l1013 lane
	for _, l := range a.dashLanes.ls {
		if l.parent.ID == 1013 {
			l1013 = l
		}
	}
	found := false
	for _, it := range l1013.cols[col] {
		found = found || it.ID == 1015
	}
	if !found {
		t.Error("#1015 should now sit in #1013's lane, In Progress column")
	}

	h.keys("H") // move back
	if len(h.fake.Updates) != 2 || h.fake.Updates[1].Patches[0].Value != "To Do" {
		t.Fatalf("lane column move back = %+v", h.fake.Updates)
	}
}

// A very small terminal must not panic while laying out the dashboard's
// two rows (kanban + lanes), only degrade gracefully.
func TestDashboardRendersAtSmallSize(t *testing.T) {
	h := newHarness(t, 80, 16)
	h.keys("1")
	v := h.app.View()
	if v == "" {
		t.Fatal("empty view at small size")
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
	for _, col := range a.dashBoard.cols {
		for _, it := range col {
			if it.AreaPath != "Platform\\Green" {
				t.Errorf("dashboard card %d outside the filter", it.ID)
			}
		}
	}
	for _, l := range a.dashLanes.ls {
		if l.parent.AreaPath != "Platform\\Green" {
			t.Errorf("dashboard lane parent %d outside the filter", l.parent.ID)
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

// ctrl+u/ctrl+d must scroll the side preview pane in place, without moving
// the underlying board/list selection or resetting the scroll position via
// refreshDetail's GotoTop.
func TestPreviewScroll(t *testing.T) {
	h := newHarness(t, 160, 18) // short enough that #1003's long description overflows the pane
	h.keys("3")                 // board view
	h.app.board.jumpTo(1003)
	h.app.refreshDetail()
	if h.app.detail.YOffset != 0 {
		t.Fatalf("initial YOffset = %d, want 0", h.app.detail.YOffset)
	}
	if !h.app.detail.AtTop() || h.app.detail.AtBottom() {
		t.Fatal("test setup: description should overflow the preview pane")
	}

	h.keys("ctrl+d")
	if h.app.detail.YOffset == 0 {
		t.Fatal("ctrl+d should scroll the preview pane down")
	}
	if got := h.app.board.currentID(); got != 1003 {
		t.Fatalf("board cursor moved to %d, want it to stay on 1003", got)
	}

	h.keys("ctrl+u")
	if h.app.detail.YOffset != 0 {
		t.Fatalf("ctrl+u should scroll back to the top, YOffset = %d", h.app.detail.YOffset)
	}

	// Same behavior with the preview beside the Dashboard's kanban.
	h.keys("1")
	h.app.dashBoard.jumpTo(1003)
	h.app.refreshDetail()
	h.keys("ctrl+d")
	if h.app.detail.YOffset == 0 {
		t.Fatal("ctrl+d should scroll the dashboard's preview pane down")
	}
	if got := h.app.dashBoard.currentID(); got != 1003 {
		t.Fatalf("dashboard kanban cursor moved to %d, want it to stay on 1003", got)
	}

	// Same behavior with the lanes focused, not just the kanban — and it
	// must not turn the preview itself into a focusable pane.
	h.keys("tab") // kanban -> lanes
	h.app.dashLanes.jumpTo(1015)
	h.app.refreshDetail()
	if h.app.detail.YOffset != 0 {
		t.Fatalf("initial lane preview YOffset = %d, want 0", h.app.detail.YOffset)
	}
	h.keys("ctrl+d")
	if h.app.detail.YOffset == 0 {
		t.Fatal("ctrl+d should scroll the preview while the lanes have focus")
	}
	if got := h.app.dashLanes.currentID(); got != 1015 {
		t.Fatalf("lanes cursor moved to %d, want it to stay on 1015", got)
	}
	if h.app.focusDetail {
		t.Fatal("scrolling the preview must not focus it")
	}
}

// With the preview pane hidden, ctrl+u/ctrl+d fall back to their old
// meaning in list views: paging the cursor by half a screen.
func TestPreviewScrollFallsBackToPagingWhenHidden(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.keys("z") // hide the list's preview pane
	if h.app.detailWidth() != 0 {
		t.Fatal("test setup: preview pane should be hidden")
	}
	before := h.app.sprint.currentID()
	h.keys("ctrl+d")
	if h.app.sprint.currentID() == before {
		t.Fatal("ctrl+d should page the list cursor when there is no preview to scroll")
	}
}
