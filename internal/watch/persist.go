package watch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// formatVersion guards the state file against future format changes.
// Version 2 added the mtime to each entry: a v1 mirror (path+size keys)
// starts empty once and rewrites itself — the old keys cannot be
// trusted to name the same files.
const formatVersion = 2

// flushDelay is how long a change waits before it reaches the disk —
// playback produces a burst of byte reports and a burst of marks would
// otherwise rewrite the file per byte chunk.
const flushDelay = 2 * time.Second

// Entry is the persisted fact about one Item.
type Entry struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	ModTime   int64  `json:"mtime"`      // unix seconds, 0 when unknown
	WatchedAt int64  `json:"watched_at"` // unix seconds
}

// stateFile is the on-disk shape of the mirror.
type stateFile struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
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
		t.watched[Key{RelPath: e.Path, Size: e.Size, ModTime: e.ModTime}] = e.WatchedAt
	}
	t.logger.Logf("[watch] loaded %d watched entries", len(t.watched))
}

// Flush rewrites the mirror when something changed since the last one.
// Called on graceful shutdown; the debounced timer flushes on its own.
func (t *Tracker) Flush() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.saveLocked()
}

// saveLocked rewrites the mirror durably: a full temp file fsynced to
// disk, then a rename, then a directory fsync — a crash can neither
// publish a half-written state nor lose an already-published one.
func (t *Tracker) saveLocked() error {
	if !t.dirty {
		return nil
	}
	entries := make([]Entry, 0, len(t.watched))
	for k, at := range t.watched {
		entries = append(entries, Entry{Path: k.RelPath, Size: k.Size, ModTime: k.ModTime, WatchedAt: at})
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
	if err := writeFileSync(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, t.file); err != nil {
		return err
	}
	syncDir(dir) // best effort: make the rename itself durable
	t.dirty = false
	t.logger.Logf("[watch] saved %d entries", len(entries))
	return nil
}

// writeFileSync writes and fsyncs: once it returns, the bytes are on
// disk, so the rename on top can never publish a short file.
func writeFileSync(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// syncDir fsyncs a directory — the same trick databases use to make a
// rename survive a crash. Best effort: some filesystems refuse it.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
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
