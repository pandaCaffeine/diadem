package httpd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type fakeStreamObserver struct {
	calls []streamCall
}

type streamCall struct {
	ip, userAgent, name string
}

func (f *fakeStreamObserver) ObserveStream(ip, userAgent, name string) {
	f.calls = append(f.calls, streamCall{ip, userAgent, name})
}

func TestObserveStream(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), make([]byte, 16), 0o644); err != nil {
		t.Fatal(err)
	}

	obs := &fakeStreamObserver{}
	srv := New(dir)
	srv.Observer = obs
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/files/clip.mp4", nil)
	req.Header.Set("User-Agent", "VLC/3.0.21 LibVLC/3.0.21")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if len(obs.calls) != 1 {
		t.Fatalf("observer calls = %d, want 1", len(obs.calls))
	}
	call := obs.calls[0]
	if call.ip != "127.0.0.1" || call.userAgent != "VLC/3.0.21 LibVLC/3.0.21" || call.name != "clip.mp4" {
		t.Errorf("call = %+v", call)
	}

	// Unknown files must not be reported.
	resp, _ = http.Get(ts.URL + "/files/missing.mkv")
	resp.Body.Close()
	if len(obs.calls) != 1 {
		t.Errorf("observer called for a missing file: %+v", obs.calls)
	}
}
