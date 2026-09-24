package ado

import (
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

func TestRelationTargetID(t *testing.T) {
	cases := []struct {
		url string
		id  int
		ok  bool
	}{
		{"https://dev.azure.com/contoso/_apis/wit/workItems/1234", 1234, true},
		{"https://dev.azure.com/contoso/abc-guid/_apis/wit/workitems/7", 7, true},
		{"vstfs:///Git/Commit/abc%2Fdef", 0, false},
		{"https://dev.azure.com/contoso/_apis/wit/attachments/99", 0, false},
		{"https://dev.azure.com/contoso/_apis/wit/workItems/", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		id, ok := relationTargetID(c.url)
		if id != c.id || ok != c.ok {
			t.Errorf("relationTargetID(%q) = %d, %v; want %d, %v", c.url, id, ok, c.id, c.ok)
		}
	}
}

func TestLinkKinds(t *testing.T) {
	rel := func(r, url string) workitemtracking.WorkItemRelation {
		return workitemtracking.WorkItemRelation{Rel: &r, Url: &url}
	}
	wi := func(id string) string { return "https://dev.azure.com/o/_apis/wit/workItems/" + id }
	got := linkKinds([]workitemtracking.WorkItemRelation{
		rel("System.LinkTypes.Hierarchy-Reverse", wi("1")),
		rel("System.LinkTypes.Related", wi("2")),
		rel("System.LinkTypes.Dependency-Forward", wi("3")),
		rel("System.LinkTypes.Dependency-Reverse", wi("4")),
		rel("System.LinkTypes.Duplicate-Forward", wi("5")),
		rel("System.LinkTypes.Duplicate-Reverse", wi("6")),
		rel("Microsoft.VSTS.Common.TestedBy-Forward", wi("7")),
		rel("ArtifactLink", "vstfs:///Git/Commit/x"),
		rel("System.LinkTypes.Related", wi("2")),
		{},
	})
	want := []link{
		{model.RelRelated, 2}, {model.RelSuccessor, 3}, {model.RelPredecessor, 4},
		{model.RelDuplicate, 5}, {model.RelDuplicateOf, 6},
	}
	if len(got) != len(want) {
		t.Fatalf("linkKinds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("linkKinds[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
