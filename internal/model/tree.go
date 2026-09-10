package model

import "sort"

// Node is a work item placed in the hierarchy.
type Node struct {
	Item     *WorkItem
	Children []*Node
	Depth    int
	// External marks a parent that is outside the queried scope (e.g. an Epic
	// that lives in the backlog while its PBIs are in the sprint). It is shown
	// dimmed so the hierarchy stays complete.
	External bool
}

// Tree is the built hierarchy plus lookup helpers.
type Tree struct {
	Roots []*Node
	byID  map[int]*Node
}

// BuildTree arranges items into a forest using ParentID. Items whose parent is
// not present become roots. Parents that are only present in `external` are
// included as dimmed roots. Cycles are broken by treating the first item seen
// on the cycle as a root.
func BuildTree(items []*WorkItem, external []*WorkItem) *Tree {
	t := &Tree{byID: make(map[int]*Node, len(items)+len(external))}
	for _, it := range items {
		t.byID[it.ID] = &Node{Item: it}
	}
	for _, it := range external {
		if _, ok := t.byID[it.ID]; !ok {
			t.byID[it.ID] = &Node{Item: it, External: true}
		}
	}

	for _, n := range t.byID {
		p := n.Item.ParentID
		if p == 0 || p == n.Item.ID {
			t.Roots = append(t.Roots, n)
			continue
		}
		parent, ok := t.byID[p]
		if !ok || createsCycle(t, n, parent) {
			t.Roots = append(t.Roots, n)
			continue
		}
		parent.Children = append(parent.Children, n)
	}

	sortNodes(t.Roots)
	for _, r := range t.Roots {
		setDepth(r, 0)
	}
	return t
}

// createsCycle reports whether attaching child under parent would create a
// loop by walking parent's ancestry via ParentID.
func createsCycle(t *Tree, child, parent *Node) bool {
	seen := map[int]bool{child.Item.ID: true}
	for cur := parent; cur != nil; {
		if seen[cur.Item.ID] {
			return true
		}
		seen[cur.Item.ID] = true
		next, ok := t.byID[cur.Item.ParentID]
		if !ok || cur.Item.ParentID == 0 {
			return false
		}
		cur = next
	}
	return false
}

func setDepth(n *Node, d int) {
	n.Depth = d
	sortNodes(n.Children)
	for _, c := range n.Children {
		setDepth(c, d+1)
	}
}

// sortNodes orders by kind (Epic before Feature before PBI), then priority,
// then ID, which approximates the backlog order without an extra API call.
func sortNodes(ns []*Node) {
	sort.SliceStable(ns, func(i, j int) bool {
		a, b := ns[i].Item, ns[j].Item
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Priority != b.Priority {
			if a.Priority == 0 {
				return false
			}
			if b.Priority == 0 {
				return true
			}
			return a.Priority < b.Priority
		}
		return a.ID < b.ID
	})
}

// Get returns the node for an ID.
func (t *Tree) Get(id int) (*Node, bool) {
	n, ok := t.byID[id]
	return n, ok
}

// Len is the number of nodes in the tree.
func (t *Tree) Len() int { return len(t.byID) }

// Walk visits every node depth-first in display order.
func (t *Tree) Walk(fn func(*Node)) {
	var visit func(*Node)
	visit = func(n *Node) {
		fn(n)
		for _, c := range n.Children {
			visit(c)
		}
	}
	for _, r := range t.Roots {
		visit(r)
	}
}

// Descendants returns the IDs of all nodes below n.
func (n *Node) Descendants() []int {
	var out []int
	var visit func(*Node)
	visit = func(x *Node) {
		for _, c := range x.Children {
			out = append(out, c.Item.ID)
			visit(c)
		}
	}
	visit(n)
	return out
}

// Flatten returns nodes in display order, skipping children of collapsed
// nodes. collapsed is keyed by work item ID.
func (t *Tree) Flatten(collapsed map[int]bool) []*Node {
	var out []*Node
	var visit func(*Node)
	visit = func(n *Node) {
		out = append(out, n)
		if collapsed[n.Item.ID] {
			return
		}
		for _, c := range n.Children {
			visit(c)
		}
	}
	for _, r := range t.Roots {
		visit(r)
	}
	return out
}
