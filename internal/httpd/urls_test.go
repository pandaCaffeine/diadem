package httpd

import (
	"net"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilePathToURL(t *testing.T) {
	root := "/Users/nikita/Movies"
	cases := map[string]string{
		filepath.Join(root, "movie.mkv"):          "/files/movie.mkv",
		filepath.Join(root, "Big Buck Bunny.mkv"): "/files/Big%20Buck%20Bunny.mkv",
		filepath.Join(root, "movies/Игрушка.mp4"): "/files/movies/%D0%98%D0%B3%D1%80%D1%83%D1%88%D0%BA%D0%B0.mp4",
		filepath.Join(root, "a/100% cool.mkv"):    "/files/a/100%25%20cool.mkv",
		filepath.Join(root, "../../etc/passwd"):   "", // outside the Library
	}
	for path, want := range cases {
		if got := FilePathToURL(root, path); got != want {
			t.Errorf("FilePathToURL(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestLocalIPv4(t *testing.T) {
	ip := net.ParseIP(LocalIPv4())
	if ip == nil || ip.To4() == nil {
		t.Errorf("LocalIPv4() = %q, want a valid IPv4", LocalIPv4())
	}
}

func TestURLRoundTrip(t *testing.T) {
	// A URL we generate must resolve back on the server path rules.
	root := t.TempDir()
	s := New(root)
	for _, name := range []string{"movie.mkv", "Big Buck Bunny.mkv", "кино/серия 01.mkv"} {
		abs := filepath.Join(root, filepath.FromSlash(name))
		urlPath := FilePathToURL(root, abs)
		if urlPath == "" {
			t.Fatalf("FilePathToURL failed for %q", name)
		}
		if _, ok := s.resolve(urlPath); !ok {
			t.Errorf("resolve(%q) rejected a URL we generated for %q", urlPath, name)
		}
	}
	if !strings.HasPrefix(FilePathToURL(root, filepath.Join(root, "x.mkv")), "/files/") {
		t.Error("generated URL lacks the /files/ prefix")
	}
}
