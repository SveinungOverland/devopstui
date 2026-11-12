package ado

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/workitemtracking"

	"github.com/sveinungoverland/devopstui/internal/model"
)

type upd = map[string]workitemtracking.WorkItemFieldUpdate

func fu(old, new any) workitemtracking.WorkItemFieldUpdate {
	return workitemtracking.WorkItemFieldUpdate{OldValue: old, NewValue: new}
}

func person(name string) map[string]any { return map[string]any{"displayName": name} }

func texts(changes []model.Change) []string {
	var out []string
	for _, c := range changes {
		out = append(out, c.By+": "+c.Text())
	}
	return out
}

func TestConvertUpdates(t *testing.T) {
	never := azuredevops.Time{Time: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)}
	wi := func(id string) *string { return ptr("https://dev.azure.com/o/_apis/wit/workItems/" + id) }
	updates := []workitemtracking.WorkItemUpdate{
		{Rev: ptr(1), Fields: &upd{
			"System.CreatedDate":     fu(nil, "2026-03-02T08:00:00Z"),
			model.FieldChangedDate:   fu(nil, "2026-03-02T08:00:00Z"),
			model.FieldChangedBy:     fu(nil, person("Alex Kim")),
			model.FieldState:         fu(nil, "New"),
			model.FieldTitle:         fu(nil, "A title"),
			model.FieldAssignedTo:    fu(nil, person("Priya Natarajan")),
			model.FieldIterationPath: fu(nil, "P\\Sprint 1"),
		}},
		// Same author as the revision before: ChangedBy is not listed.
		{Rev: ptr(2), Fields: &upd{
			model.FieldChangedDate: fu("2026-03-02T08:00:00Z", "2026-03-03T09:30:00Z"),
			model.FieldState:       fu("New", "Committed"),
			model.FieldBoardColumn: fu("New", "Committed"),
			"System.Rev":           fu(1.0, 2.0),
		}},
		// Only bookkeeping (a reorder): nothing to show.
		{Rev: ptr(3), Fields: &upd{
			model.FieldChangedDate:                  fu("2026-03-03T09:30:00Z", "2026-03-03T10:00:00Z"),
			"Microsoft.VSTS.Common.BacklogPriority": fu(1.0, 2.0),
		}},
		{Rev: ptr(4), Fields: &upd{
			model.FieldChangedDate:   fu("2026-03-03T10:00:00Z", "2026-03-04T11:00:00Z"),
			model.FieldChangedBy:     fu(person("Alex Kim"), "Sveinung Øverland <sveinung@contoso.com>"),
			model.FieldAssignedTo:    fu(person("Priya Natarajan"), nil),
			model.FieldEffort:        fu(5.0, 3.0),
			model.FieldDescription:   fu("<p>a</p>", "<p>b</p>"),
			model.FieldIterationPath: fu("P\\Sprint 1", "P\\Sprint 2"),
			"System.History":         fu(nil, "<div>Moved to the   next sprint.</div>"),
			"Custom.Risk":            fu("Low", "High"),
		}},
		// A re-parent and a pull request, on the latest revision, whose
		// RevisedDate is the year 9999.
		{Rev: ptr(5), RevisedDate: &never,
			Fields: &upd{model.FieldChangedDate: fu("2026-03-04T11:00:00Z", "2026-03-05T12:00:00Z")},
			Relations: &workitemtracking.WorkItemRelationUpdates{
				Removed: &[]workitemtracking.WorkItemRelation{{Rel: ptr("System.LinkTypes.Hierarchy-Reverse"), Url: wi("10")}},
				Added: &[]workitemtracking.WorkItemRelation{
					{Rel: ptr("System.LinkTypes.Hierarchy-Reverse"), Url: wi("11")},
					{Rel: ptr("System.LinkTypes.Hierarchy-Forward"), Url: wi("12")},
					{Rel: ptr("ArtifactLink"), Url: ptr("vstfs:///Git/PullRequestId/x"), Attributes: &map[string]any{"name": "Pull Request"}},
				},
			}},
	}
	got := convertUpdates(42, updates)
	want := []string{
		"Alex Kim: created · Assigned — → Priya Natarajan",
		"Alex Kim: State New → Committed",
		"Sveinung Øverland: Assigned Priya Natarajan → — · Iteration P\\Sprint 1 → P\\Sprint 2 · Effort 5 → 3 · Description edited · Risk Low → High · comment: Moved to the next sprint.",
		"Sveinung Øverland: Parent #10 → #11 · Pull Request added",
	}
	if strings.Join(texts(got), "\n") != strings.Join(want, "\n") {
		t.Errorf("convertUpdates:\n%s\nwant:\n%s", strings.Join(texts(got), "\n"), strings.Join(want, "\n"))
	}
	if len(got) == 4 {
		if at := got[3].At; !at.Equal(time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)) {
			t.Errorf("latest change at %v: must come from ChangedDate, not the 9999 RevisedDate", at)
		}
		if got[1].Rev != 2 || got[1].ItemID != 42 {
			t.Errorf("change = %+v", got[1])
		}
	}
}

