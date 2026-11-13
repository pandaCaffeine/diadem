package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRingIsBounded(t *testing.T) {
	r, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < ringCap+50; i++ {
		r.Logf("line %d", i)
	}
	got := r.Recent()
	if len(got) != ringCap {
		t.Fatalf("Recent() = %d lines, want %d", len(got), ringCap)
	}
	// The oldest survivors start right after the dropped head.
	if got[0] != "line 50" && !strings.HasSuffix(got[0], " line 50") {
		t.Errorf("oldest line = %q, want the one after the dropped head", got[0])
	}
}

func TestFileMirror(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diadem.log")
	r, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	r.Logf("hello %s", "tv")
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello tv") {
		t.Errorf("log file = %q, want it to contain the line", data)
	}
}

func TestNewFileError(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "missing-dir", "x.log")); err == nil {
		t.Error("opening a log under a missing folder should fail")
	}
}

func TestNilSafety(t *testing.T) {
	var r *Recorder
	r.Logf("nobody hears me")
	if lines := r.Recent(); lines != nil {
		t.Errorf("nil Recorder Recent() = %v, want nil", lines)
	}
	if err := r.Close(); err != nil {
		t.Errorf("nil Recorder Close() = %v", err)
	}
}

func TestNopLogger(t *testing.T) {
	var l Logger = NopLogger{}
	l.Logf("discarded") // must not panic
}
