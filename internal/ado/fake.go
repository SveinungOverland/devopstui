package ado

import (
	"context"
	"fmt"
	"sort"
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
	comments map[int][]model.Comment
	links    []fakeLink
	history  []model.Change // what happened to the items, for Changes
	iters    []model.Iteration
	nextID   int
	Updates  []FakeUpdate // recorded writes, for tests
	Latency  time.Duration
	FailNext error
	// FailOn fails every call of the named method (Teams, TeamAreas,
	// Boards) until cleared, where FailNext fails whichever call is next.
	FailOn map[string]error
	// Calls counts calls per method, for tests that assert what a flow
	// did or did not fetch.
	Calls map[string]int
}

// fakeLink is a work item link as stored on from; Links reads it from
// either end.
type fakeLink struct {
	from, to int
	kind     model.RelationKind
}

// FakeUpdate is a recorded write.
type FakeUpdate struct {
	ID      int
	Patches []model.Patch
	Parent  int
}

// NewFake returns a Fake pre-populated with a small realistic backlog.
func NewFake() *Fake {
	f := &Fake{me: "Sveinung Øverland", items: map[int]*model.WorkItem{}, Latency: 150 * time.Millisecond}
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
		it := &model.WorkItem{
			RemainingWork: remaining, AssignedToUnique: unique,
			ID: id, Rev: 1, Type: typ, Kind: KindOf(typ), Title: title, State: state, AssignedTo: who,
			IterationPath: iter, AreaPath: "Platform", Effort: effort, Priority: prio, ParentID: parent,
			BoardColumn: state, ChangedDate: now.Add(-time.Duration(id%24) * time.Hour), ChangedBy: "Alex Kim",
			URL: fmt.Sprintf("https://dev.azure.com/contoso/Platform/_workitems/edit/%d", id), Project: "Platform",
		}
		// Real Azure DevOps Bugs carry Repro Steps/Acceptance Criteria instead
		// of a Description, so the demo mirrors that split.
		if KindOf(typ) == model.KindBug {
			it.ReproSteps = demoReproSteps(id, title)
			it.AcceptanceCriteria = demoAcceptanceCriteria(title)
		} else {
			it.Description = demoDescription(id, title)
		}
		f.items[id] = it
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
	// A PBI of mine that hasn't been started yet but still has a stray
	// child task, for the Dashboard's lanes to drop: only PBIs/Bugs
	// currently being worked on earn a lane, regardless of their children.
	add(1025, 1002, "Product Backlog Item", "Resend invite with a fresh link", "New", "Sveinung Øverland", cur, 2, 4)
	add(1026, 1025, "Task", "Add resend button to invite page", "To Do", "Sveinung Øverland", cur, 0, 0)
	// A PBI parked at the backlog root, not yet pulled into a sprint, for the
	// "Unscheduled" iteration entry to show.
	add(1027, 1006, "Product Backlog Item", "Add workspace naming validation", "New", "", backlog, 3, 3)
	// A Done PBI of mine, for the Dashboard's "show closed" toggle: hidden
	// from the top kanban by default, shown once 'c' is pressed.
	add(1028, 1002, "Product Backlog Item", "Add SSO login button to sign-in page", "Done", "Sveinung Øverland", cur, 3, 2)
	// A task under a PBI of mine that is planned for the next sprint, for
	// the Dashboard's lanes to dim. Unassigned, so it stays out of MyItems
	// and the kanban's progress badges.
	add(1029, 1003, "Task", "Localise the invite email", "To Do", "", next, 0, 0)
	// A task that has sat In Progress for over a week, for the stale flag.
	f.items[1018].ChangedDate = now.AddDate(0, 0, -8)
	// A sub-team owns part of the area tree.
	for _, id := range []int{1014, 1018, 1019, 1021, 1022} {
		f.items[id].AreaPath = "Platform\\Green"
	}
	f.comments = map[int][]model.Comment{
		1003: demoComments(now, "Priya Natarajan", "Should the magic link expire after first use, or stay valid for the full 7 days?",
			"Sveinung Øverland", "First use only — otherwise a leaked email thread keeps working forever.\n\nWorth calling out in the acceptance criteria."),
		1013: demoComments(now, "Alex Kim", "Publisher change is in review, consumer side still needs the header read added.",
			"Sveinung Øverland", "Picking this up now that #1015 landed."),
	}
	// A feature and its task in another project, reachable only through a
	// link, so the drill-down has to load them against their own project.
	for _, it := range []*model.WorkItem{
		{ID: 3001, Type: "Feature", Title: "Single sign-on via Entra ID", State: "In Progress", AssignedTo: "Alex Kim"},
		{ID: 3002, ParentID: 3001, Type: "Task", Title: "Register the app in Entra ID", State: "To Do", RemainingWork: 3},
	} {
		it.Rev, it.Kind, it.BoardColumn, it.Project = 1, KindOf(it.Type), it.State, "Identity"
		it.AreaPath, it.IterationPath = "Identity", "Identity\\Sprint 7"
		it.ChangedDate, it.ChangedBy = now.Add(-time.Duration(it.ID-3000)*time.Hour), "Alex Kim"
		it.Description = demoDescription(it.ID, it.Title)
		it.URL = fmt.Sprintf("https://dev.azure.com/contoso/Identity/_workitems/edit/%d", it.ID)
		f.items[it.ID] = it
	}
	f.comments[3001] = demoComments(now, "Alex Kim", "Tenant admin consent is approved.", "Priya Natarajan", "Great, the invite flow can link to this.")

	// Links for the Related section: the trace work is related to the bug
	// it fixes, the dashboard waits on it, and within it the consumer task
	// waits on the publisher one. The SSO button relates across projects.
	f.links = []fakeLink{
		{1028, 3001, model.RelRelated},
		{1013, 1020, model.RelRelated},
		{1013, 1014, model.RelSuccessor},
		{1015, 1016, model.RelSuccessor},
		{1003, 1004, model.RelSuccessor},
		{1022, 1020, model.RelRelated},
	}
	f.nextID = 2000
	f.seedHistory(now)
	return f
}