func TestChangesWiql(t *testing.T) {
	since := time.Date(2026, 3, 2, 0, 0, 0, 0, time.FixedZone("CET", 3600))
	q := model.ChangeQuery{Since: since, IterationPath: "P\\Sprint 1's",
		Areas: []model.TeamArea{{Path: "P\\Green", IncludeChildren: true}, {Path: "P\\Blue"}}}
	got := changesWiql(q)
	for _, want := range []string{
		"[System.ChangedDate] >= '2026-03-01T23:00:00Z'",
		"[System.IterationPath] UNDER 'P\\Sprint 1''s'",
		"([System.AreaPath] UNDER 'P\\Green' OR [System.AreaPath] = 'P\\Blue')",
		"ORDER BY [System.ChangedDate] DESC",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("wiql lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "CreatedDate") {
		t.Errorf("an open window needs no upper bound:\n%s", got)
	}
	q = model.ChangeQuery{Since: since, Until: since.AddDate(0, 0, 14)}
	got = changesWiql(q)
	if !strings.Contains(got, "[System.CreatedDate] < '2026-03-15T23:00:00Z'") || strings.Contains(got, "AreaPath") || strings.Contains(got, "IterationPath") {
		t.Errorf("closed, unscoped window:\n%s", got)
	}
}

func TestFakeChanges(t *testing.T) {
	f := NewFake()
	f.Latency = 0
	ctx := context.Background()
	now := time.Now()
	week, err := f.Changes(ctx, "Platform", model.ChangeQuery{Since: now.AddDate(0, 0, -7)})
	if err != nil || len(week.Changes) == 0 {
		t.Fatalf("Changes = %d, %v", len(week.Changes), err)
	}
	for i, c := range week.Changes {
		if i > 0 && c.At.After(week.Changes[i-1].At) {
			t.Fatalf("changes not newest first at %d", i)
		}
	}
	// Scoped to the sprint, the feature finished last sprint and the PBI
	// on the backlog drop out.
	sprint, _ := f.Changes(ctx, "Platform", model.ChangeQuery{Since: now.AddDate(0, 0, -7), IterationPath: "Platform\\Sprint 42"})
	if len(sprint.Changes) >= len(week.Changes) {
		t.Errorf("sprint scope kept %d of %d changes", len(sprint.Changes), len(week.Changes))
	}
	for _, it := range sprint.Items {
		if it.IterationPath != "Platform\\Sprint 42" {
			t.Errorf("#%d is in %s", it.ID, it.IterationPath)
		}
	}
	green, _ := f.Changes(ctx, "Platform", model.ChangeQuery{Since: now.AddDate(0, 0, -7),
		Areas: []model.TeamArea{{Path: "Platform\\Green", IncludeChildren: true}}})
	if len(green.Items) == 0 {
		t.Fatal("no changes in the Green area")
	}
	for _, it := range green.Items {
		if it.AreaPath != "Platform\\Green" {
			t.Errorf("#%d is in area %s", it.ID, it.AreaPath)
		}
	}
	// A write through the fake shows up as a change.
	if _, err := f.Update(ctx, 1004, 1, []model.Patch{{Field: model.FieldState, Value: "Committed"}}); err != nil {
		t.Fatal(err)
	}
	after, _ := f.Changes(ctx, "Platform", model.ChangeQuery{Since: now.Add(-time.Minute)})
	if len(after.Changes) != 1 || after.Changes[0].Text() != "State Approved → Committed" {
		t.Errorf("after a write: %v", texts(after.Changes))
	}
}

func TestFakeBacklogAreas(t *testing.T) {
	f := NewFake()
	f.Latency = 0
	items, external, err := f.Backlog(context.Background(), "Platform", "Team Blue",
		[]model.TeamArea{{Path: "Platform\\Green", IncludeChildren: true}})
	if err != nil || len(items) == 0 {
		t.Fatalf("Backlog = %d, %v", len(items), err)
	}
	in := map[int]bool{}
	for _, it := range items {
		in[it.ID] = true
		if it.AreaPath != "Platform\\Green" {
			t.Errorf("#%d is outside the area", it.ID)
		}
	}
	// 1014 hangs under the Request tracing feature, which is another
	// team's: it must come along as a parent, not as an item.
	found := false
	for _, e := range external {
		found = found || e.ID == 1012
		if in[e.ID] {
			t.Errorf("#%d is both an item and a parent", e.ID)
		}
	}
	if !found {
		t.Error("parent feature 1012 missing from the external set")
	}
}

func TestMemo(t *testing.T) {
	s := &SDK{meta: map[string]metaEntry{}}
	calls := 0
	load := func() ([]string, error) { calls++; return []string{"a"}, nil }
	for i := 0; i < 3; i++ {
		if v, err := memo(s, "k", load); err != nil || len(v) != 1 {
			t.Fatalf("memo = %v, %v", v, err)
		}
	}
	if calls != 1 {
		t.Errorf("loaded %d times, want 1", calls)
	}
	s.Forget()
	_, _ = memo(s, "k", load)
	if calls != 2 {
		t.Errorf("after Forget loaded %d times, want 2", calls)
	}
	// A failure is not cached.
	fails := 0
	bad := func() (int, error) { fails++; return 0, context.DeadlineExceeded }
	_, _ = memo(s, "bad", bad)
	_, _ = memo(s, "bad", bad)
	if fails != 2 {
		t.Errorf("a failed load was cached (%d calls)", fails)
	}
}
