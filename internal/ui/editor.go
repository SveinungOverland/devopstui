package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sveinungoverland/devopstui/internal/markdown"
	"github.com/sveinungoverland/devopstui/internal/model"
)

// editorDoneMsg carries text back from an external editor.
type editorDoneMsg struct {
	text    string
	noun    string // "description" or "comment", for the "X unchanged" flash
	changed bool
	apply   func(string) tea.Cmd
	err     error
}

// editDescription opens the item's description (Markdown) for editing.
// apply receives the new text when it differs from the original.
func (a *App) editDescription(it *model.WorkItem, apply func(string) tea.Cmd) tea.Cmd {
	title := fmt.Sprintf("Description of #%d %s", it.ID, trunc(it.Title, 40))
	header := fmt.Sprintf("<!-- #%d %s — save and quit to apply, leave unchanged to cancel -->\n\n", it.ID, it.Title)
	return a.editMarkdown(it.ID, "description", title, it.Description, header, apply)
}

// editComment opens an empty Markdown composer for a new comment on it.
// apply receives the text when the composer is submitted non-empty.
func (a *App) editComment(it *model.WorkItem, apply func(string) tea.Cmd) tea.Cmd {
	title := fmt.Sprintf("Comment on #%d %s", it.ID, trunc(it.Title, 40))
	header := fmt.Sprintf("<!-- comment on #%d %s — save and quit to post, leave empty to cancel -->\n\n", it.ID, it.Title)
	return a.editMarkdown(it.ID, "comment", title, "", header, apply)
}

// editMarkdown opens a Markdown composer for item-scoped text — the
// description, or (with an empty initial) a new comment. With an external
// editor configured the program is suspended while the editor runs on a temp
// file; otherwise the built-in editor popup opens. apply receives the new
// text when it differs from initial. noun names the unsaved text for the
// built-in editor's flashes ("description unchanged", "comment unchanged").
func (a *App) editMarkdown(id int, noun, title, initial, header string, apply func(string) tea.Cmd) tea.Cmd {
	editor := a.cfg.EditorCommand()
	if editor == "" {
		// The editor keeps its own baseline (ctrl+w saves without closing),
		// so it decides what counts as a change.
		a.popup = newMDEditor(title, initial, noun, apply)
		return nil
	}

	dir := os.TempDir()
	path := filepath.Join(dir, fmt.Sprintf("devopstui-%d.md", id))
	if err := os.WriteFile(path, []byte(header+initial+"\n"), 0o600); err != nil {
		return a.setFlash("temp file: "+err.Error(), true)
	}
	parts := strings.Fields(editor)
	c := exec.Command(parts[0], append(parts[1:], path)...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		defer os.Remove(path)
		if err != nil {
			return editorDoneMsg{err: err}
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return editorDoneMsg{err: rerr}
		}
		text := strings.TrimSpace(strings.TrimPrefix(string(b), header))
		text = stripHeaderComment(text)
		return editorDoneMsg{text: text, noun: noun, changed: text != strings.TrimSpace(initial), apply: apply}
	})
}

// stripHeaderComment removes the guidance comment even if the user edited
// around it.
func stripHeaderComment(s string) string {
	if strings.HasPrefix(s, "<!--") {
		if i := strings.Index(s, "-->"); i >= 0 {
			return strings.TrimSpace(s[i+3:])
		}
	}
	return s
}

// ---------------------------------------------------------------- built-in editor

