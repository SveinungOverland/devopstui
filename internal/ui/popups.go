package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sveinungoverland/devopstui/internal/model"
)

// popup is a modal layer that receives keys before the active view.
type popup interface {
	Update(tea.Msg) (popup, tea.Cmd)
	View(w, h int) string
}

// ---------------------------------------------------------------- picker

type pickItem struct {
	Label string
	Desc  string
	Value any
}

// pickerItemsMsg carries results from a picker's server-side search. It
// is applied only if the query still matches what is typed.
type pickerItemsMsg struct {
	token int
	query string
	items []pickItem
	err   error
}

var pickerToken int

// picker is a fuzzy-filterable list; typing filters, j/k or arrows move,
// enter picks, esc cancels. With onSearch set it also queries the server
// as you type and merges the results in.
type picker struct {
	title  string
	items  []pickItem
	shown  []int
	cursor int
	input  textinput.Model
	onPick func(pickItem) tea.Cmd

	// onSearch, when set, is called with the typed query; its results
	// replace the list. token guards against out-of-order responses.
	onSearch  func(token int, query string) tea.Cmd
	token     int
	searching bool
	sentQuery string
	searchErr string
	// keep is the head of the list that survives a search, so a fixed
	// entry such as "Unassigned" stays reachable.
	keep int
}

func newPicker(title string, items []pickItem, onPick func(pickItem) tea.Cmd) *picker {
	in := textinput.New()
	in.Prompt = "/ "
	in.Placeholder = "type to filter"
	in.Focus()
	p := &picker{title: title, items: items, input: in, onPick: onPick}
	p.refilter()
	return p
}

// searchable turns on server-side search. keep is how many leading items
// are fixed entries that every result set keeps.
func (p *picker) searchable(keep int, onSearch func(token int, query string) tea.Cmd) *picker {
	pickerToken++
	p.token = pickerToken
	p.keep = keep
	p.onSearch = onSearch
	p.input.Placeholder = "type to search"
	return p
}

// search fires a query when the typed text has changed.
func (p *picker) search() tea.Cmd {
	q := strings.TrimSpace(p.input.Value())
	if p.onSearch == nil || q == p.sentQuery {
		return nil
	}
	p.sentQuery = q
	p.searching = true
	p.searchErr = ""
	return p.onSearch(p.token, q)
}

// applyResults swaps in search results, keeping the fixed head entries.
func (p *picker) applyResults(msg pickerItemsMsg) {
	if msg.token != p.token || msg.query != strings.TrimSpace(p.input.Value()) {
		return // stale: a newer query is already in flight
	}
	p.searching = false
	if msg.err != nil {
		p.searchErr = msg.err.Error()
		return
	}
	head := p.items
	if p.keep < len(head) {
		head = head[:p.keep]
	}
	p.items = append(append([]pickItem(nil), head...), msg.items...)
	p.cursor = 0
	p.refilter()
}

func (p *picker) refilter() {
	q := strings.ToLower(p.input.Value())
	p.shown = p.shown[:0]
	for i, it := range p.items {
		if q == "" || fuzzy(strings.ToLower(it.Label+" "+it.Desc), q) {
			p.shown = append(p.shown, i)
		}
	}
	if p.cursor >= len(p.shown) {
		p.cursor = max(len(p.shown)-1, 0)
	}
}

