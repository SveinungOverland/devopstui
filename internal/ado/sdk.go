package ado

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/core"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/identity"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/webapi"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/work"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/workitemtracking"

	"github.com/sveinungoverland/devopstui/internal/markdown"
	"github.com/sveinungoverland/devopstui/internal/model"
)

// SDK is the Client backed by azure-devops-go-api.
type SDK struct {
	orgURL string
	conn   *azuredevops.Connection
	core   core.Client
	work   work.Client
	wit    workitemtracking.Client

	mu         sync.Mutex
	me         string
	states     map[string][]string
	people     map[string][]model.Person // project → directory cache
	ident      identity.Client
	identTried bool

	// WriteHTML converts Markdown descriptions to HTML on write instead of
	// using Azure DevOps' native Markdown mode.
	WriteHTML bool
}

// NewSDK connects to an organisation with a personal access token.
func NewSDK(ctx context.Context, orgURL, pat string) (*SDK, error) {
	orgURL = strings.TrimRight(orgURL, "/")
	conn := azuredevops.NewPatConnection(orgURL, pat)
	c, err := core.NewClient(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("core client: %w", err)
	}
	w, err := work.NewClient(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("work client: %w", err)
	}
	wit, err := workitemtracking.NewClient(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("work item client: %w", err)
	}
	return &SDK{orgURL: orgURL, conn: conn, core: c, work: w, wit: wit,
		states: map[string][]string{}, people: map[string][]model.Person{}}, nil
}

var fields = []string{
	"System.Id", "System.Rev", model.FieldWorkItemType, model.FieldTitle, model.FieldState, model.FieldAssignedTo,
	model.FieldIterationPath, model.FieldAreaPath, model.FieldBoardColumn, model.FieldTags, model.FieldPriority,
	model.FieldEffort, model.FieldStoryPoints, model.FieldRemainingWork, model.FieldChangedDate, model.FieldChangedBy, "System.Parent",
	model.FieldDescription,
}

