// Package httpd serves Library Items over plain HTTP with byte-range
// support — the transport DLNA clients (like VLC) fetch media through.
//
// The server resolves /files/... URLs against the Library root directly on
// disk (hidden files, non-media and anything escaping the root are
// rejected), so it stays correct between rescans without sharing tree
// state with the TUI.
package httpd

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/pandaCaffeine/diadem/internal/library"
	"github.com/pandaCaffeine/diadem/internal/logx"
)

// clientIP extracts the host part of an addr like "192.0.2.4:52341".
func clientIP(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

// StreamObserver reports media file fetches: who (IP, User-Agent) is
// pulling which file. Satisfied structurally by clients.Tracker.
type StreamObserver interface {
	ObserveStream(ip, userAgent, name string)
}

// BytesObserver reports how many bytes of an Item actually reached
// clients — the raw material for marking it Watched (CONTEXT.md).
// Satisfied structurally by watch.Tracker.
type BytesObserver interface {
	ObserveBytes(relPath string, size, delivered int64)
}

// Server serves the Library rooted at root over HTTP.
type Server struct {
	root string

	// Observer, when set, is notified about every served file fetch.
	Observer StreamObserver

	// BytesObserver, when set, is notified about every served byte
	// count of a video Item.
	BytesObserver BytesObserver

	// Logger receives a line per served request (and 416s). Defaults to
	// a no-op logger.
	Logger logx.Logger
}

// New builds a Server for an absolute Library path.
func New(root string) *Server {
	return &Server{root: root, Logger: logx.NopLogger{}}
}

// Handler returns the routed HTTP handler. Step 3 will mount UPnP device
// and ContentDirectory endpoints on the same mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/files/", s.serveFile)
	return mux
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	default:
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rel, ok := s.resolve(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(s.root, rel)
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	if s.Observer != nil {
		s.Observer.ObserveStream(clientIP(r.RemoteAddr), r.UserAgent(), info.Name())
	}

	ip := clientIP(r.RemoteAddr)
	size := info.Size()
	// Only video earns the Watched mark (CONTEXT.md): audio and photos
	// are reported to the stream tracker but never to the byte counter.
	kind, _ := library.KindByExt(info.Name())
	isVideo := kind == library.MediaVideo
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", ContentType(info.Name()))

	rng, satisfiable, rangeErr := parseRange(r.Header.Get("Range"), size)
	switch {
	case rangeErr != nil:
		s.Logger.Logf("[http] %s %s %s range=%q → 416", ip, r.Method, r.URL.Path, r.Header.Get("Range"))
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		http.Error(w, "requested range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
		return
	case satisfiable:
		length := rng.end - rng.start + 1
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rng.start, rng.end, size))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", length))
		w.WriteHeader(http.StatusPartialContent)
		if r.Method == http.MethodHead {
			s.Logger.Logf("[http] %s HEAD %s → 206 %d/%dB", ip, r.URL.Path, length, size)
			return
		}
		n, _ := io.Copy(w, io.NewSectionReader(f, rng.start, length))
		s.Logger.Logf("[http] %s GET %s → 206 %d/%dB", ip, r.URL.Path, n, size)
		s.reportBytes(rel, size, n, isVideo)
	default: // no Range header, or malformed: send the whole file
		w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			s.Logger.Logf("[http] %s HEAD %s → 200 %dB", ip, r.URL.Path, size)
			return
		}
		n, _ := io.Copy(w, f)
		s.Logger.Logf("[http] %s GET %s → 200 %dB", ip, r.URL.Path, n)
		s.reportBytes(rel, size, n, isVideo)
	}
}

// reportBytes tells the Watch State what actually left the server. A
// partial count after an aborted transfer still counts: playback is a
// stream of aborted-and-retried range requests.
func (s *Server) reportBytes(rel string, size, delivered int64, isVideo bool) {
	if s.BytesObserver == nil || !isVideo || delivered <= 0 {
		return
	}
	s.BytesObserver.ObserveBytes(rel, size, delivered)
}

// resolve maps a /files/... URL path to a library-relative filesystem
// path. It rejects empty segments, dot segments ("."/"..", also encoded),
// hidden entries, path separators inside segments and non-media leaf
// names, and double-checks that the joined path stays inside the root.
func (s *Server) resolve(urlPath string) (string, bool) {
	const prefix = "/files/"
	if !strings.HasPrefix(urlPath, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(urlPath, prefix)
	if rest == "" {
		return "", false
	}
	segs := strings.Split(rest, "/")
	for _, seg := range segs {
		if seg == "" || seg == "." || seg == ".." || strings.HasPrefix(seg, ".") {
			return "", false
		}
		if strings.ContainsAny(seg, "\\\x00") {
			return "", false
		}
	}
	if _, ok := library.KindByExt(segs[len(segs)-1]); !ok {
		return "", false
	}
	rel := filepath.Join(segs...)
	abs := filepath.Join(s.root, rel)
	if !strings.HasPrefix(abs, s.root+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}
