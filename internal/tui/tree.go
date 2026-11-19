package tui

import (
	"github.com/pandaCaffeine/diadem/internal/library"
)

// Row is one visible line of the tree.
type Row struct {
	Node      *library.Node
	Depth     int
	Collapsed bool // for Containers: whether children are hidden
}

// TreeView renders the Library as a flat list of visible rows with a
// cursor and a scroll window. Collapsed state is keyed by path, so it
// survives rescans (step 6 reuses this).
type TreeView struct {
	root      *library.Node
	collapsed map[string]bool
	cursor    int // index into visible rows
	offset    int // first visible row
	Width     int
	Height    int
}

func NewTreeView(root *library.Node) *TreeView {
	return &TreeView{
		root:      root,
		collapsed: make(map[string]bool),
		Width:     80,
	}
}

// SetRoot swaps the tree (e.g. after a rescan), keeping collapsed folders
// collapsed and clamping the cursor into range.
func (t *TreeView) SetRoot(root *library.Node) {
	t.root = root
	t.clamp()
}

// Visible flattens the tree depth-first, hiding children of collapsed
// Containers. The root itself is always the first row.
func (t *TreeView) Visible() []Row {
	if t.root == nil {
		return nil
	}
	rows := make([]Row, 0, 64)
	var walk func(n *library.Node, depth int)
	walk = func(n *library.Node, depth int) {
		rows = append(rows, Row{Node: n, Depth: depth, Collapsed: t.collapsed[n.Path]})
		if n.Kind == library.Container && !t.collapsed[n.Path] {
			for _, c := range n.Children {
				walk(c, depth+1)
			}
		}
	}
	walk(t.root, 0)
	return rows
}

// Current returns the row under the cursor, or nil before the first scan.
func (t *TreeView) Current() *Row {
	rows := t.Visible()
	if t.cursor < 0 || t.cursor >= len(rows) {
		return nil
	}
	return &rows[t.cursor]
}

// Move shifts the cursor by delta rows, clamped to the visible range.
func (t *TreeView) Move(delta int) {
	rows := t.Visible()
	if len(rows) == 0 {
		return
	}
	t.cursor = clamp(t.cursor+delta, 0, len(rows)-1)
	t.ensureVisible()
}

// Page moves the cursor by one viewport height.
func (t *TreeView) Page(delta int) {
	t.Move(delta * max(t.Height, 1))
}

func (t *TreeView) Home() { t.cursor = 0; t.ensureVisible() }

func (t *TreeView) End() {
	rows := t.Visible()
	t.cursor = len(rows) - 1
	t.ensureVisible()
}

// Toggle collapses or expands the Container under the cursor.
func (t *TreeView) Toggle() {
	cur := t.Current()
	if cur == nil || cur.Node.Kind != library.Container {
		return
	}
	if t.collapsed[cur.Node.Path] {
		delete(t.collapsed, cur.Node.Path)
	} else {
		t.collapsed[cur.Node.Path] = true
	}
	t.clamp()
}

// clamp brings cursor and offset back into valid range after the visible
// row list changed (scan, toggle, resize).
func (t *TreeView) clamp() {
	rows := t.Visible()
	if len(rows) == 0 {
		t.cursor, t.offset = 0, 0
		return
	}
	t.cursor = clamp(t.cursor, 0, len(rows)-1)
	if t.offset > len(rows)-1 {
		t.offset = len(rows) - 1
	}
	t.ensureVisible()
}

// ensureVisible scrolls the offset, and only the offset, so the cursor
// row is back inside the viewport.
func (t *TreeView) ensureVisible() {
	h := max(t.Height, 1)
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+h {
		t.offset = t.cursor - h + 1
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
