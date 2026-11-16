package library

import (
	"path/filepath"
	"sync/atomic"

	"github.com/pandaCaffeine/diadem/internal/logx"
)

// Store owns the Library tree: it rescans the disk and keeps the
// freshest snapshot as the single source of truth for the TUI and the
// DLNA ContentDirectory alike. The probe cache lives here, so metadata
// survives rescans and only changed files are re-read.
type Store struct {
	root   string
	probe  *probeCache
	logger logx.Logger
	snap   atomic.Pointer[Library]
}

// NewStore builds a Store for the Library at root. A nil logger drops
// diagnostics.
func NewStore(root string, logger logx.Logger) *Store {
	if logger == nil {
		logger = logx.NopLogger{}
	}
	return &Store{root: root, probe: newProbeCache(), logger: logger}
}

// Root is the absolute Library path.
func (s *Store) Root() string { return filepath.Clean(s.root) }

// Scan rescans the Library and publishes the snapshot. Readers keep
// seeing the previous snapshot until the new one is swapped in
// atomically; a failed scan leaves the old one in place.
func (s *Store) Scan() (*Library, error) {
	lib, err := scanStore(s.root, s.probe, s.logger)
	if err == nil {
		s.snap.Store(lib)
	}
	return lib, err
}

// Current is the freshest successfully scanned snapshot, nil before the
// first scan completes.
func (s *Store) Current() *Library { return s.snap.Load() }