// seedHistory gives the demo a couple of weeks of activity, so the Changes
// view has something to show for every period. It is written to agree with
// the items above: each one's last recorded state is the state it is in.
func (f *Fake) seedHistory(now time.Time) {
	const sv, alex, priya = "Sveinung Øverland", "Alex Kim", "Priya Natarajan"
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	const day = 24 * time.Hour
	state := func(from, to string) model.FieldChange {
		return model.FieldChange{Kind: model.ChangeState, Label: "State", Old: from, New: to}
	}
	assign := func(from, to string) model.FieldChange {
		return model.FieldChange{Kind: model.ChangeAssigned, Label: "Assigned", Old: from, New: to}
	}
	comment := func(text string) model.FieldChange {
		return model.FieldChange{Kind: model.ChangeComment, Label: "Comment", New: text}
	}
	created := model.FieldChange{Kind: model.ChangeCreated, Label: "Created"}
	edited := func(label string) model.FieldChange { return model.FieldChange{Kind: model.ChangeEdit, Label: label} }
	add := func(id int, at time.Time, by string, fields ...model.FieldChange) {
		f.history = append(f.history, model.Change{ItemID: id, At: at, By: by, Fields: fields})
	}
	// The last few hours.
	add(1023, ago(25*time.Minute), priya, state("To Do", "In Progress"))
	add(1013, ago(70*time.Minute), sv, comment("Picking this up now that #1015 landed."))
	add(1016, ago(2*time.Hour), sv, state("In Progress", "Done"),
		model.FieldChange{Kind: model.ChangeEdit, Label: "Remaining", Old: "3", New: ""})
	add(1024, ago(3*time.Hour), priya, created, assign("", priya))
	add(1003, ago(4*time.Hour), sv, comment("First use only — otherwise a leaked email thread keeps working forever."))
	add(1020, ago(5*time.Hour), alex, state("New", "Approved"),
		model.FieldChange{Kind: model.ChangeEdit, Label: "Priority", Old: "2", New: "1"})
	// The day before, and the rest of the week.
	add(1013, ago(day+time.Hour), alex, comment("Publisher change is in review, consumer side still needs the header read added."))
	add(1019, ago(day+2*time.Hour), alex, state("In Progress", "Done"))
	add(1003, ago(day+3*time.Hour), priya, comment("Should the magic link expire after first use, or stay valid for the full 7 days?"))
	add(1014, ago(day+5*time.Hour), alex, state("Approved", "Committed"),
		model.FieldChange{Kind: model.ChangeEdit, Label: "Effort", Old: "5", New: "3"})
	add(1028, ago(day+6*time.Hour), sv, state("In Progress", "Done"),
		model.FieldChange{Kind: model.ChangeLink, Label: "Pull Request"})
	add(1022, ago(2*day+2*time.Hour), alex, assign("", priya))
	add(1025, ago(2*day+4*time.Hour), sv, created, assign("", sv))
	add(1026, ago(2*day+4*time.Hour-time.Minute), sv, created, assign("", sv))
	add(1013, ago(3*day), sv, state("Committed", "In Progress"))
	add(1021, ago(3*day+3*time.Hour), alex,
		model.FieldChange{Kind: model.ChangeIteration, Label: "Iteration", Old: "Platform\\Sprint 41", New: "Platform\\Sprint 42"})
	add(1003, ago(4*day), sv, state("Approved", "Committed"), edited("Description"))
	add(1004, ago(5*day), priya, state("New", "Approved"),
		model.FieldChange{Kind: model.ChangeEdit, Label: "Title", Old: "Accept invite", New: "Accept invite and create account"})
	add(1018, ago(8*day), alex, state("To Do", "In Progress"))
	add(1012, ago(9*day), sv, state("New", "In Progress"))
	add(1022, ago(10*day), priya, created)
	// Outside the sprint: a feature finished in the previous one, and
	// something parked on the backlog.
	add(1011, ago(6*day), alex, state("In Progress", "Done"))
	add(1027, ago(day+4*time.Hour), alex, created)
	sortChanges(f.history)
}

