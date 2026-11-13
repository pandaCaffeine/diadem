package httpd

import (
	"errors"
	"strconv"
	"strings"
)

// errUnsatisfiable marks a syntactically valid Range that asks for bytes
// the file does not have: the caller must answer 416.
var errUnsatisfiable = errors.New("range not satisfiable")

// httpRange is a byte range, inclusive on both ends.
type httpRange struct {
	start, end int64
}

// parseRange parses a Range header for a resource of the given size,
// single ranges only (RFC 7233). Results:
//
//	rng, true,  nil   serve this range as 206
//	_,   false, nil   ignore the header (absent, malformed, multi-range,
//	                  or an invalid byte-range-spec) and answer 200
//	_,   false, err   valid but unsatisfiable: answer 416
func parseRange(header string, size int64) (rng httpRange, satisfiable bool, err error) {
	if header == "" {
		return rng, false, nil
	}
	const prefix = "bytes="
	if !strings.HasPrefix(header, prefix) {
		return rng, false, nil
	}
	spec := strings.TrimPrefix(header, prefix)
	if strings.Contains(spec, ",") { // multi-range: serve the whole file instead
		return rng, false, nil
	}
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return rng, false, nil
	}
	startStr := strings.TrimSpace(spec[:dash])
	endStr := strings.TrimSpace(spec[dash+1:])

	if startStr == "" { // suffix form: "bytes=-N" — the last N bytes
		n, perr := strconv.ParseInt(endStr, 10, 64)
		if perr != nil || n < 0 {
			return rng, false, nil
		}
		if n > size {
			n = size
		}
		if n == 0 { // "bytes=-0" per RFC 7233 §2.1, or an empty file
			return rng, false, errUnsatisfiable
		}
		return httpRange{start: size - n, end: size - 1}, true, nil
	}

	start, perr := strconv.ParseInt(startStr, 10, 64)
	if perr != nil || start < 0 {
		return rng, false, nil
	}
	end := size - 1
	if endStr != "" {
		end, perr = strconv.ParseInt(endStr, 10, 64)
		if perr != nil || end < start { // invalid byte-range-spec: ignore it
			return rng, false, nil
		}
	}
	if start >= size { // past EOF (covers empty files too)
		return rng, false, errUnsatisfiable
	}
	if end >= size {
		end = size - 1
	}
	return httpRange{start: start, end: end}, true, nil
}
