package model

import (
	"testing"
	"time"
)

func TestFieldChangeText(t *testing.T) {
	cases := []struct {
		c    FieldChange
		want string
	}{
		{FieldChange{Kind: ChangeCreated, Label: "Created"}, "created"},
		{FieldChange{Kind: ChangeState, Label: "State", Old: "New", New: "Done"}, "State New → Done"},
		{FieldChange{Kind: ChangeAssigned, Label: "Assigned", New: "Alex Kim"}, "Assigned — → Alex Kim"},
		{FieldChange{Kind: ChangeAssigned, Label: "Assigned", Old: "Alex Kim"}, "Assigned Alex Kim → —"},
		{FieldChange{Kind: ChangeEdit, Label: "Description"}, "Description edited"},
		{FieldChange{Kind: ChangeComment, Label: "Comment", New: "looks good"}, "comment: looks good"},
		{FieldChange{Kind: ChangeLink, Label: "Parent", Old: "#1", New: "#2"}, "Parent #1 → #2"},
		{FieldChange{Kind: ChangeLink, Label: "Pull Request"}, "Pull Request added"},
		{FieldChange{Kind: ChangeLink, Label: "Related", New: "#7"}, "Related added #7"},
		{FieldChange{Kind: ChangeLink, Label: "Related", Old: "#7", Removed: true}, "Related removed #7"},
	}
	for _, c := range cases {
		if got := c.c.Text(); got != c.want {
			t.Errorf("Text(%+v) = %q, want %q", c.c, got, c.want)
		}
	}
}

func TestChangeQueryCovers(t *testing.T) {
	since := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	open := ChangeQuery{Since: since}
	closed := ChangeQuery{Since: since, Until: since.AddDate(0, 0, 14)}
	for _, c := range []struct {
		q    ChangeQuery
		at   time.Time
		want bool
	}{
		{open, since.Add(-time.Second), false},
		{open, since, true},
		{open, since.AddDate(1, 0, 0), true},
		{closed, since.AddDate(0, 0, 13), true},
		{closed, since.AddDate(0, 0, 14), false},
	} {
		if got := c.q.Covers(c.at); got != c.want {
			t.Errorf("Covers(%v) = %v, want %v", c.at, got, c.want)
		}
	}
}

func TestTallyChanges(t *testing.T) {
	got := TallyChanges([]Change{
		{ItemID: 1, Fields: []FieldChange{{Kind: ChangeCreated}, {Kind: ChangeAssigned, New: "A"}}},
		{ItemID: 1, Fields: []FieldChange{{Kind: ChangeState, Old: "New", New: "Committed"}, {Kind: ChangeAssigned, Old: "A", New: "B"}}},
		{ItemID: 2, Fields: []FieldChange{{Kind: ChangeState, Old: "Committed", New: "Done"}, {Kind: ChangeComment, New: "x"}}},
		{ItemID: 3, Fields: []FieldChange{{Kind: ChangeIteration, Old: "a", New: "b"}, {Kind: ChangeEdit, Label: "Title"}}},
	})
	want := ChangeTally{Changes: 4, Items: 3, Created: 1, Completed: 1, Comments: 1, StateMoves: 1, Reassigned: 1, Moved: 1}
	if got != want {
		t.Errorf("TallyChanges = %+v, want %+v", got, want)
	}
}
