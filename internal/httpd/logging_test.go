package httpd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pandaCaffeine/diadem/internal/logx"
)

func TestServeLogging(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), make([]byte, 64), 0o644); err != nil {
		t.Fatal(err)
	}
	rec, err := logx.New("")
	if err != nil {
		t.Fatal(err)
	}
	srv := New(dir)
	srv.Logger = rec
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/files/clip.mp4", nil)
	req.Header.Set("Range", "bytes=0-31")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	lines := strings.Join(rec.Recent(), "\n")
	for _, want := range []string{"[http]", "GET /files/clip.mp4", "→ 206 32/64B"} {
		if !strings.Contains(lines, want) {
			t.Errorf("log lines missing %q:\n%s", want, lines)
		}
	}
}
