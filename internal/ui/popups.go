package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// picker is a fuzzy-filterable list; typing filters, j/k or arrows move,
// enter picks, esc cancels.
type picker struct {
	title  string
	items  []pickItem
	shown  []int
	cursor int
	input  textinput.Model
	onPick func(pickItem) tea.Cmd
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
	return p, cmd
}

func (p *picker) View(w, h int) string {
	width := min(max(w-10, 30), 80)
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
		b.WriteString(sMuted.Render("  no matches") + "\n")
	}
	b.WriteString(sMuted.Render(fmt.Sprintf("%d/%d  enter select · esc cancel", len(p.shown), len(p.items))))
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
			lines = append(lines, padLeft(sKey.Render(kb.Help().Key), 8)+"  "+kb.Help().Desc)
		}
		cols = append(cols, strings.Join(lines, "\n"))
	}
	// Two rows of three columns keeps it readable on 100+ cols.
	row1 := lipgloss.JoinHorizontal(lipgloss.Top, padCol(cols[0]), padCol(cols[1]), padCol(cols[2]))
	row2 := lipgloss.JoinHorizontal(lipgloss.Top, padCol(cols[3]), padCol(cols[4]), padCol(cols[5]))
	body := sTitle.Render("Keys") + "\n\n" + row1 + "\n\n" + row2 + "\n\n" + sMuted.Render("any key to close")
	return sPopup.Render(body)
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
	in.SetSuggestions([]string{"sprint", "team", "filter", "project", "board", "backlog", "dash", "refresh", "quit"})
	in.ShowSuggestions = true
	return cmdbar{input: in}
}
