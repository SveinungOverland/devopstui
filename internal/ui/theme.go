package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/sveinungoverland/devopstui/internal/model"
)

var (
	cAccent  = lipgloss.AdaptiveColor{Light: "#005f87", Dark: "#5fafff"}
	cMuted   = lipgloss.AdaptiveColor{Light: "#6c6c6c", Dark: "#8a8a8a"} // readable on the cursor row too
	cText    = lipgloss.AdaptiveColor{Light: "#303030", Dark: "#d0d0d0"}
	cBorder  = lipgloss.AdaptiveColor{Light: "#c6c6c6", Dark: "#444444"}
	cFocus   = cAccent
	cCursor  = lipgloss.AdaptiveColor{Light: "#cfe3ff", Dark: "#264f78"}
	cSelect  = lipgloss.AdaptiveColor{Light: "#af5f00", Dark: "#ffaf00"}
	cErr     = lipgloss.AdaptiveColor{Light: "#af0000", Dark: "#ff5f5f"}
	cOK      = lipgloss.AdaptiveColor{Light: "#008700", Dark: "#5fd75f"}
	cEpic    = lipgloss.AdaptiveColor{Light: "#8700af", Dark: "#d787ff"}
	cFeature = lipgloss.AdaptiveColor{Light: "#d75f00", Dark: "#ffaf5f"}
	cPBI     = lipgloss.AdaptiveColor{Light: "#008787", Dark: "#5fd7d7"} // not cAccent: PBI tags would read as keys/focus
	cTask    = lipgloss.AdaptiveColor{Light: "#5f5f5f", Dark: "#9e9e9e"}
	cBug     = lipgloss.AdaptiveColor{Light: "#d70000", Dark: "#ff5f5f"}

	sHeader     = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	sCrumb      = lipgloss.NewStyle().Foreground(cText)
	sCrumbSep   = lipgloss.NewStyle().Foreground(cMuted)
	sMuted      = lipgloss.NewStyle().Foreground(cMuted)
	sText       = lipgloss.NewStyle().Foreground(cText)
	sKey        = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sErr        = lipgloss.NewStyle().Foreground(cErr).Bold(true)
	sOK         = lipgloss.NewStyle().Foreground(cOK)
	sCursor     = lipgloss.NewStyle().Background(cCursor)
	sSelected   = lipgloss.NewStyle().Foreground(cSelect).Bold(true)
	sTabActive  = lipgloss.NewStyle().Foreground(cAccent).Bold(true).Underline(true)
	sTab        = lipgloss.NewStyle().Foreground(cMuted)
	sPanel      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBorder)
	sPanelFocus = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cFocus)
	sPopup      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).Padding(0, 1)
	sTitle      = lipgloss.NewStyle().Bold(true)
	sLabel      = lipgloss.NewStyle().Foreground(cMuted).Width(11)
	sExternal   = lipgloss.NewStyle().Foreground(cMuted).Italic(true)
	sBadge      = lipgloss.NewStyle().Bold(true)
)

// rowStyler returns a function that adds the cursor background to any style
// when hl is set. Styled segments each carry their own ANSI reset, so a
// background must be applied per segment rather than around the whole
// line, or it vanishes after the first coloured piece.
func rowStyler(hl bool) func(lipgloss.Style) lipgloss.Style {
	if !hl {
		return func(s lipgloss.Style) lipgloss.Style { return s }
	}
	return func(s lipgloss.Style) lipgloss.Style { return s.Background(cCursor) }
}

// fill renders n spaces in the given style (for padding a highlighted row).
func fill(st lipgloss.Style, n int) string {
	if n <= 0 {
		return ""
	}
	return st.Render(strings.Repeat(" ", n))
}

// spread lays out a card's first line: left flush left, the right pieces
// flush right, separated by at least one space. When that doesn't fit in w,
// right pieces are dropped from the front (lowest priority first) rather
// than letting them run into left — "PBI 1005" and an effort of 3 must
// never read as "PBI 10053".
func spread(plain lipgloss.Style, left string, w int, right ...string) string {
	for len(right) > 0 {
		r := strings.Join(right, plain.Render(" "))
		if gap := w - lipgloss.Width(left) - lipgloss.Width(r); gap >= 1 {
			return left + fill(plain, gap) + r
		}
		right = right[1:]
	}
	return left + fill(plain, w-lipgloss.Width(left))
}

