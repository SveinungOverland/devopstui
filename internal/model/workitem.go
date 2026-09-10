// Package model holds the pure domain types used by the UI. Nothing in here
// imports the Azure DevOps SDK, so the UI can be exercised entirely with fakes.
package model

import "time"

// Kind is the coarse level of a work item in the backlog hierarchy.
type Kind int

const (
	KindOther Kind = iota
	KindEpic
	KindFeature
	KindRequirement // PBI / User Story
	KindTask
	KindBug
)

// Tag is the short label rendered in front of a row.
func (k Kind) Tag() string {
	switch k {
	case KindEpic:
		return "EPIC"
	case KindFeature:
		return "FEAT"
	case KindRequirement:
		return "PBI"
	case KindTask:
		return "TASK"
	case KindBug:
		return "BUG"
	default:
		return "ITEM"
	}
}

// WorkItem is the subset of an Azure DevOps work item the TUI cares about.
type WorkItem struct {
	ID            int
	Rev           int
	Type          string // raw System.WorkItemType
	Kind          Kind
	Title         string
	State         string
	AssignedTo    string // display name, "" when unassigned
	IterationPath string
	AreaPath      string
	BoardColumn   string
	Effort        float64 // Effort / Story Points, 0 when unset
	Priority      int     // 0 when unset
	Tags          []string
	Description   string // plain text
	ParentID      int    // 0 when no parent
	ChangedDate   time.Time
	ChangedBy     string
	URL           string // browser URL
}

// Assignee returns a display value for the assignee column.
func (w WorkItem) Assignee() string {
	if w.AssignedTo == "" {
		return "—"
	}
	return w.AssignedTo
}

// Iteration is a team sprint.
type Iteration struct {
	ID     string
	Name   string
	Path   string
	Start  time.Time
	Finish time.Time
	// Timeframe is "past", "current" or "future".
	Timeframe string
}

// Project is an Azure DevOps project.
type Project struct {
	ID   string
	Name string
}

// Team belongs to a project.
type Team struct {
	ID   string
	Name string
}

// Board is a Kanban board with ordered columns.
type Board struct {
	ID      string
	Name    string
	Columns []BoardColumn
}

// BoardColumn maps a column to the states it holds.
type BoardColumn struct {
	Name   string
	States []string
}

// Context is the "where am I" of the UI: everything a query needs.
type Context struct {
	Org       string
	Project   string
	Team      string
	Iteration Iteration
	Board     string
}

// Patch describes one field change to apply to a work item.
type Patch struct {
	Field string // e.g. "System.Title"
	Value any
}

// Well-known field reference names.
const (
	FieldTitle         = "System.Title"
	FieldState         = "System.State"
	FieldAssignedTo    = "System.AssignedTo"
	FieldIterationPath = "System.IterationPath"
	FieldAreaPath      = "System.AreaPath"
	FieldBoardColumn   = "System.BoardColumn"
	FieldDescription   = "System.Description"
	FieldTags          = "System.Tags"
	FieldWorkItemType  = "System.WorkItemType"
	FieldChangedDate   = "System.ChangedDate"
	FieldChangedBy     = "System.ChangedBy"
	FieldPriority      = "Microsoft.VSTS.Common.Priority"
	FieldEffort        = "Microsoft.VSTS.Scheduling.Effort"
	FieldStoryPoints   = "Microsoft.VSTS.Scheduling.StoryPoints"
)
