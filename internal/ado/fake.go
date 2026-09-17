package ado

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sveinungoverland/devopstui/internal/model"
)

// Fake is an in-memory Client used for --demo mode and tests.
type Fake struct {
	mu       sync.Mutex
	me       string
	items    map[int]*model.WorkItem
	comments map[int][]model.Comment // oldest first
	iters    []model.Iteration
	nextID   int
	Updates  []FakeUpdate // recorded writes, for tests
	Latency  time.Duration
	FailNext error
}

// commentPageSize is small on purpose so demo data and tests can exercise
// the "load older" fetch-more path without seeding huge threads.
const commentPageSize = 2

// FakeUpdate is a recorded write.
type FakeUpdate struct {
	ID      int
	Patches []model.Patch
	Parent  int
}

// NewFake returns a Fake pre-populated with a small realistic backlog.
func NewFake() *Fake {
	f := &Fake{me: "Sveinung Øverland", items: map[int]*model.WorkItem{}, comments: map[int][]model.Comment{}, Latency: 150 * time.Millisecond}
	now := time.Now()
	monday := now.AddDate(0, 0, -int(now.Weekday())+1)
	sprint := func(n int, offset int) model.Iteration {
		start := monday.AddDate(0, 0, 14*offset)
		tf := "future"
		if offset < 0 {
			tf = "past"
		} else if offset == 0 {
			tf = "current"
		}
		return model.Iteration{
			ID: fmt.Sprintf("it-%d", n), Name: fmt.Sprintf("Sprint %d", n),
			Path: fmt.Sprintf("Platform\\Sprint %d", n), Start: start, Finish: start.AddDate(0, 0, 13), Timeframe: tf,
		}
	}
	f.iters = []model.Iteration{sprint(40, -2), sprint(41, -1), sprint(42, 0), sprint(43, 1), sprint(44, 2)}
	cur := f.iters[2].Path
	next := f.iters[3].Path
	backlog := "Platform"

	add := func(id, parent int, typ, title, state, who, iter string, effort float64, prio int) {
		var remaining float64
		if KindOf(typ) == model.KindTask && state != "Done" {
			remaining = float64(2 + id%5)
		}
		_, unique := resolvePerson(who)
		f.items[id] = &model.WorkItem{
			RemainingWork: remaining, AssignedToUnique: unique,
			ID: id, Rev: 1, Type: typ, Kind: KindOf(typ), Title: title, State: state, AssignedTo: who,
			IterationPath: iter, AreaPath: "Platform", Effort: effort, Priority: prio, ParentID: parent,
			BoardColumn: state, ChangedDate: now.Add(-time.Duration(id) * time.Hour), ChangedBy: "Alex Kim",
			Description: demoDescription(id, title),
			URL:         fmt.Sprintf("https://dev.azure.com/contoso/Platform/_workitems/edit/%d", id),
		}
		if cs := demoComments(id, now); len(cs) > 0 {
			f.comments[id] = cs
		}
	}
	add(1001, 0, "Epic", "Self-service onboarding", "In Progress", "Alex Kim", backlog, 0, 1)
	add(1002, 1001, "Feature", "Invite flow", "In Progress", "Sveinung Øverland", cur, 0, 1)
	add(1003, 1002, "Product Backlog Item", "Send invite email with magic link", "Committed", "Sveinung Øverland", cur, 5, 1)
	add(1004, 1002, "Product Backlog Item", "Accept invite and create account", "Approved", "Priya Natarajan", cur, 8, 2)
	add(1005, 1002, "Product Backlog Item", "Expire invites after 7 days", "New", "", next, 3, 3)
	add(1006, 1001, "Feature", "Team workspace setup wizard", "New", "", backlog, 0, 2)
	add(1007, 1006, "Product Backlog Item", "Choose workspace template", "New", "", next, 5, 2)
	add(1010, 0, "Epic", "Observability", "In Progress", "Priya Natarajan", backlog, 0, 2)
	add(1011, 1010, "Feature", "Structured logging", "Done", "Alex Kim", f.iters[1].Path, 0, 1)
	add(1012, 1010, "Feature", "Request tracing", "In Progress", "Sveinung Øverland", cur, 0, 2)
	add(1013, 1012, "Product Backlog Item", "Propagate trace id through queue workers", "In Progress", "Sveinung Øverland", cur, 8, 1)
	add(1014, 1012, "Product Backlog Item", "Trace dashboard in Grafana", "Committed", "Alex Kim", cur, 3, 2)
	add(1015, 1013, "Task", "Add trace header to publisher", "To Do", "Sveinung Øverland", cur, 0, 0)
	add(1016, 1013, "Task", "Read trace header in consumer", "Done", "Sveinung Øverland", cur, 0, 0)
	add(1017, 1013, "Task", "Load test with tracing on", "To Do", "", cur, 0, 0)
	add(1018, 1014, "Task", "Wire Grafana datasource", "In Progress", "Alex Kim", cur, 0, 0)
	add(1019, 1014, "Task", "Build panel JSON", "Done", "Alex Kim", cur, 0, 0)
	add(1020, 1012, "Bug", "Spans lost when retry budget exhausted", "Approved", "Sveinung Øverland", cur, 2, 1)
	add(1021, 0, "Product Backlog Item", "Rotate signing keys quarterly", "New", "", cur, 3, 4)
	add(1022, 0, "Bug", "Login page flickers on Safari", "New", "Priya Natarajan", cur, 1, 2)
	// A teammate actively working under a PBI of mine, for the Dashboard's
	// active-subitem lanes; the lane shows regardless of who this is
	// assigned to.
	add(1023, 1003, "Task", "Wire up magic link email template", "In Progress", "Priya Natarajan", cur, 0, 0)
	// A bug filed under a different PBI of mine that otherwise has no
	// children, for the Dashboard's lanes to drop: it carries
	// requirement-level states, not task states, and shouldn't earn that
	// PBI a lane on its own.
	add(1024, 1020, "Bug", "Retry budget also drops the parent span id", "New", "Priya Natarajan", cur, 1, 2)
	// A sub-team owns part of the area tree.
	for _, id := range []int{1014, 1018, 1019, 1021, 1022} {
		f.items[id].AreaPath = "Platform\\Green"
	}
	f.nextID = 2000
	return f
}

