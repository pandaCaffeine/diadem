package library

import (
	"os"
	"sync"
	"time"

	"github.com/pandaCaffeine/diadem/internal/format"
	"github.com/pandaCaffeine/diadem/internal/logx"
)

// maxVideoDuration caps trusted container durations: a header claiming
// more than a day is garbage, and "unknown" beats "wrong".
const maxVideoDuration = 24 * time.Hour

// probeWorkers is how many files are probed in parallel. Header reads
// are seek-heavy but tiny, so a small pool keeps a thousand-file library
// well under a second even on spinning disks.
const probeWorkers = 4

// probeEntry is one cache record: the metadata plus the stat the file
// had when it was probed.
type probeEntry struct {
	size  int64
	mtime time.Time
	meta  format.Meta
}

// probeCache remembers Meta per file across rescans: the key is the
// absolute path, a hit requires size and mtime to match. Failed probes
// are cached too, so a file Diadem cannot read logs one [meta] line,
// not one per rescan.
type probeCache struct {
	mu    sync.Mutex
	files map[string]probeEntry
}

func newProbeCache() *probeCache {
	return &probeCache{files: make(map[string]probeEntry)}
}

// meta returns the metadata for a video Item, probing on a cache miss
// (that is: on first sight, or after the file changed on disk).
func (c *probeCache) meta(path string, size int64, mtime time.Time, logger logx.Logger) format.Meta {
	c.mu.Lock()
	if e, hit := c.files[path]; hit && e.size == size && e.mtime.Equal(mtime) {
		c.mu.Unlock()
		return e.meta
	}
	c.mu.Unlock()

	meta := probeFile(path, logger)

	c.mu.Lock()
	c.files[path] = probeEntry{size: size, mtime: mtime, meta: meta}
	c.mu.Unlock()
	return meta
}

// probeFile reads the container headers of one video file. Any failure
// — an unsupported format, an unreadable or corrupt container — logs a
// single [meta] line and yields empty Meta; it never fails the scan.
func probeFile(path string, logger logx.Logger) format.Meta {
	meta, err := format.Read(path)
	switch {
	case err != nil:
		logger.Logf("[meta] %s: cannot read video metadata: %v", path, err)
		return format.Meta{}
	case meta.Duration < 0 || meta.Duration > maxVideoDuration:
		logger.Logf("[meta] %s: implausible duration %v, ignoring it", path, meta.Duration)
		meta.Duration = 0
	}
	return meta
}

// probeVideos fills Meta for every video Item of the tree, in parallel.
// Nodes are fresh from the walk and not published yet, so writing them
// here races with nobody.
func probeVideos(root *Node, cache *probeCache, logger logx.Logger) {
	var videos []*Node
	var walk func(n *Node)
	walk = func(n *Node) {
		if n.Kind == Item {
			if n.Media == MediaVideo {
				videos = append(videos, n)
			}
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	if len(videos) == 0 {
		return
	}
	if logger == nil {
		logger = logx.NopLogger{}
	}

	sem := make(chan struct{}, probeWorkers)
	var wg sync.WaitGroup
	for _, n := range videos {
		wg.Add(1)
		go func(n *Node) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var mtime time.Time
			if fi, err := os.Stat(n.Path); err == nil {
				mtime = fi.ModTime()
			}
			if cache != nil {
				n.Meta = cache.meta(n.Path, n.Size, mtime, logger)
				return
			}
			n.Meta = probeFile(n.Path, logger)
		}(n)
	}
	wg.Wait()
}
