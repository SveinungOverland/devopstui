package ui

import (
	"fmt"
	"strings"

	"github.com/sveinungoverland/devopstui/internal/model"
)

// renderDetail draws the right-hand pane for one item.
func renderDetail(it *model.WorkItem, parent *model.WorkItem, children int, width int) string {
	if it == nil {
		return sMuted.Render("nothing selected")
	}
	width = max(width, 20)
	var b strings.Builder
	row := func(label, value string) {
		b.WriteString(sLabel.Render(label) + value + "\n")
	}
	head := kindStyle(it.Kind).Render(it.Type) + sMuted.Render(fmt.Sprintf("  #%d", it.ID))
	b.WriteString(head + "\n")
	b.WriteString(sTitle.Render(wrap(it.Title, width)) + "\n\n")

	row("State", stateStyle(it.State).Render(it.State))
	row("Assigned", it.Assignee())
	row("Iteration", lastSeg(it.IterationPath)+sMuted.Render("  "+it.IterationPath))
	if it.BoardColumn != "" && it.BoardColumn != it.State {
		row("Column", it.BoardColumn)
	}
	if it.Effort > 0 {
		row("Effort", fmtEffort(it.Effort))
	}
	if it.Priority > 0 {
		row("Priority", fmt.Sprintf("%d", it.Priority))
	}
	if parent != nil {
		row("Parent", kindStyle(parent.Kind).Render(parent.Kind.Tag())+sMuted.Render(fmt.Sprintf(" %d ", parent.ID))+trunc(parent.Title, width-20))
	} else if it.ParentID != 0 {
		row("Parent", sMuted.Render(fmt.Sprintf("#%d", it.ParentID)))
	}
	if children > 0 {
		row("Children", fmt.Sprintf("%d", children))
	}
	if len(it.Tags) > 0 {
		row("Tags", sMuted.Render(strings.Join(it.Tags, ", ")))
	}
	row("Updated", ago(it.ChangedDate)+sMuted.Render("  by "+it.ChangedBy))
	if it.Description != "" {
		b.WriteString("\n" + sMuted.Render(strings.Repeat("─", min(width, 40))) + "\n")
		b.WriteString(wrap(it.Description, width))
	}
	return b.String()
}