// fuzzy reports whether every space-separated word of q is a substring of s.
func fuzzy(s, q string) bool {
	for _, w := range strings.Fields(q) {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}

func (p *picker) Update(msg tea.Msg) (popup, tea.Cmd) {
	if res, ok := msg.(pickerItemsMsg); ok {
		p.applyResults(res)
		return p, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		return p, cmd
	}
	switch km.String() {
	case "esc":
		return nil, nil
	case "enter":
		if len(p.shown) == 0 {
			return nil, nil
		}
		it := p.items[p.shown[p.cursor]]
		return nil, p.onPick(it)
	case "down", "ctrl+n", "ctrl+j":
		if p.cursor < len(p.shown)-1 {
			p.cursor++
		}
		return p, nil
	case "up", "ctrl+p", "ctrl+k":
		if p.cursor > 0 {
			p.cursor--
		}
		return p, nil
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.refilter()
	return p, tea.Batch(cmd, p.search())
}

func (p *picker) View(w, h int) string {
	// As wide as the longest entry needs (a state list doesn't need 80
	// columns), but never narrower than the title and status line.
	content := max(lipgloss.Width(p.title), 36)
	for _, it := range p.items {
		wd := lipgloss.Width(it.Label)
		if it.Desc != "" {
			wd += 2 + lipgloss.Width(it.Desc)
		}
		content = max(content, wd)
	}
	width := min(max(w-10, 30), 80, content+6)
	maxRows := min(max(h-8, 5), 20)
	var b strings.Builder
	b.WriteString(sTitle.Render(p.title) + "\n")
	b.WriteString(p.input.View() + "\n")
	start := 0
	if p.cursor >= maxRows {
		start = p.cursor - maxRows + 1
	}
	for i := start; i < len(p.shown) && i < start+maxRows; i++ {
		it := p.items[p.shown[i]]
		cur := i == p.cursor
		st := rowStyler(cur)
		plain := st(lipgloss.NewStyle())
		lineW := width - 4
		label := trunc(it.Label, lineW)
		line := plain.Render(label)
		if it.Desc != "" {
			line += st(sMuted).Render(trunc("  "+it.Desc, lineW-lipgloss.Width(label)))
		}
		line += fill(plain, lineW-lipgloss.Width(line))
		if cur {
			line = st(sKey).Render(cursorMark+" ") + line
		} else {
			line = "  " + line
		}
		b.WriteString(line + "\n")
	}
	if len(p.shown) == 0 {
		msg := "  no matches"
		if p.searching {
			msg = "  searching…"
		}
		b.WriteString(sMuted.Render(msg) + "\n")
	}
	status := fmt.Sprintf("%d/%d  enter select · esc cancel", len(p.shown), len(p.items))
	switch {
	case p.searchErr != "":
		status = trunc(p.searchErr, width-4)
		b.WriteString(sErr.Render(status))
	case p.searching:
		b.WriteString(sMuted.Render("searching… · esc cancel"))
	default:
		b.WriteString(sMuted.Render(status))
	}
	return sPopup.Width(width).Render(b.String())
}

// ---------------------------------------------------------------- confirm

type confirm struct {
	title string
	body  string
	onYes func() tea.Cmd
}

func newConfirm(title, body string, onYes func() tea.Cmd) *confirm {
	return &confirm{title: title, body: body, onYes: onYes}
}

func (c *confirm) Update(msg tea.Msg) (popup, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "y", "Y", "enter":
			return nil, c.onYes()
		case "n", "N", "esc", "q":
			return nil, nil
		}
	}
	return c, nil
}

func (c *confirm) View(w, h int) string {
	width := min(max(w-10, 30), 70)
	body := c.title
	if c.body != "" {
		body += "\n\n" + wrap(c.body, width-2)
	}
	body += "\n\n" + sKey.Render("y") + " yes   " + sKey.Render("n") + " no"
	return sPopup.Width(width).Render(body)
}

// ---------------------------------------------------------------- prompt

// prompt is a one-line text input.
type prompt struct {
	title    string
	input    textinput.Model
	onSubmit func(string) tea.Cmd
}

func newPrompt(title, initial string, onSubmit func(string) tea.Cmd) *prompt {
	in := textinput.New()
	in.Prompt = "> "
	in.SetValue(initial)
	in.CursorEnd()
	in.Focus()
	return &prompt{title: title, input: in, onSubmit: onSubmit}
}

func (p *prompt) Update(msg tea.Msg) (popup, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			return nil, nil
		case "enter":
			return nil, p.onSubmit(strings.TrimSpace(p.input.Value()))
		}
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return p, cmd
}

func (p *prompt) View(w, h int) string {
	width := min(max(w-10, 30), 80)
	p.input.Width = width - 6
	return sPopup.Width(width).Render(sTitle.Render(p.title) + "\n" + p.input.View() + "\n" + sMuted.Render("enter save · esc cancel"))
}

