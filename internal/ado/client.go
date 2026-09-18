// Package ado abstracts Azure DevOps behind a small interface so the UI can be
// driven by either the real SDK or an in-memory fake.
package ado

import (
	"context"

	"github.com/sveinungoverland/devopstui/internal/model"
)

// Client is everything the UI needs from Azure DevOps.
type Client interface {
	// Me returns the display name of the authenticated user.
	Me(ctx context.Context) (string, error)

	Projects(ctx context.Context) ([]model.Project, error)
	Teams(ctx context.Context, project string) ([]model.Team, error)
	Iterations(ctx context.Context, project, team string) ([]model.Iteration, error)
	Boards(ctx context.Context, project, team string) ([]model.Board, error)
	BacklogConfig(ctx context.Context, project, team string) (model.BacklogConfig, error)
	// TeamAreas returns the area paths a team owns, default first.
	TeamAreas(ctx context.Context, project, team string) ([]model.TeamArea, error)
	States(ctx context.Context, project, workItemType string) ([]string, error)
	// People searches for assignable users. An empty query returns the
	// project's directory (everyone on any of its teams); a non-empty one
	// also searches the organisation's identities.
	People(ctx context.Context, project, query string) ([]model.Person, error)

	// SprintItems returns the items under iterationPath plus any parents of
	// those items that live outside it (returned in the second slice).
	SprintItems(ctx context.Context, project, team, iterationPath string) (items, external []*model.WorkItem, err error)
	// Unscheduled returns items sitting at the team's backlog root - not
	// assigned to any sprint - plus any parents of those items that live
	// outside it (returned in the second slice), the same shape as
	// SprintItems.
	Unscheduled(ctx context.Context, project, team string) (items, external []*model.WorkItem, err error)
	// Backlog returns the whole requirement-and-above backlog for the team.
	Backlog(ctx context.Context, project, team string) ([]*model.WorkItem, error)
	// MyItems returns open items assigned to the current user in the project.
	MyItems(ctx context.Context, project string) ([]*model.WorkItem, error)
	// Parents returns candidate parents (Epics and Features) in the project.
	Parents(ctx context.Context, project string) ([]*model.WorkItem, error)
	// Children returns the direct children of a work item.
	Children(ctx context.Context, project string, parentID int) ([]*model.WorkItem, error)
	// ChildrenOf batch-fetches children for several parents at once, keyed
	// by parent id. Parents with no children are simply absent from the map.
	ChildrenOf(ctx context.Context, project string, parentIDs []int) (map[int][]*model.WorkItem, error)

	Get(ctx context.Context, id int) (*model.WorkItem, error)
	// Comments returns a work item's discussion, oldest first.
	Comments(ctx context.Context, project string, id int) ([]model.Comment, error)
	// Update applies field patches with an optimistic concurrency check on rev.
	Update(ctx context.Context, id, rev int, patches []model.Patch) (*model.WorkItem, error)
	// SetParent re-parents id under parentID (0 removes the parent).
	SetParent(ctx context.Context, id, parentID int) (*model.WorkItem, error)
	// Create adds a new work item, linked under NewItem.ParentID when set.
	Create(ctx context.Context, project string, n model.NewItem) (*model.WorkItem, error)
}
