package library

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestWatcher(t *testing.T) *Watcher {
	t.Helper()
	w, err := NewWatcher(t.TempDir())
	if err != nil {
		t.Skipf("fsnotify unavailable: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// waitChanged waits for a change signal or fails the test.
func waitChanged(t *testing.T, w *Watcher, timeout time.Duration) {
	t.Helper()
	select {
	case <-w.Changed():
	case <-time.After(timeout):
		t.Fatal("watcher did not report the change")
	}
}

// expectNoChange asserts silence for a while (bursts must coalesce).
func expectNoChange(t *testing.T, w *Watcher, d time.Duration) {
	t.Helper()
	select {
	case <-w.Changed():
		t.Fatal("unexpected extra change signal")
	case <-time.After(d):
	}
}

func TestWatcherDebounce(t *testing.T) {
	w := newTestWatcher(t)
	dir := w.root
	file := filepath.Join(dir, "movie.mkv")

	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitChanged(t, w, 3*time.Second)

	// A burst of writes must coalesce into exactly one more signal.
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(file, []byte("more data"), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	waitChanged(t, w, 3*time.Second)
	expectNoChange(t, w, 700*time.Millisecond)
}

func TestWatcherSeesNewFolders(t *testing.T) {
	w := newTestWatcher(t)
	nested := filepath.Join(w.root, "season", "episode") // mkdir -p style
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	waitChanged(t, w, 3*time.Second)

	// A file deep inside the freshly created tree is noticed too, thanks
	// to the watches added for new folders.
	expectNoChange(t, w, 700*time.Millisecond) // let the burst settle
	if err := os.WriteFile(filepath.Join(nested, "deep.flac"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitChanged(t, w, 3*time.Second)
}

func TestWatcherCloseIsIdempotent(t *testing.T) {
	w := newTestWatcher(t)
	if err := w.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
