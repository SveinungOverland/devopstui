// Package model holds the pure domain types used by the UI. Nothing in here
// imports the Azure DevOps SDK, so the UI can be exercised entirely with fakes.
package model

import (
	"strings"
	"time"
)

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
	ID                 int
	Rev                int
	Type               string // raw System.WorkItemType
	Kind               Kind
	Title              string
	State              string
	AssignedTo         string // display name, "" when unassigned
	AssignedToUnique   string // sign-in address of the assignee, when known
	IterationPath      string
	AreaPath           string
	BoardColumn        string
	Effort             float64 // Effort / Story Points, 0 when unset
	RemainingWork      float64 // hours, tasks only
	Priority           int     // 0 when unset
	Tags               []string
	Description        string // plain text
	ReproSteps         string // plain text, Bugs only
	AcceptanceCriteria string // plain text
	ParentID           int    // 0 when no parent
	ChangedDate        time.Time
	ChangedBy          string
	URL                string // browser URL
	Project            string // team project, "" when unknown
}

// Assignee returns a display value for the assignee column.
func (w WorkItem) Assignee() string {
	if w.AssignedTo == "" {
		return "—"
	}
	return w.AssignedTo
}

// AssigneeRef is the value to write when copying this item's assignee to
// another item: the sign-in address when known, else the display name.
func (w WorkItem) AssigneeRef() string {
	if w.AssignedToUnique != "" {
		return w.AssignedToUnique
	}
	return w.AssignedTo
}

// Comment is one entry in a work item's discussion.
type Comment struct {
	ID          int
	Author      string
	CreatedDate time.Time
	Text        string // markdown
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

// Person is someone a work item can be assigned to.
type Person struct {
	DisplayName string
	UniqueName  string // sign-in address, "" when unknown
}

// Assignment is the value to write to System.AssignedTo. The sign-in
// address is unambiguous; the display name is the fallback.
func (p Person) Assignment() string {
	if p.UniqueName != "" {
		return p.UniqueName
	}
	return p.DisplayName
}

// Key identifies a person for de-duplication.
func (p Person) Key() string {
	if p.UniqueName != "" {
		return strings.ToLower(p.UniqueName)
	}
	return strings.ToLower(p.DisplayName)
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
	Backlog   BacklogConfig
	// FilterTeam narrows every view to items in that team's area paths.
	// It is independent of Team, which owns the sprints: in organisations
	// where sub-teams share a parent's sprints, Team is the parent and
	// FilterTeam the sub-team.
	FilterTeam  string
	FilterAreas []TeamArea
}

// TeamArea is one area path a team owns.
type TeamArea struct {
	Path            string
	IncludeChildren bool
}

// InAreas reports whether an area path belongs to any of the team areas.
func InAreas(area string, areas []TeamArea) bool {
	for _, a := range areas {
		if area == a.Path {
			return true
		}
		if a.IncludeChildren && strings.HasPrefix(area, a.Path+"\\") {
			return true
		}
	}
	return false
}

// DefaultArea returns the first area path (the team's default), or "".
func DefaultArea(areas []TeamArea) string {
	if len(areas) == 0 {
		return ""
	}
	return areas[0].Path
}

// BacklogConfig is the part of the team's process configuration the UI
// needs: which type sits at the requirement level and how bugs behave.
type BacklogConfig struct {
	RequirementType string // "Product Backlog Item", "User Story", …
	TaskType        string // usually "Task"
	FeatureType     string // "Feature"
	EpicType        string // "Epic"
	// BugsBehavior is "asRequirements", "asTasks" or "off".
	BugsBehavior string
}

// TaskLevel reports whether an item sits below the requirement level,
// taking the team's bug setting into account.
func (c BacklogConfig) TaskLevel(w *WorkItem) bool {
	switch w.Kind {
	case KindTask:
		return true
	case KindBug:
		return c.BugsBehavior == "asTasks"
	}
	return false
}

// IsTaskType reports whether a work item type name sits at the task
// level, taking the team's bug setting into account.
func (c BacklogConfig) IsTaskType(typ string) bool {
	task := c.TaskType
	if task == "" {
		task = "Task"
	}
	if strings.EqualFold(typ, task) {
		return true
	}
	return c.BugsBehavior == "asTasks" && strings.EqualFold(typ, "Bug")
}

// ChildType returns the type to create under parent, or "" when parent
// cannot have children. A nil parent yields the requirement type.
func (c BacklogConfig) ChildType(parent *WorkItem) string {
	req := c.RequirementType
	if req == "" {
		req = "Product Backlog Item"
	}
	task := c.TaskType
	if task == "" {
		task = "Task"
	}
	if parent == nil {
		return req
	}
	switch parent.Kind {
	case KindEpic:
		if c.FeatureType != "" {
			return c.FeatureType
		}
		return "Feature"
	case KindFeature:
		return req
	case KindRequirement:
		return task
	case KindBug:
		if c.BugsBehavior == "asTasks" {
			return ""
		}
		return task
	}
	return ""
}

// BugChildOf reports whether a Bug may stand in for the type ChildType
// would otherwise create under parent, following the team's bug setting.
func (c BacklogConfig) BugChildOf(parent *WorkItem) bool {
	typ := c.ChildType(parent)
	if typ == "" {
		return false
	}
	req := c.RequirementType
	if req == "" {
		req = "Product Backlog Item"
	}
	task := c.TaskType
	if task == "" {
		task = "Task"
	}
	switch c.BugsBehavior {
	case "asRequirements":
		return strings.EqualFold(typ, req)
	case "asTasks":
		return strings.EqualFold(typ, task)
	}
	return false
}

// Patch describes one field change to apply to a work item.
type Patch struct {
	Field string // e.g. "System.Title"
	Value any
}

// Well-known field reference names.
const (
	FieldTitle              = "System.Title"
	FieldState              = "System.State"
	FieldAssignedTo         = "System.AssignedTo"
	FieldIterationPath      = "System.IterationPath"
	FieldAreaPath           = "System.AreaPath"
	FieldBoardColumn        = "System.BoardColumn"
	FieldDescription        = "System.Description"
	FieldReproSteps         = "Microsoft.VSTS.TCM.ReproSteps"
	FieldAcceptanceCriteria = "Microsoft.VSTS.Common.AcceptanceCriteria"
	FieldTags               = "System.Tags"
	FieldWorkItemType       = "System.WorkItemType"
	FieldChangedDate        = "System.ChangedDate"
	FieldChangedBy          = "System.ChangedBy"
	FieldPriority           = "Microsoft.VSTS.Common.Priority"
	FieldEffort             = "Microsoft.VSTS.Scheduling.Effort"
	FieldStoryPoints        = "Microsoft.VSTS.Scheduling.StoryPoints"
	FieldRemainingWork      = "Microsoft.VSTS.Scheduling.RemainingWork"
)

// NewItem describes a work item to create.
type NewItem struct {
	Type          string
	Title         string
	ParentID      int
	IterationPath string
	AreaPath      string
	// AssignedTo is a display name or sign-in address; empty leaves the
	// new item unassigned.
	AssignedTo string
}
