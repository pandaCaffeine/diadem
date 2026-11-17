package library

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pandaCaffeine/diadem/internal/logx"
)

// tinyMP4 writes a minimal, structurally valid MP4: ftyp + moov(mvhd)
// with the given timescale and duration ticks. Just enough for the
// format package's parser (and ffprobe) to read the duration.
func tinyMP4(t *testing.T, path string, timescale, ticks uint32) {
	t.Helper()
	payload := make([]byte, 80) // version/flags, timestamps, …, next track id
	binary.BigEndian.PutUint32(payload[12:16], timescale)
	binary.BigEndian.PutUint32(payload[16:20], ticks)
	mvhd := make([]byte, 8, 8+80)
	binary.BigEndian.PutUint32(mvhd[0:4], uint32(8+80))
	copy(mvhd[4:8], "mvhd")
	mvhd = append(mvhd, payload...)
	moov := make([]byte, 8, 8+len(mvhd))
	binary.BigEndian.PutUint32(moov[0:4], uint32(8+len(mvhd)))
	copy(moov[4:8], "moov")
	moov = append(moov, mvhd...)
	ftyp := make([]byte, 16)
	binary.BigEndian.PutUint32(ftyp[0:4], 16)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:16], "isom\x00\x00\x00\x00")
	data := append(ftyp, moov...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// mustScan runs scanStore with a recorder attached.
func mustScan(t *testing.T, root string, cache *probeCache, rec *logx.Recorder) *Library {
	t.Helper()
	lib, err := scanStore(root, cache, rec)
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

// firstItem returns the first Item child of the root.
func firstItem(t *testing.T, lib *Library) *Node {
	t.Helper()
	for _, c := range lib.Root.Children {
		if c.Kind == Item {
			return c
		}
	}
	t.Fatal("no items in the scanned library")
	return nil
}

func TestScanProbesVideoMetadata(t *testing.T) {
	dir := t.TempDir()
	tinyMP4(t, filepath.Join(dir, "clip.mp4"), 1000, 5500) // 5.5s

	lib := mustScan(t, dir, nil, nil)
	n := firstItem(t, lib)
	if n.Media != MediaVideo {
		t.Fatalf("media = %v, want video", n.Media)
	}
	if n.Meta.Duration != 5500*time.Millisecond {
		t.Errorf("Meta.Duration = %v, want 5.5s", n.Meta.Duration)
	}
	if n.Meta.Title != "" {
		t.Errorf("Meta.Title = %q, want empty", n.Meta.Title)
	}

	// A folder scan must not slow down for fake files it cannot parse:
	// they simply carry no metadata.
	write(t, filepath.Join(dir, "fake.mkv"), 10)
	lib = mustScan(t, dir, nil, nil)
	found := false
	for _, c := range lib.Root.Children {
		if c.Name == "fake.mkv" {
			found = true
			if c.Meta.Duration != 0 || c.Meta.Title != "" {
				t.Errorf("fake.mkv meta = %+v, want empty", c.Meta)
			}
		}
	}
	if !found {
		t.Fatal("fake.mkv missing from the tree")
	}
}

func TestProbeFailureLogsOnce(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "broken.mkv"), 10)

	rec, err := logx.New("")
	if err != nil {
		t.Fatal(err)
	}
	cache := newProbeCache()
	mustScan(t, dir, cache, rec)
	mustScan(t, dir, cache, rec)
	mustScan(t, dir, cache, rec)

	// Count [meta] lines across all three scans: the cache must make it
	// exactly one.
	var hits int
	for _, ln := range rec.Recent() {
		if strings.Contains(ln, "[meta]") {
			hits++
		}
	}
	if hits != 1 {
		t.Errorf("got %d [meta] lines over three scans, want 1 (failure must be cached):\n%v",
			hits, rec.Recent())
	}
}

func TestProbeCacheReprobesChangedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	tinyMP4(t, path, 1000, 5500) // 5.5s
	// Force a distinct mtime for the second write below.
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}

	cache := newProbeCache()
	lib := mustScan(t, dir, cache, nil)
	if got := firstItem(t, lib).Meta.Duration; got != 5500*time.Millisecond {
		t.Fatalf("first probe = %v, want 5.5s", got)
	}

	// The file changes: duration 9s now, mtime fresh.
	tinyMP4(t, path, 1000, 9000)
	now := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	lib = mustScan(t, dir, cache, nil)
	if got := firstItem(t, lib).Meta.Duration; got != 9*time.Second {
		t.Errorf("reprobe = %v, want 9s", got)
	}

	// An unchanged file keeps its cached metadata without re-reading.
	lib = mustScan(t, dir, cache, nil)
	if got := firstItem(t, lib).Meta.Duration; got != 9*time.Second {
		t.Errorf("cached = %v, want 9s", got)
	}
}

func TestProbeImplausibleDuration(t *testing.T) {
	dir := t.TempDir()
	tinyMP4(t, filepath.Join(dir, "borked.mp4"), 1000, 1000*60*60*25) // 25h

	rec, err := logx.New("")
	if err != nil {
		t.Fatal(err)
	}
	lib := mustScan(t, dir, newProbeCache(), rec)
	n := firstItem(t, lib)
	if n.Meta.Duration != 0 {
		t.Errorf("Duration = %v, want 0 (over the %v cap)", n.Meta.Duration, maxVideoDuration)
	}
	joined := ""
	for _, ln := range rec.Recent() {
		joined += ln + "\n"
	}
	if !strings.Contains(joined, "implausible duration") {
		t.Errorf("log misses the implausible-duration line:\n%s", joined)
	}
}
