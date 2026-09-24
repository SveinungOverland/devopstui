package ado

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/webapi"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/workitemtracking"

	"github.com/sveinungoverland/devopstui/internal/model"
)

func TestFieldOps_MarkdownDefault(t *testing.T) {
	s := &SDK{}
	ops := s.fieldOps(model.FieldDescription, "# Title\n\n- [ ] item")
	if len(ops) != 2 {
		t.Fatalf("want 2 ops, got %d", len(ops))
	}
	if *ops[0].Path != "/fields/System.Description" || ops[0].Value != "# Title\n\n- [ ] item" {
		t.Errorf("field op = %+v (markdown must be sent verbatim)", ops[0])
	}
	if *ops[1].Path != "/multilineFieldsFormat/System.Description" || ops[1].Value != "Markdown" {
		t.Errorf("format op = %+v", ops[1])
	}
}

func TestFieldOps_HTMLMode(t *testing.T) {
	s := &SDK{WriteHTML: true}
	ops := s.fieldOps(model.FieldDescription, "# Title")
	if len(ops) != 1 || !strings.Contains(ops[0].Value.(string), "<h1") {
		t.Errorf("html mode ops = %+v", ops)
	}
}

func TestFieldOps_OtherFieldsUntouched(t *testing.T) {
	s := &SDK{}
	ops := s.fieldOps(model.FieldTitle, "x")
	if len(ops) != 1 || *ops[0].Path != "/fields/System.Title" || ops[0].Value != "x" {
		t.Errorf("ops = %+v", ops)
	}
}

func TestFieldOps_ReproStepsAndAcceptanceCriteria(t *testing.T) {
	s := &SDK{}
	for _, field := range []string{model.FieldReproSteps, model.FieldAcceptanceCriteria} {
		ops := s.fieldOps(field, "steps")
		if len(ops) != 2 {
			t.Fatalf("%s: want 2 ops, got %d", field, len(ops))
		}
		if *ops[0].Path != "/fields/"+field || ops[0].Value != "steps" {
			t.Errorf("%s: field op = %+v", field, ops[0])
		}
		if *ops[1].Path != "/multilineFieldsFormat/"+field || ops[1].Value != "Markdown" {
			t.Errorf("%s: format op = %+v", field, ops[1])
		}
	}
}

// TestConvert_BugNarrativeFields covers Azure DevOps' Bug field layout: the
// narrative lives in Repro Steps / Acceptance Criteria, not Description
// (see issue #36), so a Bug with those fields set but no Description must
// convert into a WorkItem with ReproSteps/AcceptanceCriteria populated and
// Description empty.
func TestConvert_BugNarrativeFields(t *testing.T) {
	s := &SDK{orgURL: "https://dev.azure.com/contoso"}
	fields := map[string]any{
		model.FieldWorkItemType:       "Bug",
		model.FieldTitle:              "Login page flickers",
		model.FieldReproSteps:         "<p>Open the login page</p>",
		model.FieldAcceptanceCriteria: "<p>No more flicker</p>",
	}
	wi := &workitemtracking.WorkItem{Id: ptr(42), Rev: ptr(1), Fields: &fields}
	got := s.convert(wi, "Platform")
	if got.Description != "" {
		t.Errorf("Description = %q, want empty for a Bug with no System.Description", got.Description)
	}
	if got.ReproSteps != "Open the login page" {
		t.Errorf("ReproSteps = %q", got.ReproSteps)
	}
	if got.AcceptanceCriteria != "No more flicker" {
		t.Errorf("AcceptanceCriteria = %q", got.AcceptanceCriteria)
	}
}

func TestCommentBody_MarkdownDefault(t *testing.T) {
	s := &SDK{}
	if got := s.commentBody("**bold**"); got != "**bold**" {
		t.Errorf("commentBody = %q, want markdown sent verbatim", got)
	}
}

func TestCommentBody_HTMLMode(t *testing.T) {
	s := &SDK{WriteHTML: true}
	if got := s.commentBody("**bold**"); !strings.Contains(got, "<strong>") {
		t.Errorf("commentBody = %q, want HTML", got)
	}
}

func TestConvertComment(t *testing.T) {
	when := azuredevops.Time{Time: time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)}
	c := workitemtracking.Comment{
		Id:          ptr(7),
		CreatedBy:   &webapi.IdentityRef{DisplayName: ptr("Alex Kim")},
		CreatedDate: &when,
		Text:        ptr("<p>Looks <strong>good</strong> to me.</p>"),
	}
	got := convertComment(c)
	want := model.Comment{ID: 7, Author: "Alex Kim", CreatedDate: when.Time, Text: "Looks **good** to me."}
	if got != want {
		t.Errorf("convertComment(%+v) = %+v, want %+v", c, got, want)
	}
}

func TestActivityWIQL(t *testing.T) {
	got := activityWIQL(14)
	want := "SELECT [System.Id] FROM WorkItems WHERE [System.TeamProject] = @project AND [System.State] <> 'Removed' AND [System.ChangedDate] >= @Today - 14 ORDER BY [System.ChangedDate] DESC"
	if got != want {
		t.Errorf("activityWIQL(14) =\n%s\nwant\n%s", got, want)
	}
}

func TestConvert_CreatedBy(t *testing.T) {
	fields := map[string]any{
		model.FieldCreatedBy: map[string]any{"displayName": "Priya Natarajan", "uniqueName": "priya@contoso.com"},
		model.FieldChangedBy: map[string]any{"displayName": "Alex Kim", "uniqueName": "alex@contoso.com"},
	}
	m := (&SDK{}).convert(&workitemtracking.WorkItem{Id: ptr(7), Rev: ptr(1), Fields: &fields}, "Platform")
	if m.CreatedBy != "Priya Natarajan" || m.ChangedBy != "Alex Kim" {
		t.Errorf("CreatedBy = %q, ChangedBy = %q", m.CreatedBy, m.ChangedBy)
	}
}

func TestFakeActivity(t *testing.T) {
	f := NewFake()
	f.Latency = 0
	items, err := f.Activity(context.Background(), "Platform")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("demo feed is empty: seed changes must fall inside the window")
	}
	since := time.Now().AddDate(0, 0, -ActivityDays)
	for i, it := range items {
		if it.ChangedDate.Before(since) {
			t.Errorf("#%d changed %v, outside the %d-day window", it.ID, it.ChangedDate, ActivityDays)
		}
		if i > 0 && it.ChangedDate.After(items[i-1].ChangedDate) {
			t.Errorf("#%d is out of order", it.ID)
		}
	}
}