// record adds a change made through the fake to its history, so the
// Changes view shows what a session itself did. The caller holds f.mu.
func (f *Fake) record(id int, fields ...model.FieldChange) {
	if len(fields) == 0 {
		return
	}
	f.history = append([]model.Change{{ItemID: id, At: time.Now(), By: f.me, Fields: fields}}, f.history...)
}

func (f *Fake) Changes(ctx context.Context, project string, q model.ChangeQuery) (model.ChangeSet, error) {
	_ = f.called("Changes")
	if err := f.wait(ctx); err != nil {
		return model.ChangeSet{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var set model.ChangeSet
	seen := map[int]bool{}
	for _, c := range f.history {
		it, ok := f.items[c.ItemID]
		if !ok || !inProject(it, project) || !q.Covers(c.At) {
			continue
		}
		if q.IterationPath != "" && it.IterationPath != q.IterationPath && !strings.HasPrefix(it.IterationPath, q.IterationPath+"\\") {
			continue
		}
		if len(q.Areas) > 0 && !model.InAreas(it.AreaPath, q.Areas) {
			continue
		}
		set.Changes = append(set.Changes, c)
		if !seen[it.ID] {
			seen[it.ID] = true
			cp := *it
			set.Items = append(set.Items, &cp)
		}
	}
	sortChanges(set.Changes)
	return set, nil
}

// demoComments builds a short back-and-forth discussion, newest last, so
// --demo and the shots have something real to show.
func demoComments(now time.Time, a1, t1, a2, t2 string) []model.Comment {
	return []model.Comment{
		{ID: 1, Author: a1, CreatedDate: now.Add(-48 * time.Hour), Text: t1},
		{ID: 2, Author: a2, CreatedDate: now.Add(-24 * time.Hour), Text: t2},
	}
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

// demoReproSteps returns Markdown for a Bug's Repro Steps field.
func demoReproSteps(id int, title string) string {
	return fmt.Sprintf(`%s.

1. Open the affected page
2. Reproduce the steps below
3. Observe the failure

`+"```"+`
level=error msg="unexpected state" id=%d
`+"```", title, id)
}

// demoAcceptanceCriteria returns Markdown for a Bug's Acceptance Criteria
// field.
func demoAcceptanceCriteria(title string) string {
	return fmt.Sprintf(`- [ ] %s no longer reproduces
- [ ] Regression test added
- [ ] Verified in the affected environment`, title)
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

// called counts a call of op and returns the failure set for it, if any.
func (f *Fake) called(op string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Calls == nil {
		f.Calls = map[string]int{}
	}
	f.Calls[op]++
	return f.FailOn[op]
}

func (f *Fake) Me(ctx context.Context) (string, error) { return f.me, f.wait(ctx) }

func (f *Fake) Projects(ctx context.Context) ([]model.Project, error) {
	return []model.Project{{ID: "p1", Name: "Platform"}, {ID: "p2", Name: "Mobile"}}, f.wait(ctx)
}

func (f *Fake) Teams(ctx context.Context, project string) ([]model.Team, error) {
	if err := f.called("Teams"); err != nil {
		return nil, err
	}
	return []model.Team{{ID: "t1", Name: "Team Blue"}, {ID: "t2", Name: "Team Green"}}, f.wait(ctx)
}

func (f *Fake) Iterations(ctx context.Context, project, team string) ([]model.Iteration, error) {
	return append([]model.Iteration(nil), f.iters...), f.wait(ctx)
}

// ProjectIterations returns the team's sprints plus a couple of iterations
// outside the team's picked list, so --demo and tests can show the wider
// project tree the move/edit pickers offer: a sprint nested under a release
// train, and a sibling team's sprint.
func (f *Fake) ProjectIterations(ctx context.Context, project string) ([]model.Iteration, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	now := time.Now()
	monday := now.AddDate(0, 0, -int(now.Weekday())+1)
	out := append([]model.Iteration(nil), f.iters...)
	out = append(out,
		model.Iteration{ID: "it-45", Name: "Sprint 45", Path: "Platform\\Release 1\\Sprint 45", Start: monday.AddDate(0, 0, 28), Finish: monday.AddDate(0, 0, 41)},
		model.Iteration{ID: "it-green-1", Name: "Green Sprint 1", Path: "Platform\\Green\\Sprint 1", Start: monday, Finish: monday.AddDate(0, 0, 13)},
	)
	return out, nil
}

func (f *Fake) Boards(ctx context.Context, project, team string) ([]model.Board, error) {
	if err := f.called("Boards"); err != nil {
		return nil, err
	}
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
	if err := f.called("TeamAreas"); err != nil {
		return nil, err
	}
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
		URL:     fmt.Sprintf("https://dev.azure.com/contoso/%s/_workitems/edit/%d", project, id),
		Project: project,
	}
	patches := []model.Patch{{Field: model.FieldTitle, Value: n.Title}, {Field: model.FieldWorkItemType, Value: n.Type}}
	if n.AssignedTo != "" {
		it.AssignedTo, it.AssignedToUnique = resolvePerson(n.AssignedTo)
		patches = append(patches, model.Patch{Field: model.FieldAssignedTo, Value: n.AssignedTo})
	}
	f.items[id] = it
	rec := []model.FieldChange{{Kind: model.ChangeCreated, Label: "Created"}}
	if it.AssignedTo != "" {
		rec = append(rec, model.FieldChange{Kind: model.ChangeAssigned, Label: "Assigned", New: it.AssignedTo})
	}
	f.record(id, rec...)
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
	return items, f.externalParents(items), nil
}

// externalParents walks up from items and returns the ancestors that are
// not among them.
func (f *Fake) externalParents(items []*model.WorkItem) []*model.WorkItem {
	in := map[int]bool{}
	for _, it := range items {
		in[it.ID] = true
	}
	var external []*model.WorkItem
	seen := map[int]bool{}
	f.mu.Lock()
	defer f.mu.Unlock()
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
	return external
}

// Unscheduled reuses SprintItems' exact-match filter with the project root
// itself as the "iteration" - the fake never nests sprints under
// sub-iterations, so it already means "not assigned to any sprint".
func (f *Fake) Unscheduled(ctx context.Context, project, team string) ([]*model.WorkItem, []*model.WorkItem, error) {
	return f.SprintItems(ctx, project, team, project)
}

func (f *Fake) Backlog(ctx context.Context, project, team string, areas []model.TeamArea) ([]*model.WorkItem, []*model.WorkItem, error) {
	_ = f.called("Backlog")
	if err := f.wait(ctx); err != nil {
		return nil, nil, err
	}
	items := f.snapshot(func(w *model.WorkItem) bool {
		return inProject(w, project) && w.Kind != model.KindTask && (len(areas) == 0 || model.InAreas(w.AreaPath, areas))
	})
	if len(areas) == 0 {
		return items, nil, nil
	}
	return items, f.externalParents(items), nil
}

func (f *Fake) MyItems(ctx context.Context, project string) ([]*model.WorkItem, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	return f.snapshot(func(w *model.WorkItem) bool {
		return inProject(w, project) && w.AssignedTo == f.me && w.State != "Removed"
	}), nil
}

func (f *Fake) Parents(ctx context.Context, project string) ([]*model.WorkItem, error) {
	return f.snapshot(func(w *model.WorkItem) bool {
		return inProject(w, project) && (w.Kind == model.KindEpic || w.Kind == model.KindFeature)
	}), nil
}

// inProject mirrors the SDK's project-scoped routes and WIQL, which never
// return another project's items.
func inProject(w *model.WorkItem, project string) bool {
	return project == "" || w.Project == project
}

func (f *Fake) Children(ctx context.Context, project string, parentID int) ([]*model.WorkItem, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	return f.snapshot(func(w *model.WorkItem) bool {
		return inProject(w, project) && w.ParentID == parentID && w.State != "Removed"
	}), nil
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
	for _, it := range f.snapshot(func(w *model.WorkItem) bool { return inProject(w, project) && want[w.ParentID] && w.State != "Removed" }) {
		out[it.ParentID] = append(out[it.ParentID], it)
	}
	return out, nil
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

func (f *Fake) Items(ctx context.Context, ids []int) ([]*model.WorkItem, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*model.WorkItem
	for _, id := range ids {
		if it, ok := f.items[id]; ok {
			c := *it
			out = append(out, &c)
		}
	}
	return out, nil
}

func (f *Fake) Links(ctx context.Context, id int) ([]model.RelatedItem, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.RelatedItem
	add := func(kind model.RelationKind, target int) {
		if it, ok := f.items[target]; ok {
			c := *it
			out = append(out, model.RelatedItem{Kind: kind, Item: &c})
		}
	}
	for _, l := range f.links {
		switch id {
		case l.from:
			add(l.kind, l.to)
		case l.to:
			add(inverse(l.kind), l.from)
		}
	}
	model.SortRelated(out)
	return out, nil
}

// inverse is the same link read from its other end.
func inverse(k model.RelationKind) model.RelationKind {
	switch k {
	case model.RelSuccessor:
		return model.RelPredecessor
	case model.RelPredecessor:
		return model.RelSuccessor
	case model.RelDuplicate:
		return model.RelDuplicateOf
	case model.RelDuplicateOf:
		return model.RelDuplicate
	}
	return k
}

func (f *Fake) Comments(ctx context.Context, project string, id int) ([]model.Comment, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if it, ok := f.items[id]; ok && !inProject(it, project) {
		return nil, fmt.Errorf("work item %d not found in project %s", id, project)
	}
	return append([]model.Comment(nil), f.comments[id]...), nil
}

func (f *Fake) AddComment(ctx context.Context, project string, id int, text string) (model.Comment, error) {
	if err := f.wait(ctx); err != nil {
		return model.Comment{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if it, ok := f.items[id]; !ok || !inProject(it, project) {
		return model.Comment{}, fmt.Errorf("work item %d not found in project %s", id, project)
	}
	c := model.Comment{ID: len(f.comments[id]) + 1, Author: f.me, CreatedDate: time.Now(), Text: text}
	f.comments[id] = append(f.comments[id], c)
	f.record(id, model.FieldChange{Kind: model.ChangeComment, Label: "Comment", New: text})
	return c, nil
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
	var rec []model.FieldChange
	note := func(kind model.ChangeKind, label, from, to string) {
		if from != to {
			rec = append(rec, model.FieldChange{Kind: kind, Label: label, Old: from, New: to})
		}
	}
	num := func(v float64) string {
		if v == 0 {
			return ""
		}
		return fmt.Sprintf("%g", v)
	}
	for _, p := range patches {
		switch p.Field {
		case model.FieldTitle:
			note(model.ChangeEdit, "Title", it.Title, p.Value.(string))
			it.Title = p.Value.(string)
		case model.FieldState:
			note(model.ChangeState, "State", it.State, p.Value.(string))
			it.State = p.Value.(string)
			it.BoardColumn = it.State
		case model.FieldAssignedTo:
			// Azure DevOps resolves a sign-in address to the display name.
			was := it.AssignedTo
			it.AssignedTo, it.AssignedToUnique = resolvePerson(p.Value.(string))
			note(model.ChangeAssigned, "Assigned", was, it.AssignedTo)
		case model.FieldIterationPath:
			note(model.ChangeIteration, "Iteration", it.IterationPath, p.Value.(string))
			it.IterationPath = p.Value.(string)
		case model.FieldDescription:
			rec = append(rec, model.FieldChange{Kind: model.ChangeEdit, Label: "Description"})
			it.Description = p.Value.(string)
		case model.FieldReproSteps:
			rec = append(rec, model.FieldChange{Kind: model.ChangeEdit, Label: "Repro steps"})
			it.ReproSteps = p.Value.(string)
		case model.FieldAcceptanceCriteria:
			rec = append(rec, model.FieldChange{Kind: model.ChangeEdit, Label: "Acceptance criteria"})
			it.AcceptanceCriteria = p.Value.(string)
		case model.FieldEffort, model.FieldStoryPoints:
			note(model.ChangeEdit, "Effort", num(it.Effort), num(p.Value.(float64)))
			it.Effort = p.Value.(float64)
		case model.FieldRemainingWork:
			note(model.ChangeEdit, "Remaining", num(it.RemainingWork), num(p.Value.(float64)))
			it.RemainingWork = p.Value.(float64)
		case model.FieldPriority:
			note(model.ChangeEdit, "Priority", num(float64(it.Priority)), num(float64(p.Value.(int))))
			it.Priority = p.Value.(int)
		}
	}
	f.record(id, rec...)
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
	link := model.FieldChange{Kind: model.ChangeLink, Label: "Parent", Removed: parentID == 0}
	if it.ParentID != 0 {
		link.Old = fmt.Sprintf("#%d", it.ParentID)
	}
	if parentID != 0 {
		link.New = fmt.Sprintf("#%d", parentID)
	}
	f.record(id, link)
	it.ParentID = parentID
	it.Rev++
	f.Updates = append(f.Updates, FakeUpdate{ID: id, Parent: parentID})
	c := *it
	return &c, nil
}

var _ Client = (*Fake)(nil)