func (s *SDK) Me(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.me != "" {
		defer s.mu.Unlock()
		return s.me, nil
	}
	s.mu.Unlock()

	client := azuredevops.NewClient(s.conn, s.orgURL)
	req, err := client.CreateRequestMessage(ctx, http.MethodGet, s.orgURL+"/_apis/connectionData", "", nil, "", "application/json", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.SendRequest(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var body struct {
		AuthenticatedUser struct {
			ProviderDisplayName string `json:"providerDisplayName"`
		} `json:"authenticatedUser"`
	}
	if err := client.UnmarshalBody(resp, &body); err != nil {
		return "", err
	}
	s.mu.Lock()
	s.me = body.AuthenticatedUser.ProviderDisplayName
	s.mu.Unlock()
	return s.me, nil
}

func (s *SDK) Projects(ctx context.Context) ([]model.Project, error) {
	var out []model.Project
	var token string
	for {
		args := core.GetProjectsArgs{}
		if token != "" {
			t, _ := strconv.Atoi(token)
			args.ContinuationToken = &t
		}
		resp, err := s.core.GetProjects(ctx, args)
		if err != nil {
			return nil, err
		}
		for _, p := range resp.Value {
			out = append(out, model.Project{ID: p.Id.String(), Name: deref(p.Name)})
		}
		if resp.ContinuationToken == "" {
			break
		}
		token = resp.ContinuationToken
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *SDK) Teams(ctx context.Context, project string) ([]model.Team, error) {
	teams, err := s.core.GetTeams(ctx, core.GetTeamsArgs{ProjectId: &project})
	if err != nil {
		return nil, err
	}
	var out []model.Team
	for _, t := range *teams {
		out = append(out, model.Team{ID: t.Id.String(), Name: deref(t.Name)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *SDK) Iterations(ctx context.Context, project, team string) ([]model.Iteration, error) {
	its, err := s.work.GetTeamIterations(ctx, work.GetTeamIterationsArgs{Project: &project, Team: &team})
	if err != nil {
		return nil, err
	}
	var out []model.Iteration
	for _, it := range *its {
		m := model.Iteration{ID: it.Id.String(), Name: deref(it.Name), Path: deref(it.Path)}
		if it.Attributes != nil {
			if it.Attributes.StartDate != nil {
				m.Start = it.Attributes.StartDate.Time
			}
			if it.Attributes.FinishDate != nil {
				m.Finish = it.Attributes.FinishDate.Time
			}
			if it.Attributes.TimeFrame != nil {
				m.Timeframe = string(*it.Attributes.TimeFrame)
			}
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

// ProjectIterations walks the project's classification tree for iterations,
// depth first, and returns every node below the project root. Paths are
// built from each node's own Name rather than trusting the API's Path field,
// which is prefixed with the tree's root segment ("\Platform\Iteration\...")
// and would not match the System.IterationPath format the rest of this
// codebase writes (e.g. "Platform\Sprint 42", see fake.go).
func (s *SDK) ProjectIterations(ctx context.Context, project string) ([]model.Iteration, error) {
	depth := 25
	root, err := s.wit.GetClassificationNode(ctx, workitemtracking.GetClassificationNodeArgs{
		Project:        &project,
		StructureGroup: &workitemtracking.TreeStructureGroupValues.Iterations,
		Depth:          &depth,
	})
	if err != nil {
		return nil, err
	}
	var out []model.Iteration
	var walk func(nodes []workitemtracking.WorkItemClassificationNode, parentPath string)
	walk = func(nodes []workitemtracking.WorkItemClassificationNode, parentPath string) {
		for _, n := range nodes {
			path := parentPath + "\\" + deref(n.Name)
			m := model.Iteration{Name: deref(n.Name), Path: path}
			if n.Identifier != nil {
				m.ID = n.Identifier.String()
			}
			if n.Attributes != nil {
				attrs := *n.Attributes
				m.Start = attrDate(attrs["startDate"])
				m.Finish = attrDate(attrs["finishDate"])
			}
			out = append(out, m)
			if n.Children != nil {
				walk(*n.Children, path)
			}
		}
	}
	if root.Children != nil {
		walk(*root.Children, project)
	}
	return out, nil
}

// attrDate reads a date out of a classification node's loosely typed
// Attributes map, where the API sends dates as RFC3339 strings.
func attrDate(v any) time.Time {
	s, _ := v.(string)
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func (s *SDK) Boards(ctx context.Context, project, team string) ([]model.Board, error) {
	refs, err := s.work.GetBoards(ctx, work.GetBoardsArgs{Project: &project, Team: &team})
	if err != nil {
		return nil, err
	}
	var out []model.Board
	for _, r := range *refs {
		id := r.Id.String()
		b, err := s.work.GetBoard(ctx, work.GetBoardArgs{Project: &project, Team: &team, Id: &id})
		if err != nil {
			return nil, err
		}
		mb := model.Board{ID: id, Name: deref(r.Name)}
		if b.Columns != nil {
			for _, c := range *b.Columns {
				col := model.BoardColumn{Name: deref(c.Name)}
				if c.StateMappings != nil {
					seen := map[string]bool{}
					for _, st := range *c.StateMappings {
						if !seen[st] {
							seen[st] = true
							col.States = append(col.States, st)
						}
					}
				}
				mb.Columns = append(mb.Columns, col)
			}
		}
		out = append(out, mb)
	}
	return out, nil
}

func (s *SDK) BacklogConfig(ctx context.Context, project, team string) (model.BacklogConfig, error) {
	cfg, err := s.work.GetBacklogConfigurations(ctx, work.GetBacklogConfigurationsArgs{Project: &project, Team: &team})
	if err != nil {
		return model.BacklogConfig{}, err
	}
	out := model.BacklogConfig{RequirementType: "Product Backlog Item", TaskType: "Task", FeatureType: "Feature", EpicType: "Epic", BugsBehavior: "asRequirements"}
	if cfg.BugsBehavior != nil {
		out.BugsBehavior = string(*cfg.BugsBehavior)
	}
	defType := func(l *work.BacklogLevelConfiguration) string {
		if l != nil && l.DefaultWorkItemType != nil {
			return deref(l.DefaultWorkItemType.Name)
		}
		return ""
	}
	if t := defType(cfg.RequirementBacklog); t != "" {
		out.RequirementType = t
	}
	if t := defType(cfg.TaskBacklog); t != "" {
		out.TaskType = t
	}
	if cfg.PortfolioBacklogs != nil {
		// Portfolio backlogs are ordered by rank; the lowest is Features.
		levels := *cfg.PortfolioBacklogs
		sort.SliceStable(levels, func(i, j int) bool { return deref(levels[i].Rank) < deref(levels[j].Rank) })
		if len(levels) > 0 {
			if t := defType(&levels[0]); t != "" {
				out.FeatureType = t
			}
		}
		if len(levels) > 1 {
			if t := defType(&levels[len(levels)-1]); t != "" {
				out.EpicType = t
			}
		}
	}
	return out, nil
}

func (s *SDK) TeamAreas(ctx context.Context, project, team string) ([]model.TeamArea, error) {
	tf, err := s.work.GetTeamFieldValues(ctx, work.GetTeamFieldValuesArgs{Project: &project, Team: &team})
	if err != nil {
		return nil, err
	}
	var out []model.TeamArea
	def := deref(tf.DefaultValue)
	if tf.Values != nil {
		for _, v := range *tf.Values {
			a := model.TeamArea{Path: deref(v.Value), IncludeChildren: deref(v.IncludeChildren)}
			if a.Path == def {
				out = append([]model.TeamArea{a}, out...) // default first
			} else {
				out = append(out, a)
			}
		}
	}
	if len(out) == 0 && def != "" {
		out = []model.TeamArea{{Path: def, IncludeChildren: true}}
	}
	return out, nil
}

func (s *SDK) Create(ctx context.Context, project string, n model.NewItem) (*model.WorkItem, error) {
	doc := []webapi.JsonPatchOperation{
		{Op: &webapi.OperationValues.Add, Path: ptr("/fields/" + model.FieldTitle), Value: n.Title},
	}
	if n.IterationPath != "" {
		doc = append(doc, webapi.JsonPatchOperation{Op: &webapi.OperationValues.Add, Path: ptr("/fields/" + model.FieldIterationPath), Value: n.IterationPath})
	}
	if n.AreaPath != "" {
		doc = append(doc, webapi.JsonPatchOperation{Op: &webapi.OperationValues.Add, Path: ptr("/fields/" + model.FieldAreaPath), Value: n.AreaPath})
	}
	if n.AssignedTo != "" {
		doc = append(doc, webapi.JsonPatchOperation{Op: &webapi.OperationValues.Add, Path: ptr("/fields/" + model.FieldAssignedTo), Value: n.AssignedTo})
	}
	if n.ParentID != 0 {
		doc = append(doc, webapi.JsonPatchOperation{Op: &webapi.OperationValues.Add, Path: ptr("/relations/-"), Value: map[string]any{
			"rel": "System.LinkTypes.Hierarchy-Reverse",
			"url": fmt.Sprintf("%s/_apis/wit/workItems/%d", s.orgURL, n.ParentID),
		}})
	}
	typ := n.Type
	wi, err := s.wit.CreateWorkItem(ctx, workitemtracking.CreateWorkItemArgs{Project: &project, Type: &typ, Document: &doc})
	if err != nil {
		return nil, err
	}
	return s.convert(wi, project), nil
}

func (s *SDK) States(ctx context.Context, project, typ string) ([]string, error) {
	key := project + "/" + typ
	s.mu.Lock()
	if v, ok := s.states[key]; ok {
		s.mu.Unlock()
		return v, nil
	}
	s.mu.Unlock()
	res, err := s.wit.GetWorkItemTypeStates(ctx, workitemtracking.GetWorkItemTypeStatesArgs{Project: &project, Type: &typ})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, st := range *res {
		out = append(out, deref(st.Name))
	}
	s.mu.Lock()
	s.states[key] = out
	s.mu.Unlock()
	return out, nil
}

// query runs a flat WIQL query and fetches the items.
func (s *SDK) query(ctx context.Context, project, wiql string) ([]*model.WorkItem, error) {
	top := 2000
	res, err := s.wit.QueryByWiql(ctx, workitemtracking.QueryByWiqlArgs{Wiql: &workitemtracking.Wiql{Query: &wiql}, Project: &project, Top: &top})
	if err != nil {
		return nil, err
	}
	var ids []int
	if res.WorkItems != nil {
		for _, r := range *res.WorkItems {
			ids = append(ids, *r.Id)
		}
	}
	return s.fetch(ctx, project, ids)
}

// fetch loads work items by id in batches of 200.
func (s *SDK) fetch(ctx context.Context, project string, ids []int) ([]*model.WorkItem, error) {
	var out []*model.WorkItem
	for start := 0; start < len(ids); start += 200 {
		end := min(start+200, len(ids))
		chunk := ids[start:end]
		policy := workitemtracking.WorkItemErrorPolicyValues.Omit
		res, err := s.wit.GetWorkItemsBatch(ctx, workitemtracking.GetWorkItemsBatchArgs{
			Project: &project,
			WorkItemGetRequest: &workitemtracking.WorkItemBatchGetRequest{
				Ids: &chunk, Fields: &fields, ErrorPolicy: &policy,
			},
		})
		if err != nil {
			return nil, err
		}
		for i := range *res {
			out = append(out, s.convert(&(*res)[i], project))
		}
	}
	return out, nil
}

func (s *SDK) SprintItems(ctx context.Context, project, team, iter string) ([]*model.WorkItem, []*model.WorkItem, error) {
	items, err := s.query(ctx, project, fmt.Sprintf(
		"SELECT [System.Id] FROM WorkItems WHERE [System.TeamProject] = @project AND [System.IterationPath] UNDER '%s' AND [System.State] <> 'Removed' ORDER BY [Microsoft.VSTS.Common.BacklogPriority] ASC, [System.Id] ASC",
		escape(iter)))
	if err != nil {
		return nil, nil, err
	}
	external, err := s.externalParents(ctx, project, items)
	if err != nil {
		return nil, nil, err
	}
	return items, external, nil
}

// Unscheduled returns items whose iteration is exactly the team's project
// root - the backlog convention this codebase already uses elsewhere for
// "no sprint" (see pickMoveTarget in internal/ui/actions.go) - rather than
// SprintItems' UNDER match, which would also pull in every sprint nested
// under that root.
func (s *SDK) Unscheduled(ctx context.Context, project, team string) ([]*model.WorkItem, []*model.WorkItem, error) {
	items, err := s.query(ctx, project, fmt.Sprintf(
		"SELECT [System.Id] FROM WorkItems WHERE [System.TeamProject] = @project AND [System.IterationPath] = '%s' AND [System.State] <> 'Removed' ORDER BY [Microsoft.VSTS.Common.BacklogPriority] ASC, [System.Id] ASC",
		escape(project)))
	if err != nil {
		return nil, nil, err
	}
	external, err := s.externalParents(ctx, project, items)
	if err != nil {
		return nil, nil, err
	}
	return items, external, nil
}

// externalParents walks the ancestry of items and returns any ancestors
// outside that set (usually Features/Epics), for display alongside a
// filtered item list.
func (s *SDK) externalParents(ctx context.Context, project string, items []*model.WorkItem) ([]*model.WorkItem, error) {
	in := map[int]bool{}
	for _, it := range items {
		in[it.ID] = true
	}
	var external []*model.WorkItem
	known := map[int]*model.WorkItem{}
	for _, it := range items {
		known[it.ID] = it
	}
	frontier := map[int]bool{}
	for _, it := range items {
		if it.ParentID != 0 && !in[it.ParentID] {
			frontier[it.ParentID] = true
		}
	}
	for len(frontier) > 0 {
		var ids []int
		for id := range frontier {
			if _, ok := known[id]; !ok {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			break
		}
		parents, err := s.fetch(ctx, project, ids)
		if err != nil {
			return nil, err
		}
		frontier = map[int]bool{}
		for _, p := range parents {
			known[p.ID] = p
			external = append(external, p)
			if p.ParentID != 0 {
				if _, ok := known[p.ParentID]; !ok {
					frontier[p.ParentID] = true
				}
			}
		}
	}
	return external, nil
}

func (s *SDK) Backlog(ctx context.Context, project, team string) ([]*model.WorkItem, error) {
	return s.query(ctx, project,
		"SELECT [System.Id] FROM WorkItems WHERE [System.TeamProject] = @project AND [System.WorkItemType] IN ('Epic','Feature','Product Backlog Item','User Story','Bug') AND [System.State] NOT IN ('Removed','Closed','Done') ORDER BY [Microsoft.VSTS.Common.BacklogPriority] ASC")
}

func (s *SDK) MyItems(ctx context.Context, project string) ([]*model.WorkItem, error) {
	return s.query(ctx, project,
		"SELECT [System.Id] FROM WorkItems WHERE [System.TeamProject] = @project AND [System.AssignedTo] = @Me AND [System.State] <> 'Removed' ORDER BY [System.ChangedDate] DESC")
}

func (s *SDK) Parents(ctx context.Context, project string) ([]*model.WorkItem, error) {
	return s.query(ctx, project,
		"SELECT [System.Id] FROM WorkItems WHERE [System.TeamProject] = @project AND [System.WorkItemType] IN ('Epic','Feature') AND [System.State] NOT IN ('Removed','Closed','Done') ORDER BY [System.WorkItemType], [System.Title]")
}

func (s *SDK) Children(ctx context.Context, project string, parentID int) ([]*model.WorkItem, error) {
	return s.query(ctx, project, fmt.Sprintf(
		"SELECT [System.Id] FROM WorkItems WHERE [System.Parent] = %d AND [System.State] <> 'Removed' ORDER BY [Microsoft.VSTS.Common.BacklogPriority] ASC, [System.Id] ASC",
		parentID))
}

func (s *SDK) ChildrenOf(ctx context.Context, project string, parentIDs []int) (map[int][]*model.WorkItem, error) {
	out := map[int][]*model.WorkItem{}
	if len(parentIDs) == 0 {
		return out, nil
	}
	ids := make([]string, len(parentIDs))
	for i, id := range parentIDs {
		ids[i] = strconv.Itoa(id)
	}
	items, err := s.query(ctx, project, fmt.Sprintf(
		// System.Parent can be filtered on but, unlike in Children's single-id
		// query, Azure DevOps rejects sorting by it when used with IN().
		"SELECT [System.Id] FROM WorkItems WHERE [System.Parent] IN (%s) AND [System.State] <> 'Removed' ORDER BY [Microsoft.VSTS.Common.BacklogPriority] ASC, [System.Id] ASC",
		strings.Join(ids, ",")))
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		out[it.ParentID] = append(out[it.ParentID], it)
	}
	return out, nil
}

func (s *SDK) Get(ctx context.Context, id int) (*model.WorkItem, error) {
	wi, err := s.wit.GetWorkItem(ctx, workitemtracking.GetWorkItemArgs{Id: &id, Fields: &fields})
	if err != nil {
		return nil, err
	}
	return s.convert(wi, ""), nil
}

func (s *SDK) Comments(ctx context.Context, project string, id int) ([]model.Comment, error) {
	top := 200
	order := workitemtracking.CommentSortOrderValues.Asc
	res, err := s.wit.GetComments(ctx, workitemtracking.GetCommentsArgs{Project: &project, WorkItemId: &id, Order: &order, Top: &top})
	if err != nil {
		return nil, err
	}
	var out []model.Comment
	if res.Comments != nil {
		for _, c := range *res.Comments {
			out = append(out, convertComment(c))
		}
	}
	return out, nil
}

// AddComment posts a new comment, writing Markdown or HTML per WriteHTML the
// same way fieldOps does for the description.
func (s *SDK) AddComment(ctx context.Context, project string, id int, text string) (model.Comment, error) {
	body := s.commentBody(text)
	c, err := s.wit.AddComment(ctx, workitemtracking.AddCommentArgs{
		Project:    &project,
		WorkItemId: &id,
		Request:    &workitemtracking.CommentCreate{Text: &body},
	})
	if err != nil {
		return model.Comment{}, err
	}
	return convertComment(*c), nil
}

// commentBody converts a comment's Markdown to HTML when WriteHTML is set,
// mirroring fieldOps' handling of the description.
func (s *SDK) commentBody(text string) string {
	if s.WriteHTML {
		return markdown.ToHTML(text)
	}
	return text
}

// convertComment maps a raw SDK comment to model.Comment, running its HTML
// body through the same HTML→Markdown conversion Description gets on read.
func convertComment(c workitemtracking.Comment) model.Comment {
	m := model.Comment{ID: deref(c.Id), Text: markdown.FromHTML(deref(c.Text))}
	if c.CreatedBy != nil {
		m.Author = deref(c.CreatedBy.DisplayName)
	}
	if c.CreatedDate != nil {
		m.CreatedDate = c.CreatedDate.Time
	}
	return m
}

func (s *SDK) Update(ctx context.Context, id, rev int, patches []model.Patch) (*model.WorkItem, error) {
	doc := []webapi.JsonPatchOperation{{Op: &webapi.OperationValues.Test, Path: ptr("/rev"), Value: rev}}
	for _, p := range patches {
		doc = append(doc, s.fieldOps(p.Field, p.Value)...)
	}
	wi, err := s.wit.UpdateWorkItem(ctx, workitemtracking.UpdateWorkItemArgs{Id: &id, Document: &doc})
	if err != nil {
		return nil, err
	}
	return s.convert(wi, ""), nil
}

// fieldOps builds the patch operations for one field. Large text fields
// are written as Markdown in Azure DevOps' native Markdown mode by adding
// the multilineFieldsFormat operation, unless WriteHTML is set.
func (s *SDK) fieldOps(field string, value any) []webapi.JsonPatchOperation {
	add := &webapi.OperationValues.Add
	if field != model.FieldDescription {
		return []webapi.JsonPatchOperation{{Op: add, Path: ptr("/fields/" + field), Value: value}}
	}
	text, _ := value.(string)
	if s.WriteHTML {
		return []webapi.JsonPatchOperation{{Op: add, Path: ptr("/fields/" + field), Value: markdown.ToHTML(text)}}
	}
	return []webapi.JsonPatchOperation{
		{Op: add, Path: ptr("/fields/" + field), Value: text},
		{Op: add, Path: ptr("/multilineFieldsFormat/" + field), Value: "Markdown"},
	}
}

func (s *SDK) SetParent(ctx context.Context, id, parentID int) (*model.WorkItem, error) {
	expand := workitemtracking.WorkItemExpandValues.Relations
	wi, err := s.wit.GetWorkItem(ctx, workitemtracking.GetWorkItemArgs{Id: &id, Expand: &expand})
	if err != nil {
		return nil, err
	}
	var doc []webapi.JsonPatchOperation
	doc = append(doc, webapi.JsonPatchOperation{Op: &webapi.OperationValues.Test, Path: ptr("/rev"), Value: deref(wi.Rev)})
	if wi.Relations != nil {
		// Remove from the highest index down so indices stay valid.
		for i := len(*wi.Relations) - 1; i >= 0; i-- {
			if deref((*wi.Relations)[i].Rel) == "System.LinkTypes.Hierarchy-Reverse" {
				doc = append(doc, webapi.JsonPatchOperation{Op: &webapi.OperationValues.Remove, Path: ptr(fmt.Sprintf("/relations/%d", i))})
			}
		}
	}
	if parentID != 0 {
		doc = append(doc, webapi.JsonPatchOperation{Op: &webapi.OperationValues.Add, Path: ptr("/relations/-"), Value: map[string]any{
			"rel": "System.LinkTypes.Hierarchy-Reverse",
			"url": fmt.Sprintf("%s/_apis/wit/workItems/%d", s.orgURL, parentID),
		}})
	}
	upd, err := s.wit.UpdateWorkItem(ctx, workitemtracking.UpdateWorkItemArgs{Id: &id, Document: &doc})
	if err != nil {
		return nil, err
	}
	return s.convert(upd, ""), nil
}

func (s *SDK) convert(wi *workitemtracking.WorkItem, project string) *model.WorkItem {
	f := map[string]any{}
	if wi.Fields != nil {
		f = *wi.Fields
	}
	m := &model.WorkItem{ID: deref(wi.Id), Rev: deref(wi.Rev)}
	m.Type = str(f[model.FieldWorkItemType])
	m.Kind = KindOf(m.Type)
	m.Title = str(f[model.FieldTitle])
	m.State = str(f[model.FieldState])
	m.IterationPath = str(f[model.FieldIterationPath])
	m.AreaPath = str(f[model.FieldAreaPath])
	m.BoardColumn = str(f[model.FieldBoardColumn])
	m.Priority = int(num(f[model.FieldPriority]))
	m.Effort = num(f[model.FieldEffort])
	if m.Effort == 0 {
		m.Effort = num(f[model.FieldStoryPoints])
	}
	m.RemainingWork = num(f[model.FieldRemainingWork])
	m.ParentID = int(num(f["System.Parent"]))
	m.AssignedTo = identityName(f[model.FieldAssignedTo])
	m.AssignedToUnique = identityUnique(f[model.FieldAssignedTo])
	m.ChangedBy = identityName(f[model.FieldChangedBy])
	if t, ok := f[model.FieldChangedDate].(azuredevops.Time); ok {
		m.ChangedDate = t.Time
	} else if ts := str(f[model.FieldChangedDate]); ts != "" {
		m.ChangedDate, _ = time.Parse(time.RFC3339, ts)
	}
	if tags := str(f[model.FieldTags]); tags != "" {
		for _, t := range strings.Split(tags, ";") {
			m.Tags = append(m.Tags, strings.TrimSpace(t))
		}
	}
	m.Description = markdown.FromHTML(str(f[model.FieldDescription]))
	proj := project
	if proj == "" {
		proj = str(f["System.TeamProject"])
	}
	if proj == "" && m.AreaPath != "" {
		proj = strings.SplitN(m.AreaPath, "\\", 2)[0]
	}
	m.URL = fmt.Sprintf("%s/%s/_workitems/edit/%d", s.orgURL, proj, m.ID)
	return m
}

func identityName(v any) string {
	switch x := v.(type) {
	case map[string]any:
		return str(x["displayName"])
	case string:
		return x
	}
	return ""
}

// identityUnique is the sign-in address of an identity field, when the
// server sent one.
func identityUnique(v any) string {
	if m, ok := v.(map[string]any); ok {
		return str(m["uniqueName"])
	}
	return ""
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

func escape(s string) string { return strings.ReplaceAll(s, "'", "''") }

func ptr[T any](v T) *T { return &v }

func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

var _ Client = (*SDK)(nil)
