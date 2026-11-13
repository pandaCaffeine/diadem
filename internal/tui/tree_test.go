package tui

import (
	"path/filepath"
	"testing"

	"github.com/pandaCaffeine/diadem/internal/library"
)

func testTree() *library.Node {
	// Movies/
	//   sub/
	//     a.mp4
	//   z.mkv
	root := &library.Node{Name: "Movies", Path: "/m", Kind: library.Container}
	sub := &library.Node{Name: "sub", Path: "/m/sub", Kind: library.Container}
	itemA := &library.Node{Name: "a.mp4", Path: "/m/sub/a.mp4", Kind: library.Item, Media: library.MediaVideo}
	itemZ := &library.Node{Name: "z.mkv", Path: "/m/z.mkv", Kind: library.Item, Media: library.MediaVideo}
	sub.Children = append(sub.Children, itemA)
	root.Children = append(root.Children, sub, itemZ)
	return root
}

func names(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Node.Name
	}
	return out
}

func TestTreeVisibleAndToggle(t *testing.T) {
	tv := NewTreeView(testTree())

	want := []string{"Movies", "sub", "a.mp4", "z.mkv"}
	if got := names(tv.Visible()); !equal(got, want) {
		t.Errorf("Visible() = %v, want %v", got, want)
	}

	// Collapse "sub": its child disappears.
	tv.cursor = 1
	tv.Toggle()
	want = []string{"Movies", "sub", "z.mkv"}
	if got := names(tv.Visible()); !equal(got, want) {
		t.Errorf("after toggle Visible() = %v, want %v", got, want)
	}

	// Collapse the root: only the root row is left.
	tv.Home()
	tv.Toggle()
	if got := names(tv.Visible()); !equal(got, []string{"Movies"}) {
		t.Errorf("collapsed root Visible() = %v, want [Movies]", got)
	}
}

func TestTreeCursorClamp(t *testing.T) {
	tv := NewTreeView(testTree())
	tv.Move(10) // past the end
	if tv.cursor != 3 {
		t.Errorf("cursor = %d, want 3", tv.cursor)
	}
	tv.Toggle() // cursor sits on z.mkv, an Item: no-op
	if tv.cursor != 3 {
		t.Errorf("toggle on Item moved cursor to %d", tv.cursor)
	}
	tv.Move(-99)
	if tv.cursor != 0 {
		t.Errorf("cursor = %d, want 0", tv.cursor)
	}
}

func equal(a, b []string) bool {
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

func TestSetRootKeepsCollapsed(t *testing.T) {
	tv := NewTreeView(testTree())
	tv.cursor = 1 // sub
	tv.Toggle()   // collapse sub

	rescanned := testTree()
	rescanned.Children[0].Children = append(rescanned.Children[0].Children,
		&library.Node{Name: "b.mp4", Path: "/m/sub/b.mp4", Kind: library.Item, Media: library.MediaVideo},
	)
	tv.SetRoot(rescanned)

	want := []string{"Movies", "sub", "z.mkv"} // sub stays collapsed
	if got := names(tv.Visible()); !equal(got, want) {
		t.Errorf("after SetRoot Visible() = %v, want %v", got, want)
	}
}

func TestPopupPath(t *testing.T) {
	p := filepath.Join("x", "y")
	if got := truncate(tildePath(p), 60); got != p {
		t.Errorf("tildePath/truncate mangled %q into %q", p, got)
	}
}