// mdEditor is the in-app fallback: a modal (vim-like) textarea with a live
// Markdown preview beside it.
//
// Normal mode: j/k/h/l move, g/G top/bottom, space toggles a checkbox on
// the current line, dd/yy cut/copy the line and p/P paste it, i/a/A/o/O
// enter insert mode, ctrl+w saves and keeps editing, ctrl+s saves and
// closes, q closes.
// Insert mode: type; esc returns to normal mode.
type mdEditor struct {
	title string
	// noun names the text being edited, for the "X unchanged" flash — either
	// "description" or "comment".
	noun     string
	area     textarea.Model
	preview  viewport.Model
	showPrev bool
	insert   bool
	// initial is the baseline the buffer is compared against: the text as
	// opened, and after a ctrl+w write the text that was saved.
	initial  string
	inflight string // text of a ctrl+w write still in flight, "" when none
	saved    bool   // a write landed and the buffer still matches it
	quitArm  bool   // q pressed once with unsaved changes
	pending  string // first key of a two-key command ("d" or "y")
	register string // last dd/yy line, pasted with p/P
	hasReg   bool   // register holds a line (which may be empty)
	onSubmit func(string) tea.Cmd
	lastSrc  string
}

func newMDEditor(title, initial, noun string, onSubmit func(string) tea.Cmd) *mdEditor {
	ta := textarea.New()
	ta.SetValue(initial)
	ta.ShowLineNumbers = true
	ta.Prompt = "│ "
	ta.CharLimit = 0
	ta.MaxHeight = 0
	ta.Placeholder = "Markdown… press i to type"
	ta.Focus()
	e := &mdEditor{title: title, noun: noun, area: ta, showPrev: true, initial: initial, onSubmit: onSubmit, preview: viewport.New(40, 10)}
	e.gotoTop()
	return e
}

// gotoTop moves to the first row. SetValue leaves the cursor at the end and
// CursorUp moves by wrapped row, not logical line.
func (e *mdEditor) gotoTop() {
	for i := 0; i < 100000 && (e.area.Line() > 0 || e.area.LineInfo().RowOffset > 0); i++ {
		e.area.CursorUp()
	}
	e.area.CursorStart()
}

// lineDown/lineUp move by logical line like vim's j/k, stepping over any
// wrapped rows of a long line.
func (e *mdEditor) lineDown() {
	target := e.area.Line() + 1
	if target >= e.area.LineCount() {
		return
	}
	for i := 0; i < 1000 && e.area.Line() < target; i++ {
		e.area.CursorDown()
	}
}

func (e *mdEditor) lineUp() {
	target := e.area.Line() - 1
	if target < 0 {
		return
	}
	for i := 0; i < 1000 && e.area.Line() > target; i++ {
		e.area.CursorUp()
	}
}

func (e *mdEditor) gotoLine(n int) {
	e.gotoTop()
	for i := 0; i < 100000 && e.area.Line() < n; i++ {
		e.area.CursorDown()
	}
	e.area.CursorStart()
}

var checkboxRe = regexp.MustCompile(`^(\s*(?:[-*+]|\d+[.)])\s+)\[( |x|X)\](\s?)(.*)$`)
var listItemRe = regexp.MustCompile(`^(\s*(?:[-*+]|\d+[.)])\s+)(.*)$`)

// toggleCheckbox flips "- [ ]" to "- [x]" on the current line. A plain list
// item gains a checkbox; other lines are left alone.
func (e *mdEditor) toggleCheckbox() {
	lines := strings.Split(e.area.Value(), "\n")
	n := e.area.Line()
	if n >= len(lines) {
		return
	}
	line := lines[n]
	switch {
	case checkboxRe.MatchString(line):
		m := checkboxRe.FindStringSubmatch(line)
		box := "[x]"
		if m[2] != " " {
			box = "[ ]"
		}
		lines[n] = m[1] + box + m[3] + m[4]
	case listItemRe.MatchString(line):
		m := listItemRe.FindStringSubmatch(line)
		lines[n] = m[1] + "[ ] " + m[2]
	default:
		return
	}
	e.area.SetValue(strings.Join(lines, "\n"))
	e.gotoLine(n)
}

// lines returns the buffer split into lines and the current line index.
func (e *mdEditor) lines() ([]string, int) {
	return strings.Split(e.area.Value(), "\n"), e.area.Line()
}

func (e *mdEditor) setLines(lines []string, cursor int) {
	e.area.SetValue(strings.Join(lines, "\n"))
	e.gotoLine(min(max(cursor, 0), max(len(lines)-1, 0)))
}

