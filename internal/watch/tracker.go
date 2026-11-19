// Package watch owns the Watch State: which video Items have been
// Watched. Diadem marks an Item on its own once clients have pulled
// enough of its bytes (CONTEXT.md: Watched), the user can toggle the
// mark in the TUI, and the state survives restarts in a JSON mirror
// inside the Library (.diadem/watched.json). The in-memory map is the
// single source of truth — the file is always rewritten whole, never
// patched.
package watch

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/pandaCaffeine/diadem/internal/library"
	"github.com/pandaCaffeine/diadem/internal/logx"
)

// WatchedPercent is the share of an Item's size (in percent) that must
// reach clients before Diadem calls it Watched on its own. A constant,
// not a flag: tune it here after watching real playback.
const WatchedPercent = 30

// Key identifies one media file across restarts: its path relative to
// the Library root (the ContentDirectory ObjectID form) plus its size.
// The size guards against a different file reusing the path: a replaced
// or re-ripped Item starts unwatched.
type Key struct {
	RelPath string
	Size    int64
}

// Tracker owns the Watch State. All methods are safe for concurrent
// use: ObserveBytes comes from HTTP handler goroutines, Prune from the
// scan path, Toggle from the TUI. The on-disk mirror lives in
// persist.go.
type Tracker struct {
	mu      sync.Mutex
	file    string
	logger  logx.Logger
	watched map[Key]int64 // WatchedAt, unix seconds
	reading map[Key]int64 // bytes pulled so far for not-yet-Watched Items
	dirty   bool          // mirror needs a rewrite
	timer   *time.Timer   // debounced flush; nil until first change
}

// New builds a Tracker mirroring its state into filePath (usually
// <Library>/.diadem/watched.json). A nil logger drops diagnostics.
func New(filePath string, logger logx.Logger) *Tracker {
	if logger == nil {
		logger = logx.NopLogger{}
	}
	return &Tracker{
		file:    filePath,
		logger:  logger,
		watched: make(map[Key]int64),
		reading: make(map[Key]int64),
	}
}

// Watched reports whether the Item is Watched.
func (t *Tracker) Watched(k Key) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.watched[k]
	return ok
}

// MarkedAt reports when the Item became Watched, in unix seconds.
func (t *Tracker) MarkedAt(k Key) (int64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	at, ok := t.watched[k]
	return at, ok
}

// ToggleWatched flips the mark by hand: the TUI's `w`. A manual mark
// sticks exactly like an automatic one; unmarking clears the byte
// counter too, so playback can earn the mark back from zero.
func (t *Tracker) ToggleWatched(k Key) {
	if k.Size <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.watched[k]; ok {
		delete(t.watched, k)
		t.logger.Logf("[watch] unmarked %s", k.RelPath)
	} else {
		t.watched[k] = time.Now().Unix()
		delete(t.reading, k)
		t.logger.Logf("[watch] marked %s", k.RelPath)
	}
	t.dirty = true
	t.armTimerLocked()
}

// ObserveBytes reports bytes of an Item that actually reached clients —
// summed over every request and every client on the LAN. Once the sum
// passes the WatchedPercent line, the Item is Watched. Watched Items
// stop counting: the counter only ever earns the mark, never loses it.
func (t *Tracker) ObserveBytes(k Key, delivered int64) {
	if k.Size <= 0 || delivered <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.watched[k]; ok {
		return
	}
	t.reading[k] += delivered
	if t.reading[k] < watchedBytes(k.Size) {
		return
	}
	delete(t.reading, k)
	t.watched[k] = time.Now().Unix()
	t.logger.Logf("[watch] marked %s", k.RelPath)
	t.dirty = true
	t.armTimerLocked()
}

// watchedBytes is the byte count that earns the mark: a plain integer
// form of size · WatchedPercent / 100.
func watchedBytes(size int64) int64 {
	return size * WatchedPercent / 100
}

// Prune drops entries whose file is gone from the Library. An entry
// dies only when its parent Container is present in the fresh snapshot
// while an Item with the same path and size is not — so a renamed,
// deleted or replaced file forgets its mark, but a whole missing region
// of the tree (an unplugged drive, a flaky mount) keeps its entries.
func (t *Tracker) Prune(root *library.Node) {
	if root == nil {
		return
	}

	items := make(map[Key]bool)
	dirs := make(map[string]bool)
	var walk func(n *library.Node)
	walk = func(n *library.Node) {
		rel, err := filepath.Rel(root.Path, n.Path)
		if err != nil {
			return
		}
		if n.Kind == library.Item {
			items[Key{RelPath: rel, Size: n.Size}] = true
			return
		}
		dirs[rel] = true
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)

	t.mu.Lock()
	defer t.mu.Unlock()
	removed := 0
	for k := range t.watched {
		if dirs[filepath.Dir(k.RelPath)] && !items[k] {
			delete(t.watched, k)
			removed++
		}
	}
	if removed > 0 {
		t.logger.Logf("[watch] pruned %d stale entries", removed)
		t.dirty = true
		t.armTimerLocked()
	}
}