// ---------------------------------------------------------------- help

type helpPopup struct{}

func (helpPopup) Update(msg tea.Msg) (popup, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		return nil, nil
	}
	return helpPopup{}, nil
}

func (helpPopup) View(w, h int) string {
	var cols []string
	for _, g := range helpGroups {
		var lines []string
		for _, kb := range g {
			desc := kb.Help().Desc
			if where, ok := helpWhere[desc]; ok {
				desc += sMuted.Render(" (" + where + ")")
			}
			lines = append(lines, padLeft(sKey.Render(kb.Help().Key), 8)+"  "+desc)
		}
		cols = append(cols, strings.Join(lines, "\n"))
	}
	// Fill each row with as many groups as fit the screen, in order: all
	// six side by side on a wide monitor, three per row around 140
	// columns, fewer on a narrow terminal.
	avail := w - 6 // popup border and padding
	var rows []string
	var row []string
	rowW := 0
	for _, c := range cols {
		c = padCol(c)
		cw := lipgloss.Width(c)
		if len(row) > 0 && rowW+cw > avail {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, row...))
			row, rowW = nil, 0
		}
		row = append(row, c)
		rowW += cw
	}
	rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, row...))
	body := sTitle.Render("Keys") + "\n\n" + strings.Join(rows, "\n\n") + "\n\n" + flagLegend(avail) + "\n\n" + sMuted.Render("any key to close")
	return sPopup.Render(body)
}

// flagLegend explains the health glyphs, packed into as few lines as fit.
func flagLegend(w int) string {
	var entries []string
	for _, s := range model.AllSignals() {
		entries = append(entries, signalStyle(s).Render(signalGlyphs[s])+" "+s.Name()+sMuted.Render(" – "+signalHelp[s]))
	}
	lines := []string{sTitle.Render("Flags") + sMuted.Render("  :attention shows only flagged items, :stale <days> sets the threshold")}
	line := ""
	for _, e := range entries {
		switch {
		case line == "":
			line = e
		case lipgloss.Width(line)+3+lipgloss.Width(e) <= w:
			line += "   " + e
		default:
			lines = append(lines, line)
			line = e
		}
	}
	return strings.Join(append(lines, line), "\n")
}

var signalHelp = map[model.Signal]string{
	model.SignalOpenTasks:    "done, but tasks still open",
	model.SignalReadyToClose: "every task done, item still open",
	model.SignalStale:        "active, no change in a while (amber at twice that)",
	model.SignalMissing:      "active with no assignee, or done with hours left",
	model.SignalOrphan:       "PBI without a parent Feature",
}

// helpWhere marks the bindings that share a key with another, so the ?
// overlay says which view each one belongs to. c comments in the details
// view and hides done items everywhere else.
var helpWhere = map[string]string{
	keys.Comment.Help().Desc: "details",
	keys.Closed.Help().Desc:  "elsewhere",
}

func padCol(s string) string { return lipgloss.NewStyle().MarginRight(3).Render(s) }

// ---------------------------------------------------------------- results

// report shows the outcome of a bulk operation.
type report struct {
	title string
	lines []string
}

func (r *report) Update(msg tea.Msg) (popup, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		return nil, nil
	}
	return r, nil
}

func (r *report) View(w, h int) string {
	width := min(max(w-10, 30), 90)
	body := sTitle.Render(r.title) + "\n\n" + strings.Join(r.lines, "\n") + "\n\n" + sMuted.Render("any key to close")
	return sPopup.Width(width).Render(body)
}

// ---------------------------------------------------------------- command bar

// cmdbar is the `:` line at the bottom of the screen. It is not a popup;
// it replaces the footer while active.
type cmdbar struct {
	input textinput.Model
}

func newCmdbar() cmdbar {
	in := textinput.New()
	in.Prompt = ":"
	in.SetSuggestions([]string{"sprint", "team", "filter", "project", "board", "backlog", "dash", "refresh", "auto", "attention", "stale", "quit"})
	in.ShowSuggestions = true
	return cmdbar{input: in}
}
