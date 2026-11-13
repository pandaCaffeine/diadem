package library

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// debounceDelay is the quiet period after the first filesystem event
// before the change is reported. Copying a file bursts with write events;
// we only care once things settle.
const debounceDelay = 500 * time.Millisecond

// Watcher watches the Library tree and reports changes, debounced.
// fsnotify is not recursive, so every visible folder gets a watch of its
// own. A signal means "rescan": Browse and file serving read the disk
// live anyway, so a fresh scan is all it takes to show new files
// everywhere at once.
type Watcher struct {
	fsw   *fsnotify.Watcher
	root  string
	stops chan struct{}

	changed   chan struct{}
	stopped   chan struct{}
	closeOnce sync.Once
}

// NewWatcher starts watching root and everything visible under it.
func NewWatcher(root string) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := fsw.Add(root); err != nil {
		_ = fsw.Close()
		return nil, err
	}
	w := &Watcher{
		fsw:     fsw,
		root:    root,
		stops:   make(chan struct{}),
		changed: make(chan struct{}, 1),
		stopped: make(chan struct{}),
	}
	go w.loop()
	return w, nil
}

// Changed yields a signal whenever the Library changed. The channel is
// buffered and sends coalesce: a burst of events becomes one rescan.
func (w *Watcher) Changed() <-chan struct{} { return w.changed }

// Close stops watching. Safe to call more than once.
func (w *Watcher) Close() error {
	var err error
	w.closeOnce.Do(func() {
		close(w.stops)
		err = w.fsw.Close()
	})
	<-w.stopped
	return err
}

// loop is the single goroutine that owns the fsnotify watcher: it debounces
// event bursts and keeps the per-folder watches in sync with the tree.
func (w *Watcher) loop() {
	defer close(w.stopped)
	var debounce <-chan time.Time
	for {
		select {
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if ev.Op&fsnotify.Create != 0 {
				w.watchDirs(ev.Name) // a new folder needs watches of its own
			}
			if debounce == nil {
				// First event of a burst starts the clock; further events
				// during the window are covered by the rescan.
				debounce = time.After(debounceDelay)
			}
		case <-debounce:
			debounce = nil
			w.notify()
			w.watchDirs(w.root) // sync watches with what the rescan will see
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			} // filesystem errors are non-fatal: miss events, not the app
		case <-w.stops:
			return
		}
	}
}

func (w *Watcher) notify() {
	select {
	case w.changed <- struct{}{}:
	default: // a signal is already pending; consumers coalesce
	}
}

// watchDirs adds watches for start and every visible folder below it.
// Re-adding an existing watch is a no-op, which makes this a cheap way to
// recover watches for recreated or deeply nested folders. Errors on
// individual folders are ignored: unreadable folders are invisible anyway.
func (w *Watcher) watchDirs(start string) {
	if info, err := os.Stat(start); err != nil || !info.IsDir() {
		return
	}
	_ = filepath.WalkDir(start, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if path != w.root && path != start && strings.HasPrefix(d.Name(), ".") {
			return fs.SkipDir
		}
		_ = w.fsw.Add(path)
		return nil
	})
}
