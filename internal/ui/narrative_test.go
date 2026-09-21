package ui

import (
	"strings"
	"testing"
)

// Item #1022 ("Login page flickers on Safari") is a demo Bug: Azure DevOps
// Bugs carry Repro Steps / Acceptance Criteria instead of a Description
// (issue #36), and the demo fake mirrors that.

func TestBugDetailShowsReproStepsAndAcceptanceCriteria(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1022)
	h.app.refreshDetail()
	h.dump("22-bug-detail")
	v := h.app.View()
	for _, want := range []string{"Repro steps", "Acceptance criteria"} {
		if !strings.Contains(v, want) {
			t.Errorf("bug detail view missing %q:\n%s", want, v)
		}
	}
}

func TestBugDescKeyEditsReproSteps(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1022)
	h.keys("d")
	ed, ok := h.app.popup.(*mdEditor)
	if !ok {
		t.Fatalf("expected built-in editor, got %T", h.app.popup)
	}
	if !strings.Contains(ed.title, "Repro Steps") {
		t.Fatalf("editor title = %q, want it to name Repro Steps", ed.title)
	}
	h.keys("i", "A", "esc", "ctrl+s")
	if len(h.fake.Updates) != 1 || h.fake.Updates[0].Patches[0].Field != "Microsoft.VSTS.TCM.ReproSteps" {
		t.Fatalf("updates = %+v", h.fake.Updates)
	}
}

func TestFormFieldsForBug(t *testing.T) {
	h := newHarness(t, 160, 45)
	h.app.sprint.jumpTo(1022) // Bug
	h.keys("e")
	f, ok := h.app.popup.(*form)
	if !ok {
		t.Fatalf("expected form, got %T", h.app.popup)
	}
	var labels []string
	for _, fld := range f.fields {
		labels = append(labels, fld.label)
	}
	joined := strings.Join(labels, ",")
	if !strings.Contains(joined, "Repro Steps") || !strings.Contains(joined, "Acceptance Criteria") {
		t.Fatalf("bug form fields = %v, want Repro Steps and Acceptance Criteria", labels)
	}
	if strings.Contains(joined, "Description") {
		t.Fatalf("bug form fields = %v, should not list Description", labels)
	}
	h.keys("esc")

	h.app.sprint.jumpTo(1003) // PBI
	h.keys("e")
	f, ok = h.app.popup.(*form)
	if !ok {
		t.Fatalf("expected form, got %T", h.app.popup)
	}
	labels = nil
	for _, fld := range f.fields {
		labels = append(labels, fld.label)
	}
	joined = strings.Join(labels, ",")
	if !strings.Contains(joined, "Description") {
		t.Fatalf("non-bug form fields = %v, want Description", labels)
	}
	if strings.Contains(joined, "Repro Steps") || strings.Contains(joined, "Acceptance Criteria") {
		t.Fatalf("non-bug form fields = %v, should not list Repro Steps/Acceptance Criteria", labels)
	}
}
