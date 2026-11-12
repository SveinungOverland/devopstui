package ado

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/workitemtracking"
	"golang.org/x/sync/errgroup"

	"github.com/sveinungoverland/devopstui/internal/markdown"
	"github.com/sveinungoverland/devopstui/internal/model"
)

// maxChangedItems bounds how many items' histories one Changes call reads.
// Each is a request of its own, so an unscoped query over a busy project
// must not turn into thousands of them; the most recently changed win.
const maxChangedItems = 300

// itemHistory is an item's whole update list, converted. It stays valid
// for as long as the item is at the same revision, so a refresh only
// re-reads the items that changed since the last one.
type itemHistory struct {
	rev     int
	changed time.Time
	changes []model.Change
}

func (s *SDK) Changes(ctx context.Context, project string, q model.ChangeQuery) (model.ChangeSet, error) {
	wiql := changesWiql(q)
	top := maxChangedItems + 1
	// Without time precision WIQL compares dates by day and rejects a
	// literal that carries a time.
	precise := true
	res, err := s.wit.QueryByWiql(ctx, workitemtracking.QueryByWiqlArgs{
		Wiql: &workitemtracking.Wiql{Query: &wiql}, Project: &project, Top: &top, TimePrecision: &precise,
	})
	if err != nil {
		return model.ChangeSet{}, err
	}
	var ids []int
	if res.WorkItems != nil {
		for _, r := range *res.WorkItems {
			ids = append(ids, *r.Id)
		}
	}
	var set model.ChangeSet
	if len(ids) > maxChangedItems {
		ids, set.Truncated = ids[:maxChangedItems], true
	}
	items, err := s.fetch(ctx, project, ids)
	if err != nil {
		return model.ChangeSet{}, err
	}

	perItem := make([][]model.Change, len(items))
	failed := make([]bool, len(items))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(8)
	for i, it := range items {
		g.Go(func() error {
			all, err := s.itemChanges(gctx, it)
			if err != nil {
				if gctx.Err() != nil {
					return gctx.Err() // cancelled or timed out: the whole call failed
				}
				failed[i] = true // one unreadable item must not empty the page
				return nil
			}
			for _, c := range all {
				if q.Covers(c.At) {
					perItem[i] = append(perItem[i], c)
				}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return model.ChangeSet{}, err
	}
	for i, it := range items {
		if failed[i] {
			set.Failed++
		}
		// An item changed since the window closed, but not inside it, has
		// nothing to show.
		if len(perItem[i]) > 0 {
			set.Items = append(set.Items, it)
			set.Changes = append(set.Changes, perItem[i]...)
		}
	}
	sortChanges(set.Changes)
	return set, nil
}

// sortChanges orders newest first, an item's own revisions newest first
// within the same instant.
func sortChanges(changes []model.Change) {
	sort.SliceStable(changes, func(i, j int) bool {
		a, b := changes[i], changes[j]
		if !a.At.Equal(b.At) {
			return a.At.After(b.At)
		}
		if a.ItemID != b.ItemID {
			return a.ItemID < b.ItemID
		}
		return a.Rev > b.Rev
	})
}

// changesWiql finds the items touched since the window opened. What each
// one's changes were, and whether they fall before the window closed, is
// read from its updates afterwards.
func changesWiql(q model.ChangeQuery) string {
	const stamp = "2006-01-02T15:04:05Z"
	w := fmt.Sprintf("SELECT [System.Id] FROM WorkItems WHERE [System.TeamProject] = @project AND [System.ChangedDate] >= '%s'",
		q.Since.UTC().Format(stamp))
	if !q.Until.IsZero() {
		w += fmt.Sprintf(" AND [System.CreatedDate] < '%s'", q.Until.UTC().Format(stamp))
	}
	if q.IterationPath != "" {
		w += fmt.Sprintf(" AND [System.IterationPath] UNDER '%s'", escape(q.IterationPath))
	}
	return w + areaClause(q.Areas) + " ORDER BY [System.ChangedDate] DESC"
}

// itemChanges returns every change ever made to an item, oldest first.
func (s *SDK) itemChanges(ctx context.Context, it *model.WorkItem) ([]model.Change, error) {
	s.mu.Lock()
	h, ok := s.history[it.ID]
	s.mu.Unlock()
	if ok && h.rev == it.Rev && h.changed.Equal(it.ChangedDate) {
		return h.changes, nil
	}
	const page = 200
	var updates []workitemtracking.WorkItemUpdate
	id, top := it.ID, page
	for skip := 0; ; skip += page {
		args := workitemtracking.GetUpdatesArgs{Id: &id, Top: &top, Skip: &skip}
		if it.Project != "" {
			args.Project = &it.Project
		}
		res, err := s.wit.GetUpdates(ctx, args)
		if err != nil {
			return nil, err
		}
		if res == nil {
			break
		}
		updates = append(updates, *res...)
		if len(*res) < page {
			break
		}
	}
	changes := convertUpdates(it.ID, updates)
	s.mu.Lock()
	s.history[it.ID] = itemHistory{rev: it.Rev, changed: it.ChangedDate, changes: changes}
	s.mu.Unlock()
	return changes, nil
}

// convertUpdates turns an item's raw updates, oldest first, into the
// changes worth showing. Updates that only touched bookkeeping fields
// (rank, watermark, dates) are dropped.
func convertUpdates(itemID int, updates []workitemtracking.WorkItemUpdate) []model.Change {
	var out []model.Change
	by := ""
	for _, u := range updates {
		var fields map[string]workitemtracking.WorkItemFieldUpdate
		if u.Fields != nil {
			fields = *u.Fields
		}
		// An update lists only the fields that changed, so the author is
		// missing whenever the same person made the previous one too.
		if name := personName(fields[model.FieldChangedBy].NewValue); name != "" {
			by = name
		}
		c := model.Change{ItemID: itemID, Rev: deref(u.Rev), At: updateTime(u, fields), By: by}
		if c.By == "" && u.RevisedBy != nil {
			c.By = cleanName(deref(u.RevisedBy.DisplayName))
		}
		c.Fields = append(fieldChanges(fields), relationChanges(u.Relations)...)
		if c.At.IsZero() || len(c.Fields) == 0 {
			continue
		}
		out = append(out, c)
	}
	return out
}

// updateTime is when an update was made. That is the new ChangedDate; the
// update's own RevisedDate is when the revision was superseded, which for
// the latest one is the year 9999.
func updateTime(u workitemtracking.WorkItemUpdate, fields map[string]workitemtracking.WorkItemFieldUpdate) time.Time {
	for _, f := range []string{model.FieldChangedDate, "System.AuthorizedDate"} {
		if t := parseTime(fields[f].NewValue); !t.IsZero() {
			return t
		}
	}
	if u.RevisedDate != nil && u.RevisedDate.Time.Year() < 9000 {
		return u.RevisedDate.Time
	}
	return time.Time{}
}

func parseTime(v any) time.Time {
	s, _ := v.(string)
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// tracked lists the fields the Changes view reports, in the order they
// show within one revision.
var tracked = []struct {
	field string
	kind  model.ChangeKind
	label string
	// bare fields report only that they changed: their values are too
	// long to show on a line.
	bare bool
}{
	{model.FieldState, model.ChangeState, "State", false},
	{model.FieldBoardColumn, model.ChangeState, "Column", false},
	{model.FieldAssignedTo, model.ChangeAssigned, "Assigned", false},
	{model.FieldIterationPath, model.ChangeIteration, "Iteration", false},
	{model.FieldAreaPath, model.ChangeEdit, "Area", false},
	{model.FieldTitle, model.ChangeEdit, "Title", false},
	{model.FieldEffort, model.ChangeEdit, "Effort", false},
	{model.FieldStoryPoints, model.ChangeEdit, "Points", false},
	{model.FieldRemainingWork, model.ChangeEdit, "Remaining", false},
	{model.FieldPriority, model.ChangeEdit, "Priority", false},
	{model.FieldTags, model.ChangeEdit, "Tags", false},
	{model.FieldDescription, model.ChangeEdit, "Description", true},
	{model.FieldReproSteps, model.ChangeEdit, "Repro steps", true},
	{model.FieldAcceptanceCriteria, model.ChangeEdit, "Acceptance criteria", true},
}

// commentPreview caps how much of a comment a change carries.
const commentPreview = 240

func fieldChanges(fields map[string]workitemtracking.WorkItemFieldUpdate) []model.FieldChange {
	var out []model.FieldChange
	// The first update sets every field from nothing; listing each as a
	// change would bury the one fact that matters.
	if created, ok := fields["System.CreatedDate"]; ok && created.OldValue == nil {
		out = append(out, model.FieldChange{Kind: model.ChangeCreated, Label: "Created"})
		if who := personName(fields[model.FieldAssignedTo].NewValue); who != "" {
			out = append(out, model.FieldChange{Kind: model.ChangeAssigned, Label: "Assigned", New: who})
		}
		return out
	}
	_, stateChanged := fields[model.FieldState]
	for _, t := range tracked {
		f, ok := fields[t.field]
		if !ok {
			continue
		}
		// The column follows the state; it is news only when it moved
		// within one.
		if t.field == model.FieldBoardColumn && stateChanged {
			continue
		}
		fc := model.FieldChange{Kind: t.kind, Label: t.label}
		if !t.bare {
			fc.Old, fc.New = changeValue(f.OldValue), changeValue(f.NewValue)
			if fc.Old == fc.New {
				continue
			}
		}
		out = append(out, fc)
	}
	// Custom fields come out of a map; sort them so a revision reads the
	// same on every load.
	var custom []model.FieldChange
	for name, f := range fields {
		if !strings.HasPrefix(name, "Custom.") {
			continue
		}
		fc := model.FieldChange{Kind: model.ChangeEdit, Label: strings.TrimPrefix(name, "Custom."),
			Old: changeValue(f.OldValue), New: changeValue(f.NewValue)}
		if fc.Old != fc.New {
			custom = append(custom, fc)
		}
	}
	sort.Slice(custom, func(i, j int) bool { return custom[i].Label < custom[j].Label })
	out = append(out, custom...)
	if text := commentText(fields["System.History"].NewValue); text != "" {
		out = append(out, model.FieldChange{Kind: model.ChangeComment, Label: "Comment", New: text})
	}
	return out
}

var spaces = regexp.MustCompile(`\s+`)

// commentText flattens a comment's HTML to one line, cut to a preview.
func commentText(v any) string {
	html, _ := v.(string)
	if html == "" {
		return ""
	}
	text := strings.TrimSpace(spaces.ReplaceAllString(markdown.FromHTML(html), " "))
	if r := []rune(text); len(r) > commentPreview {
		text = string(r[:commentPreview]) + "…"
	}
	return text
}

// relationChanges reports links added or removed: the parent, other work
// items, and artifacts such as pull requests and commits. Child links are
// left out, since the child's own creation or re-parenting already says it.
func relationChanges(rels *workitemtracking.WorkItemRelationUpdates) []model.FieldChange {
	if rels == nil {
		return nil
	}
	var out []model.FieldChange
	each := func(list *[]workitemtracking.WorkItemRelation, added bool) {
		if list == nil {
			return
		}
		for _, r := range *list {
			label, target, ok := relationLabel(r)
			if !ok {
				continue
			}
			fc := model.FieldChange{Kind: model.ChangeLink, Label: label, Removed: !added}
			if added {
				fc.New = target
			} else {
				fc.Old = target
			}
			out = append(out, fc)
		}
	}
	each(rels.Removed, false)
	each(rels.Added, true)
	// Re-parenting arrives as a removal and an addition; show it as one move.
	oldParent, newParent := -1, -1
	for i, fc := range out {
		switch {
		case fc.Label != "Parent":
		case fc.Removed:
			oldParent = i
		default:
			newParent = i
		}
	}
	if oldParent >= 0 && newParent >= 0 {
		out[newParent].Old = out[oldParent].Old
		out = append(out[:oldParent], out[oldParent+1:]...)
	}
	return out
}

// linkLabels name a work item link in a change, in the singular.
var linkLabels = map[model.RelationKind]string{
	model.RelRelated:     "Related",
	model.RelPredecessor: "Predecessor",
	model.RelSuccessor:   "Successor",
	model.RelDuplicate:   "Duplicate",
	model.RelDuplicateOf: "Duplicate of",
}

func relationLabel(r workitemtracking.WorkItemRelation) (label, target string, ok bool) {
	rel := deref(r.Rel)
	if id, isItem := relationTargetID(deref(r.Url)); isItem {
		target = fmt.Sprintf("#%d", id)
	}
	switch rel {
	case "System.LinkTypes.Hierarchy-Forward":
		return "", "", false
	case "System.LinkTypes.Hierarchy-Reverse":
		return "Parent", target, target != ""
	case "AttachedFile":
		return "Attachment", "", true
	case "Hyperlink":
		return "Hyperlink", "", true
	}
	if kind, known := relationKinds[rel]; known {
		return linkLabels[kind], target, true
	}
	// Artifact links name themselves: "Pull Request", "Fixed in Commit",
	// "Branch", "Build".
	if r.Attributes != nil {
		if name, _ := (*r.Attributes)["name"].(string); name != "" {
			return name, target, true
		}
	}
	return "Link", target, true
}

// changeValue renders a field's old or new value for display.
func changeValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%.2g", x)
	case map[string]any:
		return personName(x)
	}
	return fmt.Sprint(v)
}

// personName is the display name in an identity value, which arrives as an
// object from current servers and as "Name <address>" from older ones.
func personName(v any) string {
	return cleanName(identityName(v))
}

func cleanName(s string) string {
	if i := strings.Index(s, " <"); i > 0 && strings.HasSuffix(s, ">") {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
