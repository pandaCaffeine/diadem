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
// the Library root (the ContentDirectory ObjectID form), its size and
// its modification time. Size and mtime together guard against a
// different file reusing the path: a replaced or re-ripped Item starts
// unwatched, even when the replacement happens to be the same size.
type Key struct {
	RelPath string
	Size    int64
	ModTime int64 // unix seconds; 0 when unknown
}

// KeyFor derives the Watch State key of a scanned Item from its path
// relative to the Library root. One derivation, shared by the TUI, the
// ContentDirectory marker and Prune, so all three speak the same
// identity.
func KeyFor(relPath string, n *library.Node) Key {
	k := Key{RelPath: relPath, Size: n.Size}
	if !n.ModTime.IsZero() {
		k.ModTime = n.ModTime.Unix()
	}
	return k
}

// Tracker owns the Watch State. All methods are safe for concurrent
// use: ObserveBytes comes from HTTP handler goroutines, Prune from the
// scan path, Toggle from the TUI. The on-disk mirror lives in
// persist.go.
type Tracker struct {
	mu      sync.Mutex
	file    string
	logger  logx.Logger
	watched map[Key]int64     // WatchedAt, unix seconds
	reading map[Key]*progress // served-byte coverage of not-yet-Watched Items
	dirty   bool              // mirror needs a rewrite
	timer   *time.Timer       // debounced flush; nil until first change
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
		reading: make(map[Key]*progress),
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

// ObserveBytes reports a served byte range of an Item — [start,
// start+delivered) — merged with every range served before it, across
// requests and clients on the LAN. Two things earn the mark: distinct
// bytes covering the WatchedPercent share (a re-watched span never
// counts twice), and byte 0 among them, so a seek straight to the
// ending does not pass for a watched episode. Watched Items stop
// counting: the counter only ever earns the mark, never loses it.
func (t *Tracker) ObserveBytes(k Key, start, delivered int64) {
	if k.Size <= 0 || delivered <= 0 || start < 0 {
		return
	}
	lo, hi := start, start+delivered
	if hi > k.Size {
		hi = k.Size // a hostile or buggy report cannot cross the size
	}
	if lo >= hi {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.watched[k]; ok {
		return
	}
	p := t.reading[k]
	if p == nil {
		p = &progress{}
		t.reading[k] = p
	}
	p.add(lo, hi)
	if !p.startedAtZero() || p.covered() < watchedBytes(k.Size) {
		return
	}
	delete(t.reading, k)
	t.watched[k] = time.Now().Unix()
	t.logger.Logf("[watch] marked %s", k.RelPath)
	t.dirty = true
	t.armTimerLocked()
}

// span is a served byte range [lo, hi) of an Item.
type span struct{ lo, hi int64 }

// spanLimit caps how many disjoint spans one Item keeps: a pathological
// client could carve thousands of gaps into the stream, and playback
// never comes close.
const spanLimit = 4096

// progress is how much of a not-yet-Watched Item has left the server:
// the union of served byte ranges, kept merged and sorted.
type progress struct {
	spans []span
}

// add merges [lo, hi) into the covered set.
func (p *progress) add(lo, hi int64) {
	i := 0
	for i < len(p.spans) && p.spans[i].hi < lo { // before the new span
		i++
	}
	j := i
	for j < len(p.spans) && p.spans[j].lo <= hi { // touching or overlapping
		if p.spans[j].lo < lo {
			lo = p.spans[j].lo
		}
		if p.spans[j].hi > hi {
			hi = p.spans[j].hi
		}
		j++
	}
	// Build fresh: writing into p.spans[:i] below would clobber the
	// p.spans[j:] tail when j == i (a pure insert before everything).
	out := make([]span, 0, len(p.spans)+1)
	out = append(out, p.spans[:i]...)
	out = append(out, span{lo, hi})
	out = append(out, p.spans[j:]...)
	p.spans = out
	if len(p.spans) > spanLimit {
		p.spans = []span{{p.spans[0].lo, p.spans[len(p.spans)-1].hi}}
	}
}

// covered is the count of distinct bytes served so far.
func (p *progress) covered() int64 {
	var n int64
	for _, s := range p.spans {
		n += s.hi - s.lo
	}
	return n
}

// startedAtZero reports whether byte 0 was ever served: playback that
// begins with a seek must not earn the mark.
func (p *progress) startedAtZero() bool {
	return len(p.spans) > 0 && p.spans[0].lo == 0
}

// watchedBytes is the byte count that earns the mark: a plain integer
// form of size · WatchedPercent / 100.
func watchedBytes(size int64) int64 {
	return size * WatchedPercent / 100
}

// Prune drops entries whose file is gone from the Library. An entry
// dies only when its parent Container is present in the fresh snapshot
// while an Item with the same path, size and mtime is not — so a
// renamed, deleted or replaced file forgets its mark, but a whole
// missing region of the tree (an unplugged drive, a flaky mount) keeps
// its entries.
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
			items[KeyFor(rel, n)] = true
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
