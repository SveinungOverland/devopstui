package model

import "testing"

func item(id, parent int, kind Kind) *WorkItem {
	return &WorkItem{ID: id, ParentID: parent, Kind: kind, Title: "x"}
}

func ids(ns []*Node) []int {
	out := make([]int, len(ns))
	for i, n := range ns {
		out[i] = n.Item.ID
	}
	return out
}

func eq(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBuildTree_Hierarchy(t *testing.T) {
	items := []*WorkItem{
		item(3, 2, KindRequirement),
		item(2, 1, KindFeature),
		item(1, 0, KindEpic),
		item(4, 2, KindRequirement),
	}
	tr := BuildTree(items, nil)
	flat := tr.Flatten(nil)
	if got := ids(flat); !eq(got, []int{1, 2, 3, 4}) {
		t.Fatalf("flatten order = %v", got)
	}
	if flat[3].Depth != 2 {
		t.Fatalf("depth of 4 = %d, want 2", flat[3].Depth)
	}
}

func TestBuildTree_ExternalParent(t *testing.T) {
	items := []*WorkItem{item(10, 5, KindRequirement)}
	ext := []*WorkItem{item(5, 0, KindFeature)}
	tr := BuildTree(items, ext)
	if len(tr.Roots) != 1 || tr.Roots[0].Item.ID != 5 || !tr.Roots[0].External {
		t.Fatalf("expected dimmed external root 5, got %+v", tr.Roots)
	}
}

func TestBuildTree_OrphanAndCycle(t *testing.T) {
	items := []*WorkItem{
		item(1, 99, KindRequirement), // orphan: parent missing
		item(2, 3, KindFeature),      // cycle 2 <-> 3
		item(3, 2, KindFeature),
	}
	tr := BuildTree(items, nil)
	flat := tr.Flatten(nil)
	if len(flat) != 3 {
		t.Fatalf("expected all 3 nodes visible, got %d", len(flat))
	}
}

func TestFlatten_Collapsed(t *testing.T) {
	items := []*WorkItem{
		item(1, 0, KindEpic),
		item(2, 1, KindFeature),
		item(3, 2, KindRequirement),
	}
	tr := BuildTree(items, nil)
	got := ids(tr.Flatten(map[int]bool{2: true}))
	if !eq(got, []int{1, 2}) {
		t.Fatalf("collapsed flatten = %v", got)
	}
	n, _ := tr.Get(1)
	if d := n.Descendants(); !eq(d, []int{2, 3}) {
		t.Fatalf("descendants = %v", d)
	}
}
