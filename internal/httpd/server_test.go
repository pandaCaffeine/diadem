package httpd

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pattern returns deterministic content of the given length.
func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

func newTestServer(t *testing.T) (*httptest.Server, []byte, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "movies"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := pattern(4096)
	files := map[string][]byte{
		"movies/clip.mp4":           body,
		"movies/Big Buck Bunny.mkv": body[:1024],
		"movies/кино/серия 01.mkv":  body[:100],
		"notes.txt":                 body,
		".hidden/secret.mkv":        body,
		"movies/.dotfile.mkv":       body,
	}
	for rel, content := range files {
		abs := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ts := httptest.NewServer(New(dir).Handler())
	t.Cleanup(ts.Close)
	return ts, body, dir
}

func get(t *testing.T, url, rangeHeader string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, data
}

func TestServeFullFile(t *testing.T) {
	ts, body, _ := newTestServer(t)
	resp, data := get(t, ts.URL+"/files/movies/clip.mp4", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(data) != len(body) || string(data) != string(body) {
		t.Error("body does not match the file")
	}
	if got := resp.Header.Get("Content-Type"); got != "video/mp4" {
		t.Errorf("Content-Type = %q, want video/mp4", got)
	}
	if got := resp.Header.Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("Accept-Ranges = %q, want bytes", got)
	}
	if got := resp.Header.Get("Content-Length"); got != "4096" {
		t.Errorf("Content-Length = %q, want 4096", got)
	}
}

func TestServeRanges(t *testing.T) {
	ts, body, _ := newTestServer(t)
	base := ts.URL + "/files/movies/clip.mp4"

	cases := []struct {
		rangeHeader string
		wantStatus  int
		wantStart   int
		wantEnd     int // inclusive; for 200 the whole file
		wantCR      string
	}{
		{"bytes=0-1023", 206, 0, 1023, "bytes 0-1023/4096"},
		{"bytes=1000-", 206, 1000, 4095, "bytes 1000-4095/4096"},
		{"bytes=-100", 206, 3996, 4095, "bytes 3996-4095/4096"},
		{"bytes=0-999999", 206, 0, 4095, "bytes 0-4095/4096"},
		{"bytes=9999-", 416, 0, 0, "bytes */4096"},
		{"bytes=banana", 200, 0, 4095, ""},
		{"bytes=0-10,20-30", 200, 0, 4095, ""},
		{"bytes=10-5", 200, 0, 4095, ""},
	}
	for _, tc := range cases {
		resp, data := get(t, base, tc.rangeHeader)
		if resp.StatusCode != tc.wantStatus {
			t.Errorf("Range %q: status = %d, want %d", tc.rangeHeader, resp.StatusCode, tc.wantStatus)
			continue
		}
		if got := resp.Header.Get("Content-Range"); got != tc.wantCR {
			t.Errorf("Range %q: Content-Range = %q, want %q", tc.rangeHeader, got, tc.wantCR)
		}
		if tc.wantStatus == 206 {
			wantLen := tc.wantEnd - tc.wantStart + 1
			if len(data) != wantLen || string(data) != string(body[tc.wantStart:tc.wantEnd+1]) {
				t.Errorf("Range %q: got %d bytes, want %d matching bytes", tc.rangeHeader, len(data), wantLen)
			}
			if got := resp.Header.Get("Content-Length"); got != fmt.Sprint(wantLen) {
				t.Errorf("Range %q: Content-Length = %q, want %d", tc.rangeHeader, got, wantLen)
			}
		}
		if tc.wantStatus == 200 && len(data) != len(body) {
			t.Errorf("Range %q: 200 response should carry the whole file, got %d bytes", tc.rangeHeader, len(data))
		}
	}
}

func TestServeEscapedNames(t *testing.T) {
	ts, _, dir := newTestServer(t)
	for _, rel := range []string{
		"movies/Big Buck Bunny.mkv",
		"movies/кино/серия 01.mkv",
	} {
		url := ts.URL + FilePathToURL(dir, filepath.Join(dir, rel))
		resp, data := get(t, url, "")
		if resp.StatusCode != http.StatusOK || len(data) == 0 {
			t.Errorf("GET %q: status=%d len=%d, want 200 with content", url, resp.StatusCode, len(data))
		}
	}
}

func TestServeRejects(t *testing.T) {
	ts, _, _ := newTestServer(t)
	rejected := []string{
		"/files/notes.txt",           // not media
		"/files/.hidden/secret.mkv",  // hidden folder
		"/files/movies/.dotfile.mkv", // hidden file
		"/files/movies/",             // a folder, not a file
		"/files/",                    // nothing
		"/other/clip.mp4",            // wrong mount point
	}
	for _, path := range rejected {
		if resp, _ := get(t, ts.URL+path, ""); resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %q: status = %d, want 404", path, resp.StatusCode)
		}
	}
}

func TestServeRejectsTraversal(t *testing.T) {
	// Our own resolve() must never be fooled, no matter what the mux or
	// client normalized beforehand.
	_, _, dir := newTestServer(t)
	s := New(dir)
	for _, path := range []string{
		"/files/../notes.txt",
		"/files/movies/../../../etc/passwd",
		"/files/%2e%2e/notes.txt",
		"/files/movies/..%2f..%2fnotes.txt",
	} {
		if _, ok := s.resolve(path); ok {
			t.Errorf("resolve(%q) accepted a traversal path", path)
		}
	}

	// Through the mux, dot paths get a redirect to the cleaned location —
	// never the file contents.
	h := s.Handler()
	for _, path := range []string{"/files/../notes.txt", "/files/movies/../../../etc/passwd"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code < 300 || rec.Code >= 400 { // 301/307/308 redirect, not content
			t.Errorf("GET %q: status = %d, want a 3xx redirect", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "pattern") {
			t.Errorf("GET %q leaked file content", path)
		}
	}
}

func TestHeadAndMethod(t *testing.T) {
	ts, _, _ := newTestServer(t)
	url := ts.URL + "/files/movies/clip.mp4"

	req, _ := http.NewRequest(http.MethodHead, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("HEAD status = %d, want 200", resp.StatusCode)
	}
	if resp.ContentLength != 4096 {
		t.Errorf("HEAD ContentLength = %d, want 4096", resp.ContentLength)
	}

	req, _ = http.NewRequest(http.MethodPost, url, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", resp.StatusCode)
	}
}