// demoDescription returns Markdown so the renderer has something to show.
func demoDescription(id int, title string) string {
	switch id % 3 {
	case 0:
		return fmt.Sprintf(`## Goal

%s so that users get value **quickly** and *safely*.

### Acceptance criteria

- [ ] Happy path works end to end
- [ ] Errors are surfaced in the UI
- [x] Telemetry event emitted

See [the design doc](https://example.com/design) for details.`, title)
	case 1:
		return fmt.Sprintf(`%s.

> Context: raised during sprint review; customers hit this weekly.

Steps to reproduce:

1. Open the invite page
2. Click **Send**
3. Observe the log line:

`+"```"+`
level=error msg="token expired" id=%d
`+"```"+`

| Env | Reproduces |
|-----|------------|
| dev | yes |
| prod | sometimes |`, title, id)
	default:
		return fmt.Sprintf("%s.\n\nSmall change, no acceptance criteria beyond `go test ./...` passing.", title)
	}
}

// demoComments seeds a discussion thread for some items and leaves others
// empty, so both states show up in --demo mode and golden frames.
func demoComments(id int, now time.Time) []model.Comment {
	author := func(name string, ago time.Duration) (string, string, time.Time) {
		display, uniq := resolvePerson(name)
		return display, uniq, now.Add(-ago)
	}
	switch id % 3 {
	case 0:
		return nil
	case 1:
		var cs []model.Comment
		for i, c := range []struct {
			name string
			ago  time.Duration
			text string
		}{
			{"Alex Kim", 3 * 24 * time.Hour, "Started on this — will push a draft today."},
			{"Priya Natarajan", 2 * 24 * time.Hour, "Nice, let me know if you want a second pair of eyes on the design."},
			{"Sveinung Øverland", time.Hour, "Draft is up, PTAL @Priya Natarajan."},
		} {
			a, u, when := author(c.name, c.ago)
			cs = append(cs, model.Comment{ID: id*100 + i + 1, Author: a, AuthorUnique: u, Text: c.text, CreatedDate: when, ModifiedDate: when})
		}
		return cs
	default:
		a, u, when := author("Priya Natarajan", 30*time.Minute)
		return []model.Comment{{ID: id*100 + 1, Author: a, AuthorUnique: u, Text: "Can we get an estimate on this by Friday?", CreatedDate: when, ModifiedDate: when}}
	}
}

