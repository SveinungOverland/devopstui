package model

import (
	"reflect"
	"testing"
)

func TestMentions(t *testing.T) {
	cases := []struct {
		name     string
		it       WorkItem
		comments []Comment
		exclude  []int
		want     []int
	}{
		{"plain", WorkItem{ID: 1, Description: "see #1234"}, nil, nil, []int{1234}},
		{"start of text", WorkItem{ID: 1, Description: "#42 first"}, nil, nil, []int{42}},
		{"several, in order", WorkItem{ID: 1, Description: "#3 then #2", ReproSteps: "and #1"}, nil, nil, []int{3, 2}},
		{"dedup", WorkItem{ID: 1, Description: "#5 and #5 again"}, nil, nil, []int{5}},
		{"self excluded", WorkItem{ID: 7, Description: "this is #7"}, nil, nil, nil},
		{"parent excluded", WorkItem{ID: 7, ParentID: 6, Description: "under #6"}, nil, nil, nil},
		{"caller exclusions", WorkItem{ID: 1, Description: "#8 #9"}, nil, []int{8}, []int{9}},
		{"comments", WorkItem{ID: 1}, []Comment{{Text: "landed in #77"}}, nil, []int{77}},
		{"acceptance criteria", WorkItem{ID: 1, AcceptanceCriteria: "- like (#12)"}, nil, nil, []int{12}},
		{"markdown link", WorkItem{ID: 1, Description: "[#300](https://x/_workitems/edit/300)"}, nil, nil, []int{300}},
		{"url fragment ignored", WorkItem{ID: 1, Description: "https://example.com/page#123"}, nil, nil, nil},
		{"html entity ignored", WorkItem{ID: 1, Description: "&#123; x"}, nil, nil, nil},
		{"word suffix ignored", WorkItem{ID: 1, Description: "abc#123 C#4"}, nil, nil, nil},
		{"not a number", WorkItem{ID: 1, Description: "#12abc # heading"}, nil, nil, nil},
		{"inside code still counts", WorkItem{ID: 1, Description: "```\nfixes #55\n```"}, nil, nil, []int{55}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Mentions(&c.it, c.comments, c.exclude...)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Mentions = %v, want %v", got, c.want)
			}
		})
	}
}

func TestSortRelated(t *testing.T) {
	rs := []RelatedItem{
		{RelMention, &WorkItem{ID: 1}},
		{RelSuccessor, &WorkItem{ID: 9}},
		{RelRelated, &WorkItem{ID: 5}},
		{RelSuccessor, &WorkItem{ID: 3}},
	}
	SortRelated(rs)
	var got []int
	for _, r := range rs {
		got = append(got, r.Item.ID)
	}
	if want := []int{5, 3, 9, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}
