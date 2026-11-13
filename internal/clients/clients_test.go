package clients

import (
	"sync"
	"testing"
	"time"
)

func TestTrackerTracksByIP(t *testing.T) {
	tr := NewTracker()
	tr.ObserveSearch("192.0.2.9", "VLC/3.0.21 LibVLC/3.0.21")
	tr.ObserveDescription("192.0.2.9", "VLC/3.0.21 LibVLC/3.0.21")
	tr.ObserveBrowse("192.0.2.9", "", "movies")
	tr.ObserveStream("192.0.2.4", "VLC/3.0.21 TV", "Игрушка.mkv")

	got := tr.Snapshot()
	if len(got) != 2 {
		t.Fatalf("clients = %d, want 2", len(got))
	}
	// Most recently seen first: the streamer just acted.
	if got[0].IP != "192.0.2.4" || got[0].Kind != KindStream || got[0].Target != "Игрушка.mkv" {
		t.Errorf("top client = %+v", got[0])
	}
	if got[1].IP != "192.0.2.9" || got[1].Kind != KindBrowse || got[1].Target != "movies" {
		t.Errorf("second client = %+v", got[1])
	}
	// One entry per IP: earlier kinds were replaced, first seen kept.
	if !got[1].FirstSeen.Before(got[1].LastSeen) && got[1].FirstSeen != got[1].LastSeen {
		t.Errorf("FirstSeen should not move: %+v", got[1])
	}
	// Empty User-Agent must not erase a remembered one.
	if got[1].UserAgent != "VLC/3.0.21 LibVLC/3.0.21" {
		t.Errorf("UserAgent = %q, want kept", got[1].UserAgent)
	}
}

func TestTrackerIgnoresEmptyIP(t *testing.T) {
	tr := NewTracker()
	tr.ObserveStream("", "x", "y")
	if got := tr.Snapshot(); len(got) != 0 {
		t.Errorf("empty IP should be ignored, got %+v", got)
	}
}

func TestTrackerEvictsStalest(t *testing.T) {
	tr := &Tracker{byIP: make(map[string]*Client), limit: 2}
	old := time.Now().Add(-time.Hour)
	tr.byIP["10.0.0.1"] = &Client{IP: "10.0.0.1", LastSeen: old}
	tr.byIP["10.0.0.2"] = &Client{IP: "10.0.0.2", LastSeen: time.Now()}
	tr.ObserveSearch("10.0.0.3", "") // table full: evict 10.0.0.1

	got := tr.Snapshot()
	if len(got) != 2 {
		t.Fatalf("clients = %d, want 2", len(got))
	}
	for _, c := range got {
		if c.IP == "10.0.0.1" {
			t.Error("stalest client was not evicted")
		}
	}
}

func TestTrackerConcurrent(t *testing.T) {
	tr := NewTracker()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				tr.ObserveStream("192.0.2."+itoa(n), "ua", "file.mkv")
				_ = tr.Snapshot()
			}
		}(i)
	}
	wg.Wait()
	if got := tr.Snapshot(); len(got) != 8 {
		t.Errorf("clients = %d, want 8", len(got))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
