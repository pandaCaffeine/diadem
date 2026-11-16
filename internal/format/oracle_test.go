package format

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// durationTolerance is how far a parser duration may drift from
// ffprobe's: container math is exact, but ffprobe sometimes derives the
// value from streams rather than headers.
const durationTolerance = 2 * time.Second

// ffprobeMissing reports whether the optional oracle binary is absent.
func ffprobeMissing(t *testing.T) bool {
	t.Helper()
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed — skipping the oracle check")
	}
	return false
}

// probeWithFFProbe asks ffprobe for a file's duration and title. The
// oracle is optional by design: Diadem itself never shells out.
func probeWithFFProbe(t *testing.T, path string) (time.Duration, string, bool) {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error",
		"-show_entries", "format=duration:format_tags=title",
		"-of", "json", path).Output()
	if err != nil {
		return 0, "", false
	}
	var j struct {
		Format struct {
			Duration string `json:"duration"`
			Tags     struct {
				Title string `json:"title"`
			} `json:"tags"`
		} `json:"format"`
	}
	if json.Unmarshal(out, &j) != nil || j.Format.Duration == "" {
		return 0, "", false
	}
	sec, err := strconv.ParseFloat(j.Format.Duration, 64)
	if err != nil {
		return 0, "", false
	}
	return time.Duration(sec * float64(time.Second)), j.Format.Tags.Title, true
}

// checkAgainstFFProbe compares a parser result with the oracle.
func checkAgainstFFProbe(t *testing.T, path string, meta Meta) {
	t.Helper()
	probed, probedTitle, ok := probeWithFFProbe(t, path)
	if !ok {
		t.Skipf("ffprobe produced no duration for %s", filepath.Base(path))
	}
	if d := abs64(int64(meta.Duration - probed)); d > int64(durationTolerance) {
		t.Errorf("%s: Duration = %v, ffprobe says %v", filepath.Base(path), meta.Duration, probed)
	}
	if probedTitle != "" && meta.Title != probedTitle {
		t.Errorf("%s: Title = %q, ffprobe says %q", filepath.Base(path), meta.Title, probedTitle)
	}
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// TestOracleSyntheticFFProbe cross-checks the hand-built fixtures with
// ffprobe. Fixtures too minimal for the demuxer skip gracefully — the
// parser-only tests in format_test.go still cover them.
func TestOracleSyntheticFFProbe(t *testing.T) {
	if ffprobeMissing(t) {
		return
	}
	cases := []struct {
		name string
		file string
		data []byte
	}{
		{"mp4", "oracle.mp4", buildMP4(1000, 90_500, "Oracle MP4")},
		{"mp4 moov at end", "oracle-end.mp4", buildMP4(1000, 90_500, "Oracle MP4", false, true)},
		{"mkv", "oracle.mkv", buildMKV(1_000_000, 90_500, "Oracle MKV", false)},
		{"mkv unknown size", "oracle-live.mkv", buildMKV(1_000_000, 90_500, "Oracle MKV", true)},
		{"avi", "oracle.avi", buildAVI(40_000, 2262, 0, "Oracle AVI")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeFile(t, c.file, c.data)
			meta, err := Read(path)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			checkAgainstFFProbe(t, path, meta)
		})
	}
}

// TestOracleRealWorldTestdata runs the parser against the real video
// files in the repo's testdata/ folder (gitignored, so its contents are
// machine-specific) and compares with ffprobe. Skipped when either side
// is unavailable.
func TestOracleRealWorldTestdata(t *testing.T) {
	if ffprobeMissing(t) {
		return
	}
	var files []string
	for _, pattern := range []string{"*.mkv", "*.mp4", "*.m4v", "*.mov", "*.webm", "*.avi"} {
		matches, _ := filepath.Glob(filepath.Join("..", "..", "testdata", pattern))
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Skip("no real video files in testdata/ — drop a couple in to exercise this test")
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			meta, err := Read(path)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if meta.Duration <= 0 {
				t.Errorf("Duration = %v, want a real duration from a real file", meta.Duration)
			}
			checkAgainstFFProbe(t, path, meta)
			if testing.Verbose() {
				t.Logf("%s: %v %q", filepath.Base(path), meta.Duration, meta.Title)
			}
		})
	}
}
