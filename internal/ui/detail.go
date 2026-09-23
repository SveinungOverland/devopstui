package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/sveinungoverland/devopstui/internal/markdown"
	"github.com/sveinungoverland/devopstui/internal/model"
)

// renderDetail draws the right-hand pane for one item. children are the
// item's direct children, if known; cfg decides which of them are tasks.
// comments is only ever populated by the Board, Sprint and Backlog views
// (see refreshDetail) and appended as a Discussion section after the
// description.
func renderDetail(it *model.WorkItem, parent *model.WorkItem, children []*model.WorkItem, comments []model.Comment, width int, cfg model.BacklogConfig) string {
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
	iter := lastSeg(it.IterationPath)
	if it.IterationPath != iter {
		iter += sMuted.Render("  " + it.IterationPath) // the full path only adds something when nested
	}
	row("Iteration", iter)
	if it.BoardColumn != "" && it.BoardColumn != it.State {
		row("Column", it.BoardColumn)
	}
	if it.Effort > 0 {
		row("Effort", fmtEffort(it.Effort))
	}
	if cfg.TaskLevel(it) && it.RemainingWork > 0 {
		row("Remaining", fmtEffort(it.RemainingWork)+"h")
	}
	if it.Priority > 0 {
		row("Priority", fmt.Sprintf("%d", it.Priority))
	}
	if parent != nil {
		row("Parent", kindStyle(parent.Kind).Render(parent.Kind.Tag())+sMuted.Render(fmt.Sprintf(" %d ", parent.ID))+trunc(parent.Title, width-20))
	} else if it.ParentID != 0 {
		row("Parent", sMuted.Render(fmt.Sprintf("#%d", it.ParentID)))
	}
	if len(it.Tags) > 0 {
		row("Tags", sMuted.Render(strings.Join(it.Tags, ", ")))
	}
	row("Updated", ago(it.ChangedDate)+sMuted.Render("  by "+it.ChangedBy))

	if len(children) > 0 {
		p := computeProgress(children, func(w *model.WorkItem) bool { return w.ParentID == it.ID && cfg.TaskLevel(w) })
		head := fmt.Sprintf("%d children", len(children))
		if pr, ok := p[it.ID]; ok {
			head = fmt.Sprintf("%d/%d tasks done", pr.done, pr.total)
			if pr.remaining > 0 {
				head += fmt.Sprintf(" · %sh remaining", fmtEffort(pr.remaining))
			}
			if extra := len(children) - pr.total; extra > 0 {
				head += fmt.Sprintf(" · %d other", extra)
			}
		}
		b.WriteString("\n" + sMuted.Render(head) + "\n")
		for _, c := range children {
			mark := "○"
			if isDone(c.State) {
				mark = sOK.Render("●")
			}
			rem := ""
			if cfg.TaskLevel(c) && c.RemainingWork > 0 && !isDone(c.State) {
				rem = fmtEffort(c.RemainingWork) + "h"
			}
			// Columns: prefix(*) state(11) remaining(4) assignee(3)
			prefixW := width - 22
			prefix := fmt.Sprintf(" %s %s %s ", mark, kindStyle(c.Kind).Render(pad(c.Kind.Tag(), 4)), sMuted.Render(fmt.Sprintf("%d", c.ID)))
			prefix += trunc(c.Title, prefixW-lipgloss.Width(prefix)-1)
			line := pad(prefix, prefixW) + " " + stateStyle(c.State).Render(pad(trunc(c.State, 11), 11)) +
				sMuted.Render(padLeft(rem, 4)) + " " + sMuted.Render(initials(c.AssignedTo))
			b.WriteString(line + "\n")
		}
	}

	if sections := narrativeSections(it); len(sections) > 0 {
		b.WriteString("\n" + sMuted.Render(strings.Repeat("─", width)) + "\n")
		b.WriteString(renderNarrative(sections, width))
	}

	if len(comments) > 0 {
		b.WriteString("\n" + sMuted.Render(strings.Repeat("─", width)) + "\n")
		b.WriteString(sMuted.Render(fmt.Sprintf("Discussion (%d)", len(comments))) + "\n\n")
		b.WriteString(markdown.Render(formatComments(comments), width))
	}
	return b.String()
}
