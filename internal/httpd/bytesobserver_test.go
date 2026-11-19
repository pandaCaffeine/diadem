package httpd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pandaCaffeine/diadem/internal/watch"
)

type fakeBytesObserver struct {
	calls []bytesCall
}

type bytesCall struct {
	k         watch.Key
	start     int64
	delivered int64
}

func (f *fakeBytesObserver) ObserveBytes(k watch.Key, start, delivered int64) {
	f.calls = append(f.calls, bytesCall{k, start, delivered})
}

func newBytesServer(t *testing.T, name string, size int) (*Server, *fakeBytesObserver, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	obs := &fakeBytesObserver{}
	srv := New(dir)
	srv.BytesObserver = obs
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, obs, ts
}

func TestObserveBytesRangeAndFull(t *testing.T) {
	_, obs, ts := newBytesServer(t, "clip.mp4", 16)

	// One ranged GET: exactly the requested slice counts.
	resp, err := http.Get(ts.URL + "/files/clip.mp4")
	if err != nil {
		t.Fatal(err)
	}
	n, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if n != 16 {
		t.Fatalf("full GET body = %d bytes, want 16", n)
	}
	if len(obs.calls) != 1 {
		t.Fatalf("observer calls = %d, want 1", len(obs.calls))
	}
	if c := obs.calls[0]; c.k.RelPath != "clip.mp4" || c.k.Size != 16 || c.delivered != 16 {
		t.Errorf("call = %+v", c)
	}

	// A second, ranged GET reports its slice with the offset it served.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/files/clip.mp4", nil)
	req.Header.Set("Range", "bytes=4-7")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(obs.calls) != 2 {
		t.Fatalf("observer calls = %d, want 2", len(obs.calls))
	}
	if c := obs.calls[1]; c.start != 4 || c.delivered != 4 {
		t.Errorf("ranged call = %+v, want start 4 delivered 4", c)
	}
}

func TestObserveBytesSkipsNonVideoAndHead(t *testing.T) {
	_, obs, ts := newBytesServer(t, "song.flac", 16)

	// HEAD reports nothing.
	req, _ := http.NewRequest(http.MethodHead, ts.URL+"/files/song.flac", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Audio does not earn Watched marks either.
	resp, err = http.Get(ts.URL + "/files/song.flac")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if len(obs.calls) != 0 {
		t.Errorf("observer called %+v, want nothing for HEAD/audio", obs.calls)
	}
}

func TestObserveBytesCountsAbortedTransfer(t *testing.T) {
	// Playback is a stream of aborted-and-retried range requests: the
	// bytes that left before the client walked away still count.
	_, obs, ts := newBytesServer(t, "film.mp4", 1<<20)

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/files/film.mp4", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8<<10)
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		t.Fatal(err)
	}
	cancel()
	_, _ = io.Copy(io.Discard, resp.Body) // drain until the error surfaces
	resp.Body.Close()

	// The handler goroutine notices the dead connection on its own
	// schedule; wait for its report instead of racing it.
	deadline := time.Now().Add(2 * time.Second)
	for len(obs.calls) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(obs.calls) != 1 {
		t.Fatalf("observer calls = %d, want 1", len(obs.calls))
	}
	c := obs.calls[0]
	if c.delivered <= 0 || c.delivered >= int64(1<<20) {
		t.Errorf("delivered = %d, want a partial count", c.delivered)
	}
}
