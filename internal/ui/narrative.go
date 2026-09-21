package ui

import (
	"strings"

	"github.com/sveinungoverland/devopstui/internal/markdown"
	"github.com/sveinungoverland/devopstui/internal/model"
)

// narrativeSection is one block of an item's narrative text — Description,
// or for Bugs, Repro Steps and Acceptance Criteria.
type narrativeSection struct {
	heading string
	body    string
}

// narrativeSections returns which of Description, Repro Steps and
// Acceptance Criteria are present on it, in that order. Azure DevOps Bugs
// don't carry a Description at all, so this is how the detail panes fall
// back to whichever narrative fields the item actually has.
func narrativeSections(it *model.WorkItem) []narrativeSection {
	var out []narrativeSection
	if it.Description != "" {
		out = append(out, narrativeSection{"Description", it.Description})
	}
	if it.ReproSteps != "" {
		out = append(out, narrativeSection{"Repro steps", it.ReproSteps})
	}
	if it.AcceptanceCriteria != "" {
		out = append(out, narrativeSection{"Acceptance criteria", it.AcceptanceCriteria})
	}
	return out
}

// narrativeKey is a cheap identity for a set of sections, for view caching:
// it changes whenever any section's heading or body changes.
func narrativeKey(sections []narrativeSection) string {
	var b strings.Builder
	for _, s := range sections {
		b.WriteString(s.heading)
		b.WriteByte(0)
		b.WriteString(s.body)
		b.WriteByte(0)
	}
	return b.String()
}

// renderNarrative renders a set of sections as one block. Each section gets
// its own muted heading only when more than one is present, so an item with
// just a Description renders exactly as it always has.
func renderNarrative(sections []narrativeSection, width int) string {
	var b strings.Builder
	for i, s := range sections {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if len(sections) > 1 {
			b.WriteString(sMuted.Render(s.heading) + "\n")
		}
		b.WriteString(markdown.Render(s.body, width))
	}
	return b.String()
}
