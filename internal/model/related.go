package model

import (
	"regexp"
	"sort"
	"strconv"
)

// RelationKind is how a related item is connected to the one being viewed,
// read from the viewed item's side.
type RelationKind int

const (
	RelRelated     RelationKind = iota
	RelPredecessor              // must finish before this item
	RelSuccessor                // waits on this item
	RelDuplicate                // a duplicate of this item
	RelDuplicateOf              // this item duplicates it
	RelMention                  // #id in the text or discussion
)

// Label names the relation for the Related pane's group headings.
func (k RelationKind) Label() string {
	switch k {
	case RelPredecessor:
		return "Predecessors"
	case RelSuccessor:
		return "Successors"
	case RelDuplicate:
		return "Duplicates"
	case RelDuplicateOf:
		return "Duplicate of"
	case RelMention:
		return "Mentioned"
	default:
		return "Related"
	}
}

// RelatedItem is one entry of an item's Related section.
type RelatedItem struct {
	Kind RelationKind
	Item *WorkItem
}

// SortRelated orders entries by group, then id, so the pane reads the same
// on every load regardless of the order the server returned links in.
func SortRelated(rs []RelatedItem) {
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].Kind != rs[j].Kind {
			return rs[i].Kind < rs[j].Kind
		}
		return rs[i].Item.ID < rs[j].Item.ID
	})
}

// mentionRe matches #1234 where the # does not continue a word, a URL
// fragment (page#12) or an HTML entity (&#123;).
var mentionRe = regexp.MustCompile(`(?:^|[^\w&/#])#(\d+)\b`)

// Mentions returns the ids referenced as #1234 in the item's narrative
// fields and discussion, in order of first appearance. The item itself,
// its parent and anything in exclude are left out.
func Mentions(it *WorkItem, comments []Comment, exclude ...int) []int {
	skip := map[int]bool{it.ID: true, it.ParentID: true, 0: true}
	for _, id := range exclude {
		skip[id] = true
	}
	texts := []string{it.Description, it.ReproSteps, it.AcceptanceCriteria}
	for _, c := range comments {
		texts = append(texts, c.Text)
	}
	var out []int
	for _, t := range texts {
		for _, m := range mentionRe.FindAllStringSubmatch(t, -1) {
			id, err := strconv.Atoi(m[1])
			if err != nil || skip[id] {
				continue
			}
			skip[id] = true
			out = append(out, id)
		}
	}
	return out
}
