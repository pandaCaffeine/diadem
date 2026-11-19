// Package watch owns the Watch State: which video Items have been
// Watched. Diadem marks an Item on its own once clients have pulled
// enough of its bytes (CONTEXT.md: Watched), the user can toggle the
// mark in the TUI, and the state survives restarts in a JSON mirror
// inside the Library (.diadem/watched.json). The in-memory map is the
// single source of truth — the file is always rewritten whole, never
// patched.
package watch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/pandaCaffeine/diadem/internal/library"
	"github.com/pandaCaffeine/diadem/internal/logx"
)

// WatchedPercent is the share of an Item's size (in percent) that must
// reach clients before Diadem calls it Watched on its own. A constant,
// not a flag: tune it here after watching real playback.
const WatchedPercent = 30

// formatVersion guards the state file against future format changes.
const formatVersion = 1

// flushDelay is how long a change waits before it reaches the disk —
// playback produces a burst of byte reports and a burst of marks would
// otherwise rewrite the file per byte chunk.
const flushDelay = 2 * time.Second

// Key identifies one media file across restarts: its path relative to
// the Library root (the ContentDirectory ObjectID form) plus its size.
// The size guards against a different file reusing the path: a replaced
// or re-ripped Item starts unwatched.
type Key struct {
	RelPath string
	Size    int64
}

// Entry is the persisted fact about one Item.
type Entry struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	WatchedAt int64  `json:"watched_at"` // unix seconds
}

// stateFile is the on-disk shape of the mirror.
type stateFile struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Tracker owns the Watch State. All methods are safe for concurrent
// use: ObserveBytes comes from HTTP handler goroutines, Prune from the
// scan path, Toggle from the TUI.
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

// Load reads the mirror. Best effort, like Metadata: a missing file is a
// normal first run, a corrupt one starts empty and gets overwritten by
// the next flush — neither is an error.
func (t *Tracker) Load() {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := os.ReadFile(t.file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// First run (or the user deleted the file): make sure the
			// mirror appears, so the user can see and reset the state.
			t.dirty = true
			t.armTimerLocked()
			return
		}
		t.logger.Logf("[watch] cannot read %s: %v", t.file, err)
		return
	}

	var sf stateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.logger.Logf("[watch] %s is corrupt — starting empty: %v", t.file, err)
		t.dirty = true
		t.armTimerLocked()
		return
	}
	if sf.Version != formatVersion {
		t.logger.Logf("[watch] %s has format version %d, want %d — starting empty", t.file, sf.Version, formatVersion)
		t.dirty = true
		t.armTimerLocked()
		return
	}
	for _, e := range sf.Entries {
		if e.Path == "" || e.Size <= 0 {
			continue
		}
		t.watched[Key{RelPath: e.Path, Size: e.Size}] = e.WatchedAt
	}
	t.logger.Logf("[watch] loaded %d watched entries", len(t.watched))
}

// Watched reports whether the Item is Watched.
func (t *Tracker) Watched(relPath string, size int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.watched[Key{RelPath: relPath, Size: size}]
	return ok
}

// MarkedAt reports when the Item became Watched, in unix seconds.
func (t *Tracker) MarkedAt(relPath string, size int64) (int64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	at, ok := t.watched[Key{RelPath: relPath, Size: size}]
	return at, ok
}

// ToggleWatched flips the mark by hand: the TUI's `w`. A manual mark
// sticks exactly like an automatic one; unmarking clears the byte
// counter too, so playback can earn the mark back from zero.
func (t *Tracker) ToggleWatched(relPath string, size int64) {
	if size <= 0 {
		return
	}
	k := Key{RelPath: relPath, Size: size}
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
func (t *Tracker) ObserveBytes(relPath string, size, delivered int64) {
	if size <= 0 || delivered <= 0 {
		return
	}
	k := Key{RelPath: relPath, Size: size}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.watched[k]; ok {
		return
	}
	t.reading[k] += delivered
	if t.reading[k] < watchedBytes(size) {
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

// Flush rewrites the mirror when something changed since the last one.
// Called on graceful shutdown; the debounced timer flushes on its own.
func (t *Tracker) Flush() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.saveLocked()
}

// saveLocked rewrites the mirror atomically: full temp file, then a
// rename, so a crash never leaves a half-written state behind.
func (t *Tracker) saveLocked() error {
	if !t.dirty {
		return nil
	}
	entries := make([]Entry, 0, len(t.watched))
	for k, at := range t.watched {
		entries = append(entries, Entry{Path: k.RelPath, Size: k.Size, WatchedAt: at})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	data, err := json.MarshalIndent(stateFile{Version: formatVersion, Entries: entries}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(t.file)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := t.file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, t.file); err != nil {
		return err
	}
	t.dirty = false
	t.logger.Logf("[watch] saved %d entries", len(entries))
	return nil
}

// armTimerLocked (re)starts the debounced flush. Called with t.mu held.
func (t *Tracker) armTimerLocked() {
	if t.timer == nil {
		t.timer = time.AfterFunc(flushDelay, t.flushDue)
		return
	}
	t.timer.Reset(flushDelay)
}

func (t *Tracker) flushDue() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.saveLocked(); err != nil {
		t.logger.Logf("[watch] cannot save %s: %v", t.file, err)
	}
}
