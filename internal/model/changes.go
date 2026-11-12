package model

import (
	"strings"
	"time"
)

// ChangeKind classifies one thing that happened to a work item, for the
// Changes view's tally and colours.
type ChangeKind int

const (
	ChangeEdit ChangeKind = iota // title, description, effort and the like
	ChangeCreated
	ChangeState
	ChangeAssigned
	ChangeIteration
	ChangeComment
	ChangeLink // parent, related items, pull requests, attachments
)

// FieldChange is one field's change within a revision. Old and New are
// display values; both are empty when only the fact matters (a description
// was edited, an item was created).
type FieldChange struct {
	Kind     ChangeKind
	Label    string // "State", "Assigned", "Description", …
	Old, New string
	// Removed marks a link that was taken away; a link is otherwise an
	// addition. Not every link has a target to show (a pull request, an
	// attachment), so Old and New alone cannot tell the two apart.
	Removed bool
}

// Text is the change on one line, unstyled.
func (c FieldChange) Text() string {
	switch {
	case c.Kind == ChangeCreated:
		return "created"
	case c.Kind == ChangeComment:
		return "comment: " + c.New
	case c.Kind == ChangeLink && c.Old != "" && c.New != "":
		// a re-parent: one link swapped for another
	case c.Kind == ChangeLink && c.Removed:
		return strings.TrimSpace(c.Label + " removed " + c.Old)
	case c.Kind == ChangeLink:
		return strings.TrimSpace(c.Label + " added " + c.New)
	case c.Old == "" && c.New == "":
		return c.Label + " edited"
	}
	return c.Label + " " + c.OldText() + " → " + c.NewText()
}

// OldText and NewText are the two sides of the arrow, with a dash standing
// in for an empty value (unassigned, no effort).
func (c FieldChange) OldText() string { return orDash(c.Old) }
func (c FieldChange) NewText() string { return orDash(c.New) }

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// Change is one revision of a work item: who did it, when, and what it
// changed.
type Change struct {
	ItemID int
	Rev    int
	At     time.Time
	By     string
	Fields []FieldChange
}

// Text is every field change of the revision on one line.
func (c Change) Text() string {
	parts := make([]string, len(c.Fields))
	for i, f := range c.Fields {
		parts[i] = f.Text()
	}
	return strings.Join(parts, " · ")
}

// ChangeQuery says which changes to fetch.
type ChangeQuery struct {
	// Since and Until bound when the change was made. A zero Until means
	// up to now.
	Since, Until time.Time
	// IterationPath narrows to items under that iteration; "" is the
	// whole project.
	IterationPath string
	// Areas narrows to items in those area paths (the team filter).
	Areas []TeamArea
}

// Covers reports whether t falls inside the query's window.
func (q ChangeQuery) Covers(t time.Time) bool {
	if t.Before(q.Since) {
		return false
	}
	return q.Until.IsZero() || t.Before(q.Until)
}

// ChangeSet is the answer to a ChangeQuery.
type ChangeSet struct {
	// Items is every item with a change in the window, as it is now.
	Items []*WorkItem
	// Changes is newest first.
	Changes []Change
	// Truncated is set when more items changed than the backend reads the
	// history of; the most recently changed ones are kept.
	Truncated bool
	// Failed counts items whose history could not be read.
	Failed int
}

// ChangeTally sums up a list of changes.
type ChangeTally struct {
	Changes, Items                int
	Created, Completed, Comments  int
	StateMoves, Reassigned, Moved int
}

// TallyChanges counts changes by what they did. Completed counts the state
// changes that ended in a done state; StateMoves counts the rest.
func TallyChanges(changes []Change) ChangeTally {
	t := ChangeTally{Changes: len(changes)}
	items := map[int]bool{}
	for _, c := range changes {
		items[c.ItemID] = true
		created := false
		for _, f := range c.Fields {
			created = created || f.Kind == ChangeCreated
		}
		for _, f := range c.Fields {
			switch f.Kind {
			case ChangeCreated:
				t.Created++
			case ChangeComment:
				t.Comments++
			case ChangeAssigned:
				if !created { // born assigned is not a reassignment
					t.Reassigned++
				}
			case ChangeIteration:
				t.Moved++
			case ChangeState:
				if IsDone(f.New) {
					t.Completed++
				} else {
					t.StateMoves++
				}
			}
		}
	}
	t.Items = len(items)
	return t
}