// deleteLine is dd: cut the current line into the register.
func (e *mdEditor) deleteLine() {
	lines, n := e.lines()
	if n >= len(lines) {
		return
	}
	e.register, e.hasReg = lines[n], true
	if len(lines) == 1 {
		e.setLines([]string{""}, 0)
		return
	}
	e.setLines(append(lines[:n:n], lines[n+1:]...), n)
}

// yankLine is yy: copy the current line into the register.
func (e *mdEditor) yankLine() {
	lines, n := e.lines()
	if n < len(lines) {
		e.register, e.hasReg = lines[n], true
	}
}

// pasteLine is p (below) / P (above): insert the register as a new line.
func (e *mdEditor) pasteLine(above bool) {
	if !e.hasReg {
		return
	}
	lines, n := e.lines()
	at := n + 1
	if above {
		at = n
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:at]...)
	out = append(out, e.register)
	out = append(out, lines[at:]...)
	e.setLines(out, at)
}

func (e *mdEditor) changed() bool {
	return strings.TrimSpace(e.area.Value()) != strings.TrimSpace(e.initial)
}

// write saves the buffer and keeps the editor open, so a long edit can be
// committed in steps. The baseline only moves once the write lands (see
// saveDone), so a failed save still counts as unsaved changes.
func (e *mdEditor) write() tea.Cmd {
	if !e.changed() {
		return flash(e.noun + " unchanged")
	}
	text := e.area.Value()
	cmd := e.onSubmit(text)
	if cmd == nil {
		// Applied without a round trip — the edit form keeps the new text
		// as a pending value and saves it with the rest of the fields.
		e.markSaved(text)
		return nil
	}
	e.inflight = text
	return cmd
}

// saveDone reports the outcome of the write started by ctrl+w.
func (e *mdEditor) saveDone(err error) {
	text := e.inflight
	e.inflight = ""
	if err == nil && text != "" {
		e.markSaved(text)
	}
}

func (e *mdEditor) markSaved(text string) {
	e.initial = text
	e.saved = true
	e.quitArm = false
}

// flash shows a message in the footer without touching the buffer.
func flash(text string) tea.Cmd {
	return func() tea.Msg { return flashMsg{text: text} }
}

func (e *mdEditor) Update(msg tea.Msg) (popup, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		e.area, cmd = e.area.Update(msg)
		return e, cmd
	}
	// Keys valid in both modes.
	switch km.String() {
	case "ctrl+s":
		if !e.changed() {
			return nil, flash(e.noun + " unchanged")
		}
		return nil, e.onSubmit(e.area.Value())
	case "ctrl+p":
		e.showPrev = !e.showPrev
		return e, nil
	case "ctrl+d":
		e.preview.HalfViewDown()
		return e, nil
	case "ctrl+u":
		e.preview.HalfViewUp()
		return e, nil
	}

	if e.insert {
		if km.String() == "esc" {
			e.insert = false
			return e, nil
		}
		var cmd tea.Cmd
		e.area, cmd = e.area.Update(msg)
		return e, cmd
	}

	// Normal mode.
	k := km.String()
	e.quitArm = e.quitArm && k == "q"
	if e.pending != "" {
		pending := e.pending
		e.pending = ""
		switch pending + k {
		case "dd":
			e.deleteLine()
		case "yy":
			e.yankLine()
		}
		return e, nil // any other second key cancels the command
	}
	switch k {
	// ctrl+w is vim's :w — save and carry on editing. It is normal mode
	// only so the textarea keeps ctrl+w as delete-word while typing.
	case "ctrl+w":
		return e, e.write()
	case "d", "y":
		e.pending = k
	case "p":
		e.pasteLine(false)
	case "P":
		e.pasteLine(true)
	case "j", "down":
		e.lineDown()
	case "k", "up":
		e.lineUp()
	case "h", "left":
		e.area.SetCursor(e.cursorCol() - 1)
	case "l", "right":
		e.area.SetCursor(e.cursorCol() + 1)
	case "0", "home":
		e.area.CursorStart()
	case "$", "end":
		e.area.CursorEnd()
	case "g":
		e.gotoTop()
	case "G":
		e.gotoLine(e.area.LineCount() - 1)
	case " ":
		e.toggleCheckbox()
	case "i":
		e.insert = true
	case "a":
		e.area.SetCursor(e.cursorCol() + 1)
		e.insert = true
	case "A":
		e.area.CursorEnd()
		e.insert = true
	case "o":
		e.area.CursorEnd()
		e.area.InsertString("\n")
		e.insert = true
	case "O":
		e.area.CursorStart()
		e.area.InsertString("\n")
		e.area.CursorUp()
		e.insert = true
	case "q", "esc":
		if km.String() == "esc" && e.changed() {
			return e, nil // esc only leaves insert mode; q closes
		}
		if e.changed() && !e.quitArm {
			e.quitArm = true
			return e, nil
		}
		return nil, nil
	}
	return e, nil
}

