package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pandaCaffeine/diadem/internal/library"
)

func newTestTracker(t *testing.T) (*Tracker, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), ".diadem", "watched.json")
	tr := New(file, nil)
	return tr, file
}

func TestRoundTrip(t *testing.T) {
	tr, file := newTestTracker(t)
	tr.Load()

	tr.ToggleWatched("movies/a.mkv", 100)
	if err := tr.Flush(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var sf stateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatalf("mirror is not valid JSON: %v", err)
	}
	if sf.Version != formatVersion {
		t.Errorf("version = %d, want %d", sf.Version, formatVersion)
	}
	if len(sf.Entries) != 1 || sf.Entries[0].Path != "movies/a.mkv" || sf.Entries[0].Size != 100 {
		t.Fatalf("entries = %+v", sf.Entries)
	}
	if sf.Entries[0].WatchedAt <= 0 {
		t.Errorf("watched_at = %d, want a unix timestamp", sf.Entries[0].WatchedAt)
	}

	// A fresh tracker restores the mark; a different size is a different
	// file and starts unwatched.
	reloaded := New(file, nil)
	reloaded.Load()
	if !reloaded.Watched("movies/a.mkv", 100) {
		t.Error("mark did not survive the restart")
	}
	if reloaded.Watched("movies/a.mkv", 101) {
		t.Error("a different size must not inherit the mark")
	}
	if at, ok := reloaded.MarkedAt("movies/a.mkv", 100); !ok || at <= 0 {
		t.Errorf("MarkedAt = %d %v, want the saved timestamp", at, ok)
	}
}

func TestFirstRunCreatesMirror(t *testing.T) {
	tr, file := newTestTracker(t)
	tr.Load() // no file yet: a normal first run
	if err := tr.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("mirror not created on first run: %v", err)
	}
}

func TestCorruptFileStartsEmpty(t *testing.T) {
	_, file := newTestTracker(t)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := New(file, nil)
	tr.Load()
	if tr.Watched("a.mkv", 1) {
		t.Error("corrupt state must not yield marks")
	}
	// The next flush overwrites the corrupt file with a valid mirror.
	if err := tr.Flush(); err != nil {
		t.Fatal(err)
	}
	var sf stateFile
	if data, err := os.ReadFile(file); err != nil || json.Unmarshal(data, &sf) != nil {
		t.Errorf("corrupt file not replaced: %v", err)
	}
}

