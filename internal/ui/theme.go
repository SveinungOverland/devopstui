package ui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/sveinungoverland/devopstui/internal/model"
)

var (
	cAccent  = lipgloss.AdaptiveColor{Light: "#005f87", Dark: "#5fafff"}
	cMuted   = lipgloss.AdaptiveColor{Light: "#8a8a8a", Dark: "#6c6c6c"}
	cText    = lipgloss.AdaptiveColor{Light: "#303030", Dark: "#d0d0d0"}
	cBorder  = lipgloss.AdaptiveColor{Light: "#c6c6c6", Dark: "#444444"}
	cFocus   = cAccent
	cCursor  = lipgloss.AdaptiveColor{Light: "#e4e4e4", Dark: "#303030"}
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

func stateStyle(state string) lipgloss.Style {
	switch state {
	case "Done", "Closed", "Resolved", "Completed":
		return lipgloss.NewStyle().Foreground(cOK)
	case "In Progress", "Active", "Committed", "Doing":
		return lipgloss.NewStyle().Foreground(cAccent)
	case "Removed":
		return lipgloss.NewStyle().Foreground(cMuted).Strikethrough(true)
	default:
		return lipgloss.NewStyle().Foreground(cText)
	}
}