func (e *mdEditor) cursorCol() int {
	li := e.area.LineInfo()
	return li.StartColumn + li.ColumnOffset
}

func (e *mdEditor) View(w, h int) string {
	width := min(max(w-6, 50), 160)
	height := min(max(h-8, 8), 40)
	editW := width
	if e.showPrev && width >= 90 {
		editW = width / 2
	} else {
		e.showPrev = e.showPrev && width >= 90
	}
	e.area.SetWidth(editW - 2)
	e.area.SetHeight(height)

	body := e.area.View()
	if e.showPrev {
		prevW := width - editW - 3
		if src := e.area.Value(); src != e.lastSrc || e.preview.Width != prevW {
			e.lastSrc = src
			e.preview.Width = prevW
			e.preview.Height = height
			e.preview.SetContent(markdown.Render(src, prevW-2))
		}
		sepStyle := lipgloss.NewStyle().Foreground(cBorder)
		sep := strings.TrimSuffix(strings.Repeat(sepStyle.Render("│")+"\n", height), "\n")
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, " "+sep+" ", e.preview.View())
	}

	mode := lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(lipgloss.Color("0")).Background(cAccent).Render("NORMAL")
	hints := sKey.Render("i") + sMuted.Render(" insert  ") + sKey.Render("j/k") + sMuted.Render(" move  ") +
		sKey.Render("space") + sMuted.Render(" toggle [ ]  ") + sKey.Render("dd yy p") + sMuted.Render(" lines  ") +
		sKey.Render("ctrl+w") + sMuted.Render(" save  ") + sKey.Render("ctrl+s") + sMuted.Render(" save+close  ") +
		sKey.Render("q") + sMuted.Render(" close")
	if e.insert {
		mode = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(lipgloss.Color("0")).Background(cSelect).Render("INSERT")
		hints = sKey.Render("esc") + sMuted.Render(" normal mode  ") + sKey.Render("ctrl+s") + sMuted.Render(" save+close")
	}
	if e.quitArm {
		hints = sErr.Render("unsaved changes: ") + sKey.Render("q") + sMuted.Render(" again to discard, ") + sKey.Render("ctrl+s") + sMuted.Render(" to save and close")
	}
	if e.pending != "" {
		hints = sKey.Render(e.pending) + sMuted.Render(" …  (") + sKey.Render(e.pending+e.pending) + sMuted.Render(" line)")
	}
	pos := sMuted.Render(fmt.Sprintf("  %d/%d", e.area.Line()+1, e.area.LineCount()))
	modified := ""
	switch {
	case e.inflight != "":
		modified = sMuted.Render(" [saving…]")
	case e.changed():
		modified = sSelected.Render(" [+]")
	case e.saved:
		modified = sOK.Render(" [saved]")
	}
	right := sKey.Render("ctrl+p") + sMuted.Render(" preview")
	status := mode + pos + modified + "  " + hints
	status = pad(status, width-2-lipgloss.Width(right)) + right
	return sPopup.Width(width).Render(sTitle.Render(e.title) + "\n" + body + "\n" + status)
}
