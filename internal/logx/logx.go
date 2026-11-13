// Package logx is Diadem's little logger: a bounded in-memory ring that
// powers the TUI Logs tab, optionally mirrored to a file when --debug is
// on. Methods are safe for concurrent use and nil-safe.
package logx

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// ringCap is how many recent lines the Logs tab can look back at.
const ringCap = 300

// Logger is the logging capability the other packages depend on.
type Logger interface {
	Logf(format string, args ...any)
}

// NopLogger discards everything.
type NopLogger struct{}

func (NopLogger) Logf(string, ...any) {}

// Recorder keeps recent lines in memory and, when built with a file path,
// appends everything there too. A nil *Recorder records nothing.
type Recorder struct {
	mu    sync.Mutex
	lines []string
	file  *os.File
}

// New builds a Recorder. An empty path means memory only.
func New(path string) (*Recorder, error) {
	r := &Recorder{}
	if path == "" {
		return r, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	r.file = f
	return r, nil
}

// Logf records one timestamped line. Nil-safe.
func (r *Recorder) Logf(format string, args ...any) {
	if r == nil {
		return
	}
	line := time.Now().Format("15:04:05") + " " + fmt.Sprintf(format, args...)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	if len(r.lines) > ringCap {
		r.lines = r.lines[len(r.lines)-ringCap:]
	}
	if r.file != nil {
		_, _ = fmt.Fprintln(r.file, line)
	}
}

// Recent returns the remembered lines, oldest first. Nil-safe.
func (r *Recorder) Recent() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}

// Close releases the log file, if any. Nil-safe.
func (r *Recorder) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		err := r.file.Close()
		r.file = nil
		return err
	}
	return nil
}

var _ Logger = (*Recorder)(nil)