// KindOf maps a work item type name to a Kind.
func KindOf(typ string) model.Kind {
	switch strings.ToLower(typ) {
	case "epic":
		return model.KindEpic
	case "feature":
		return model.KindFeature
	case "product backlog item", "user story", "requirement", "issue":
		return model.KindRequirement
	case "task":
		return model.KindTask
	case "bug":
		return model.KindBug
	default:
		return model.KindOther
	}
}

func (f *Fake) wait(ctx context.Context) error {
	if f.Latency > 0 {
		select {
		case <-time.After(f.Latency):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FailNext != nil {
		err := f.FailNext
		f.FailNext = nil
		return err
	}
	return nil
}

func (f *Fake) Me(ctx context.Context) (string, error) { return f.me, f.wait(ctx) }

func (f *Fake) Projects(ctx context.Context) ([]model.Project, error) {
	return []model.Project{{ID: "p1", Name: "Platform"}, {ID: "p2", Name: "Mobile"}}, f.wait(ctx)
}

func (f *Fake) Teams(ctx context.Context, project string) ([]model.Team, error) {
	return []model.Team{{ID: "t1", Name: "Team Blue"}, {ID: "t2", Name: "Team Green"}}, f.wait(ctx)
}

func (f *Fake) Iterations(ctx context.Context, project, team string) ([]model.Iteration, error) {
	return append([]model.Iteration(nil), f.iters...), f.wait(ctx)
}

func (f *Fake) Boards(ctx context.Context, project, team string) ([]model.Board, error) {
	return []model.Board{{ID: "b1", Name: "Backlog items", Columns: []model.BoardColumn{
		{Name: "New", States: []string{"New"}},
		{Name: "Approved", States: []string{"Approved"}},
		{Name: "Committed", States: []string{"Committed"}},
		{Name: "In Progress", States: []string{"In Progress"}},
		{Name: "Done", States: []string{"Done"}},
	}}}, f.wait(ctx)
}

func (f *Fake) BacklogConfig(ctx context.Context, project, team string) (model.BacklogConfig, error) {
	return model.BacklogConfig{
		RequirementType: "Product Backlog Item", TaskType: "Task", FeatureType: "Feature", EpicType: "Epic",
		BugsBehavior: "asRequirements",
	}, f.wait(ctx)
}

func (f *Fake) TeamAreas(ctx context.Context, project, team string) ([]model.TeamArea, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	switch team {
	case "Team Green":
		return []model.TeamArea{{Path: "Platform\\Green", IncludeChildren: true}}, nil
	default:
		return []model.TeamArea{{Path: "Platform", IncludeChildren: true}}, nil
	}
}

func (f *Fake) Create(ctx context.Context, project string, n model.NewItem) (*model.WorkItem, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.TrimSpace(n.Title) == "" {
		return nil, fmt.Errorf("title is required")
	}
	id := f.nextID
	f.nextID++
	state := "New"
	if KindOf(n.Type) == model.KindTask {
		state = "To Do"
	}
	it := &model.WorkItem{
		ID: id, Rev: 1, Type: n.Type, Kind: KindOf(n.Type), Title: n.Title, State: state, BoardColumn: state,
		IterationPath: n.IterationPath, AreaPath: n.AreaPath, ParentID: n.ParentID,
		ChangedDate: time.Now(), ChangedBy: f.me,
		URL: fmt.Sprintf("https://dev.azure.com/contoso/Platform/_workitems/edit/%d", id),
	}
	patches := []model.Patch{{Field: model.FieldTitle, Value: n.Title}, {Field: model.FieldWorkItemType, Value: n.Type}}
	if n.AssignedTo != "" {
		it.AssignedTo, it.AssignedToUnique = resolvePerson(n.AssignedTo)
		patches = append(patches, model.Patch{Field: model.FieldAssignedTo, Value: n.AssignedTo})
	}
	f.items[id] = it
	f.Updates = append(f.Updates, FakeUpdate{ID: id, Parent: n.ParentID, Patches: patches})
	c := *it
	return &c, nil
}

func (f *Fake) States(ctx context.Context, project, typ string) ([]string, error) {
	switch KindOf(typ) {
	case model.KindTask:
		return []string{"To Do", "In Progress", "Done"}, nil
	case model.KindEpic, model.KindFeature:
		return []string{"New", "In Progress", "Done", "Removed"}, nil
	default:
		return []string{"New", "Approved", "Committed", "In Progress", "Done", "Removed"}, nil
	}
}

// people is the demo directory. Only the first four are on a team; the
// rest stand in for people found by searching the organisation.
var fakePeople = []model.Person{
	{DisplayName: "Sveinung Øverland", UniqueName: "sveinung@contoso.com"},
	{DisplayName: "Alex Kim", UniqueName: "alex.kim@contoso.com"},
	{DisplayName: "Priya Natarajan", UniqueName: "priya@contoso.com"},
	{DisplayName: "Jordan Lee", UniqueName: "jordan.lee@contoso.com"},
	{DisplayName: "Ada Lovelace", UniqueName: "ada@contoso.com"},
	{DisplayName: "Grace Hopper", UniqueName: "grace.hopper@contoso.com"},
	{DisplayName: "Kari Nordmann", UniqueName: "kari.nordmann@contoso.com"},
	{DisplayName: "Ola Nordmann", UniqueName: "ola.nordmann@contoso.com"},
	{DisplayName: "Sam Rivera", UniqueName: "sam.rivera@contoso.com"},
}

// resolvePerson maps a display name or sign-in address to both forms, the
// way the server echoes back a resolved identity.
func resolvePerson(v string) (name, unique string) {
	for _, p := range fakePeople {
		if strings.EqualFold(p.UniqueName, v) || strings.EqualFold(p.DisplayName, v) {
			return p.DisplayName, p.UniqueName
		}
	}
	return v, ""
}

func (f *Fake) People(ctx context.Context, project, query string) ([]model.Person, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	if query == "" {
		return matchPeople(fakePeople[:4], ""), nil // the project's teams
	}
	return matchPeople(fakePeople, query), nil // plus organisation search
}

func (f *Fake) snapshot(filter func(*model.WorkItem) bool) []*model.WorkItem {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*model.WorkItem
	for _, it := range f.items {
		if filter(it) {
			c := *it
			out = append(out, &c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (f *Fake) SprintItems(ctx context.Context, project, team, iter string) ([]*model.WorkItem, []*model.WorkItem, error) {
	if err := f.wait(ctx); err != nil {
		return nil, nil, err
	}
	items := f.snapshot(func(w *model.WorkItem) bool { return w.IterationPath == iter })
	in := map[int]bool{}
	for _, it := range items {
		in[it.ID] = true
	}
	// Walk up to collect external ancestors.
	var external []*model.WorkItem
	seen := map[int]bool{}
	f.mu.Lock()
	for _, it := range items {
		p := it.ParentID
		for p != 0 && !in[p] && !seen[p] {
			seen[p] = true
			if par, ok := f.items[p]; ok {
				c := *par
				external = append(external, &c)
				p = par.ParentID
			} else {
				break
			}
		}
	}
	f.mu.Unlock()
	return items, external, nil
}

func (f *Fake) Backlog(ctx context.Context, project, team string) ([]*model.WorkItem, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	return f.snapshot(func(w *model.WorkItem) bool { return w.Kind != model.KindTask }), nil
}

func (f *Fake) MyItems(ctx context.Context, project string) ([]*model.WorkItem, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	return f.snapshot(func(w *model.WorkItem) bool {
		return w.AssignedTo == f.me && w.State != "Done" && w.State != "Removed"
	}), nil
}

func (f *Fake) Parents(ctx context.Context, project string) ([]*model.WorkItem, error) {
	return f.snapshot(func(w *model.WorkItem) bool { return w.Kind == model.KindEpic || w.Kind == model.KindFeature }), nil
}

func (f *Fake) Children(ctx context.Context, project string, parentID int) ([]*model.WorkItem, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	return f.snapshot(func(w *model.WorkItem) bool { return w.ParentID == parentID && w.State != "Removed" }), nil
}

func (f *Fake) ChildrenOf(ctx context.Context, project string, parentIDs []int) (map[int][]*model.WorkItem, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	want := map[int]bool{}
	for _, id := range parentIDs {
		want[id] = true
	}
	out := map[int][]*model.WorkItem{}
	for _, it := range f.snapshot(func(w *model.WorkItem) bool { return want[w.ParentID] && w.State != "Removed" }) {
		out[it.ParentID] = append(out[it.ParentID], it)
	}
	return out, nil
}

// Comments paginates f.comments[id] (oldest first) from the newest end
// backwards, commentPageSize at a time, so demo mode and tests can exercise
// the fetch-more path.
func (f *Fake) Comments(ctx context.Context, project string, id int, token string) ([]model.Comment, string, error) {
	if err := f.wait(ctx); err != nil {
		return nil, "", err
	}
	f.mu.Lock()
	all := f.comments[id]
	f.mu.Unlock()

	end := len(all)
	if token != "" {
		if n, err := strconv.Atoi(token); err == nil && n >= 0 && n <= len(all) {
			end = n
		}
	}
	start := max(end-commentPageSize, 0)
	page := append([]model.Comment(nil), all[start:end]...)
	next := ""
	if start > 0 {
		next = strconv.Itoa(start)
	}
	return page, next, nil
}

func (f *Fake) Get(ctx context.Context, id int) (*model.WorkItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	it, ok := f.items[id]
	if !ok {
		return nil, fmt.Errorf("work item %d not found", id)
	}
	c := *it
	return &c, nil
}

func (f *Fake) Update(ctx context.Context, id, rev int, patches []model.Patch) (*model.WorkItem, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	it, ok := f.items[id]
	if !ok {
		return nil, fmt.Errorf("work item %d not found", id)
	}
	if it.Rev != rev {
		return nil, fmt.Errorf("work item %d changed on server (rev %d, you had %d)", id, it.Rev, rev)
	}
	for _, p := range patches {
		switch p.Field {
		case model.FieldTitle:
			it.Title = p.Value.(string)
		case model.FieldState:
			it.State = p.Value.(string)
			it.BoardColumn = it.State
		case model.FieldAssignedTo:
			// Azure DevOps resolves a sign-in address to the display name.
			it.AssignedTo, it.AssignedToUnique = resolvePerson(p.Value.(string))
		case model.FieldIterationPath:
			it.IterationPath = p.Value.(string)
		case model.FieldDescription:
			it.Description = p.Value.(string)
		case model.FieldEffort, model.FieldStoryPoints:
			it.Effort = p.Value.(float64)
		case model.FieldRemainingWork:
			it.RemainingWork = p.Value.(float64)
		case model.FieldPriority:
			it.Priority = p.Value.(int)
		}
	}
	it.Rev++
	it.ChangedDate = time.Now()
	it.ChangedBy = f.me
	f.Updates = append(f.Updates, FakeUpdate{ID: id, Patches: patches})
	c := *it
	return &c, nil
}

func (f *Fake) SetParent(ctx context.Context, id, parentID int) (*model.WorkItem, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	it, ok := f.items[id]
	if !ok {
		return nil, fmt.Errorf("work item %d not found", id)
	}
	it.ParentID = parentID
	it.Rev++
	f.Updates = append(f.Updates, FakeUpdate{ID: id, Parent: parentID})
	c := *it
	return &c, nil
}

var _ Client = (*Fake)(nil)
