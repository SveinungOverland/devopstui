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