// cursorMark is the left-edge marker of the highlighted row.
const cursorMark = "▌"

// selfMark flags the item drilled into among its siblings.
const selfMark = "◆"

func kindStyle(k model.Kind) lipgloss.Style {
	switch k {
	case model.KindEpic:
		return sBadge.Foreground(cEpic)
	case model.KindFeature:
		return sBadge.Foreground(cFeature)
	case model.KindRequirement:
		return sBadge.Foreground(cPBI)
	case model.KindBug:
		return sBadge.Foreground(cBug)
	default:
		return sBadge.Foreground(cTask)
	}
}

func isActiveState(state string) bool { return model.IsActive(state) }

func stateStyle(state string) lipgloss.Style {
	switch state {
	case "Done", "Closed", "Resolved", "Completed":
		return lipgloss.NewStyle().Foreground(cOK)
	case "Removed":
		return lipgloss.NewStyle().Foreground(cMuted).Strikethrough(true)
	default:
		if isActiveState(state) {
			return lipgloss.NewStyle().Foreground(cAccent)
		}
		return lipgloss.NewStyle().Foreground(cText)
	}
}

// health is the App's staleness setting, shared by pointer with every view
// so :stale reaches them all. A nil *health uses the default.
type health struct {
	staleAfter time.Duration
}

func (h *health) rules() model.HealthRules {
	r := model.HealthRules{Now: time.Now(), StaleAfter: defaultStaleAfter}
	if h != nil {
		r.StaleAfter = h.staleAfter
	}
	return r
}

const defaultStaleAfter = 5 * 24 * time.Hour

// assess is model.Assess with the task tally taken from a progress badge.
func (h *health) assess(it *model.WorkItem, p progress) model.Health {
	return model.Assess(it, model.TaskTally{Done: p.done, Total: p.total, Latest: p.latest}, h.rules())
}

// assessAll assesses every item, keeping only the flagged ones.
func (h *health) assessAll(items []*model.WorkItem, prog map[int]progress) map[int]model.Health {
	out := map[int]model.Health{}
	for _, it := range items {
		if hh := h.assess(it, prog[it.ID]); hh.Signals != 0 {
			out[it.ID] = hh
		}
	}
	return out
}

// signalGlyphs are one cell wide each and never the only cue: every one
// is a distinct shape, so they read without colour too.
var signalGlyphs = map[model.Signal]string{
	model.SignalOpenTasks:    "✗",
	model.SignalReadyToClose: "✓",
	model.SignalStale:        "◷",
	model.SignalUnassigned:   "?",
	model.SignalOrphan:       "↑",
}

func signalStyle(s model.Signal) lipgloss.Style {
	switch s {
	case model.SignalOpenTasks:
		return lipgloss.NewStyle().Foreground(cErr).Bold(true)
	case model.SignalReadyToClose:
		return lipgloss.NewStyle().Foreground(cOK).Bold(true)
	case model.SignalUnassigned:
		return lipgloss.NewStyle().Foreground(cSelect).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(cMuted)
	}
}

// glyphStyle is signalStyle, except that staleness fades in: muted at
// first, amber once the item has sat for twice the threshold.
func (h *health) glyphStyle(s model.Signal, hh model.Health) lipgloss.Style {
	if s == model.SignalStale {
		if after := h.rules().StaleAfter; after > 0 && hh.Idle >= 2*after {
			return lipgloss.NewStyle().Foreground(cSelect)
		}
	}
	return signalStyle(s)
}

// glyph renders the item's top signal in one cell, or a space. st adds the
// cursor background.
func (h *health) glyph(hh model.Health, st func(lipgloss.Style) lipgloss.Style) string {
	top := hh.Signals.Top()
	if top == 0 {
		return st(lipgloss.NewStyle()).Render(" ")
	}
	return st(h.glyphStyle(top, hh)).Render(signalGlyphs[top])
}

// flags renders every signal as glyph plus description, for the places
// with room for all of them.
func (h *health) flags(hh model.Health) []string {
	var out []string
	desc := hh.Describe()
	for i, s := range hh.Signals.List() {
		out = append(out, h.glyphStyle(s, hh).Render(signalGlyphs[s])+" "+desc[i])
	}
	return out
}
