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
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
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

// readAt reads up to n bytes at off; a short read at EOF is fine.
func readAt(f io.ReaderAt, off int64, n int64) ([]byte, error) {
	if n <= 0 {
		return nil, nil
	}
	b := make([]byte, n)
	m, err := f.ReadAt(b, off)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return b[:m], nil
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// decodeText cleans up a raw text payload: UTF-16 with BOM is decoded,
// invalid bytes are dropped, NUL padding and surrounding space trimmed.
// An empty or over-long result is treated as "no title".
func decodeText(b []byte) string {
	if len(b) >= 2 && (b[0] == 0xFF && b[1] == 0xFE || b[0] == 0xFE && b[1] == 0xFF) {
		be := b[0] == 0xFE && b[1] == 0xFF
		u := make([]uint16, 0, (len(b)-2)/2)
		for i := 2; i+1 < len(b); i += 2 {
			if be {
				u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
			} else {
				u = append(u, uint16(b[i+1])<<8|uint16(b[i]))
			}
		}
		b = []byte(string(utf16.Decode(u)))
	}
	s := strings.ToValidUTF8(string(b), "")
	s = strings.Trim(strings.TrimRight(s, "\x00"), " \t\r\n")
	if utf8.RuneCountInString(s) > maxTitleLen {
		s = string([]rune(s)[:maxTitleLen])
	}
	return s
}
