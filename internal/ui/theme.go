package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/sveinungoverland/devopstui/internal/model"
)

var (
	cAccent  = lipgloss.AdaptiveColor{Light: "#005f87", Dark: "#5fafff"}
	cMuted   = lipgloss.AdaptiveColor{Light: "#8a8a8a", Dark: "#6c6c6c"}
	cText    = lipgloss.AdaptiveColor{Light: "#303030", Dark: "#d0d0d0"}
	cBorder  = lipgloss.AdaptiveColor{Light: "#c6c6c6", Dark: "#444444"}
	cFocus   = cAccent
	cCursor  = lipgloss.AdaptiveColor{Light: "#cfe3ff", Dark: "#264f78"}
	cSelect  = lipgloss.AdaptiveColor{Light: "#af5f00", Dark: "#ffaf00"}
	cErr     = lipgloss.AdaptiveColor{Light: "#af0000", Dark: "#ff5f5f"}
	cOK      = lipgloss.AdaptiveColor{Light: "#008700", Dark: "#5fd75f"}
	cEpic    = lipgloss.AdaptiveColor{Light: "#8700af", Dark: "#d787ff"}
	cFeature = lipgloss.AdaptiveColor{Light: "#d75f00", Dark: "#ffaf5f"}
	cPBI     = lipgloss.AdaptiveColor{Light: "#0057b8", Dark: "#5fafff"}
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

// cursorMark is the left-edge marker of the highlighted row.
const cursorMark = "▌"

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

// isActiveState reports whether state means "currently being worked on",
// across the process templates' differing names for that stage (Scrum's
// task-level "In Progress" vs. its requirement-level "Committed", etc).
func isActiveState(state string) bool {
	switch state {
	case "In Progress", "Active", "Committed", "Doing":
		return true
	default:
		return false
	}
}

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
