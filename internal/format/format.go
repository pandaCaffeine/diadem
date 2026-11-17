// Package format reads video container metadata — the title and the
// duration — straight from file headers, without external tools. Three
// container families are understood: MP4/MOV (.mp4, .m4v, .mov),
// Matroska (.mkv, .webm) and AVI (.avi).
//
// Parsers read only a few kilobytes per file: they walk box/chunk
// structures by header, seeking over payloads. A file they cannot make
// sense of is reported as an error; the caller decides whether that
// matters (metadata is best-effort, never fatal — a file Diadem cannot
// probe is still served).
package format

import (
	"errors"
	"path/filepath"
	"strings"
	"time"
)

// maxTitleLen caps how many bytes of a title string are accepted;
// anything longer is cut rather than stored.
const maxTitleLen = 1024

// Meta is the metadata extracted from a container's headers.
type Meta struct {
	// Title is the container-declared title, "" when absent.
	Title string
	// Duration is the playback duration; 0 when unknown.
	Duration time.Duration
}

// ErrUnsupported reports a video file whose container format Diadem does
// not parse. The file itself is fine and still served.
var ErrUnsupported = errors.New("unsupported video container format")

// Read extracts Meta from the file at path, dispatching on the file
// extension the same way the rest of Diadem does (see httpd/mime.go).
// Each parser still verifies the container's structure, so a mislabeled
// extension surfaces as a parse error, not as wrong data.
func Read(path string) (Meta, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".m4v", ".mov":
		return readMP4(path)
	case ".mkv", ".webm":
		return readMKV(path)
	case ".avi":
		return readAVI(path)
	default:
		return Meta{}, ErrUnsupported
	}
}
