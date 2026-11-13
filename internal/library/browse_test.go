package library

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadContainer(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"sub", "empty", "onlyjunk"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]int{
		"movie.mkv":          10,
		"sub/Игрушка.mp4":    5,
		"onlyjunk/notes.txt": 1,
		"notes.txt":          1,
		".DS_Store":          1,
	}
	for rel, size := range files {
		if err := os.WriteFile(filepath.Join(dir, rel), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := ReadContainer(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Containers first ("sub" only: empty and onlyjunk are pruned), then
	// items in natural order.
	want := []struct {
		name string
		kind NodeKind
	}{
		{"sub", Container},
		{"movie.mkv", Item},
	}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(entries), len(want), entries)
	}
	for i, w := range want {
		if entries[i].Name != w.name || entries[i].Kind != w.kind {
			t.Errorf("entry %d = {%s %v}, want {%s %v}", i, entries[i].Name, entries[i].Kind, w.name, w.kind)
		}
	}
	if entries[1].Size != 10 || entries[1].Media != MediaVideo {
		t.Errorf("item entry = %+v, want size 10 video", entries[1])
	}
}

func TestReadContainerNested(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "b", "deep.mp3"), make([]byte, 1), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ReadContainer(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "a" {
		t.Fatalf("entries = %+v, want just folder a", entries)
	}
	inner, err := ReadContainer(entries[0].Path)
	if err != nil || len(inner) != 1 || inner[0].Name != "b" {
		t.Fatalf("inner entries = %+v err=%v, want just folder b", inner, err)
	}
}

func TestHasMedia(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "b", "deep.mp3"), make([]byte, 1), 0o644); err != nil {
		t.Fatal(err)
	}
	if !HasMedia(dir) || !HasMedia(filepath.Join(dir, "a")) {
		t.Error("HasMedia should find deeply nested media")
	}
	if HasMedia(t.TempDir()) {
		t.Error("HasMedia on empty tree should be false")
	}
}