func TestUnsupportedVersionStartsEmpty(t *testing.T) {
	_, file := newTestTracker(t)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{"version":99,"entries":[{"path":"a.mkv","size":1,"watched_at":5}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tr := New(file, nil)
	tr.Load()
	if tr.Watched("a.mkv", 1) {
		t.Error("a future format must not be read as v1")
	}
}

func TestObserveBytesThreshold(t *testing.T) {
	tr, _ := newTestTracker(t)
	tr.Load()

	const size = 1000
	// watchedBytes(1000) = 300: strictly below stays unwatched.
	tr.ObserveBytes("v.mkv", size, 150)
	tr.ObserveBytes("v.mkv", size, 149) // 299 total
	if tr.Watched("v.mkv", size) {
		t.Fatal("marked below the threshold")
	}
	tr.ObserveBytes("v.mkv", size, 1) // 300 total — the mark
	if !tr.Watched("v.mkv", size) {
		t.Fatal("not marked at the threshold")
	}
	if at, ok := tr.MarkedAt("v.mkv", size); !ok || at <= 0 {
		t.Errorf("MarkedAt = %d %v", at, ok)
	}

	// A Watched Item stops counting; repeated pulls cannot unmark it.
	tr.ObserveBytes("v.mkv", size, 10_000)
	if !tr.Watched("v.mkv", size) {
		t.Error("a rewatch unmarked the Item")
	}
	// Degenerate sizes never mark.
	tr.ObserveBytes("zero.mkv", 0, 5)
	tr.ObserveBytes("empty.mkv", 100, 0)
	if tr.Watched("zero.mkv", 0) || tr.Watched("empty.mkv", 100) {
		t.Error("degraded report marked an Item")
	}
}

func TestToggleUnmarkThenReearn(t *testing.T) {
	tr, _ := newTestTracker(t)
	tr.Load()

	tr.ToggleWatched("v.mkv", 100)
	if !tr.Watched("v.mkv", 100) {
		t.Fatal("manual mark missing")
	}
	tr.ToggleWatched("v.mkv", 100)
	if tr.Watched("v.mkv", 100) {
		t.Fatal("manual unmark missing")
	}
	// The counter restarted from zero: below the threshold stays
	// unwatched, past it the mark is earned again.
	tr.ObserveBytes("v.mkv", 100, 10)
	if tr.Watched("v.mkv", 100) {
		t.Fatal("marked before the threshold after unmark")
	}
	tr.ObserveBytes("v.mkv", 100, 20) // 30 of 100 = the threshold
	if !tr.Watched("v.mkv", 100) {
		t.Fatal("mark not earned back")
	}
}

func TestPrune(t *testing.T) {
	tr, _ := newTestTracker(t)
	tr.Load()

	root := &library.Node{Name: "Lib", Path: "/t", Kind: library.Container}
	movies := &library.Node{Name: "movies", Path: "/t/movies", Kind: library.Container}
	movies.Children = []*library.Node{
		{Name: "a.mkv", Path: "/t/movies/a.mkv", Kind: library.Item, Media: library.MediaVideo, Size: 5},
	}
	root.Children = []*library.Node{
		movies,
		{Name: "b.mkv", Path: "/t/b.mkv", Kind: library.Item, Media: library.MediaVideo, Size: 7},
	}

	tr.ToggleWatched("movies/a.mkv", 5)   // alive: file matches the tree
	tr.ToggleWatched("movies/a.mkv", 9)   // replaced by another size → dies
	tr.ToggleWatched("movies/old.mkv", 1) // deleted → dies
	tr.ToggleWatched("ghost/x.mkv", 2)    // whole region absent → lives
	tr.ToggleWatched("b.mkv", 7)          // alive

	tr.Prune(root)

	if !tr.Watched("movies/a.mkv", 5) {
		t.Error("pruned a live file")
	}
	if !tr.Watched("b.mkv", 7) {
		t.Error("pruned a live top-level file")
	}
	if tr.Watched("movies/a.mkv", 9) {
		t.Error("kept an entry for a file replaced by another size")
	}
	if tr.Watched("movies/old.mkv", 1) {
		t.Error("kept an entry for a deleted file")
	}
	if !tr.Watched("ghost/x.mkv", 2) {
		t.Error("pruned an entry whose region is absent from the tree")
	}
}

func TestPruneNilRoot(t *testing.T) {
	tr, _ := newTestTracker(t)
	tr.Load()
	tr.ToggleWatched("a.mkv", 1)
	tr.Prune(nil) // must not panic and must not touch anything
	if !tr.Watched("a.mkv", 1) {
		t.Error("nil snapshot pruned entries")
	}
}

func TestPruneKeepsEntriesUntilParentKnown(t *testing.T) {
	// A root-only snapshot (nothing scanned inside yet) has the root
	// Container but no Items: top-level entries die, nested ones live —
	// the region below is simply not visible yet.
	tr, _ := newTestTracker(t)
	tr.Load()
	tr.ToggleWatched("a.mkv", 1)
	tr.ToggleWatched("sub/b.mkv", 2)
	tr.Prune(&library.Node{Name: "Lib", Path: "/t", Kind: library.Container})

	if tr.Watched("a.mkv", 1) {
		t.Error("kept an entry for a top-level file the root container proves absent")
	}
	if !tr.Watched("sub/b.mkv", 2) {
		t.Error("pruned an entry in a container the snapshot does not show")
	}
}

func TestSaveSortsEntries(t *testing.T) {
	tr, file := newTestTracker(t)
	tr.Load()
	tr.ToggleWatched("z.mkv", 1)
	tr.ToggleWatched("a/m.mkv", 2)
	tr.ToggleWatched("a.mkv", 3)
	if err := tr.Flush(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var sf stateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(sf.Entries); i++ {
		if sf.Entries[i-1].Path > sf.Entries[i].Path {
			t.Fatalf("entries not sorted: %+v", sf.Entries)
		}
	}
}

func TestPruneAgainstRealScan(t *testing.T) {
	// The strict rule reads paths with filepath.Rel against a tree the
	// scanner built on a real filesystem; pin that agreement once.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "movies"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct {
		rel  string
		size int
	}{
		{"movies/a.mkv", 10},
		{"gone.mkv", 10},
	} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(f.rel)), make([]byte, f.size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tr := New(filepath.Join(dir, ".diadem", "watched.json"), nil)
	tr.Load()
	tr.ToggleWatched("movies/a.mkv", 10)
	tr.ToggleWatched("gone.mkv", 10)

	// The file leaves the disk, a rescan sees the tree without it, and
	// the next prune forgets the mark.
	if err := os.Remove(filepath.Join(dir, "gone.mkv")); err != nil {
		t.Fatal(err)
	}
	lib, err := library.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	tr.Prune(lib.Root)

	if !tr.Watched("movies/a.mkv", 10) {
		t.Error("pruned an entry for a live file")
	}
	if tr.Watched("gone.mkv", 10) {
		t.Error("kept an entry for a deleted file")
	}
}
