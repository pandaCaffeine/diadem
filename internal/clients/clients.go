// Package clients tracks which devices on the LAN talk to Diadem and what
// they last did, powering the TUI Clients tab. Events arrive as observer
// callbacks from the SSDP and HTTP layers.
package clients

import (
	"sort"
	"sync"
	"time"
)

// Kind is the last activity a Client performed.
type Kind uint8

const (
	KindSearch   Kind = iota + 1 // SSDP M-SEARCH: looking for servers
	KindDiscover                 // fetched the device description
	KindBrowse                   // listed a Container via ContentDirectory
	KindStream                   // fetching Item bytes over HTTP
)

func (k Kind) String() string {
	switch k {
	case KindSearch:
		return "searching"
	case KindDiscover:
		return "discovering"
	case KindBrowse:
		return "browsing"
	case KindStream:
		return "streaming"
	}
	return "?"
}

// Client is one device seen talking to Diadem, keyed by IP.
type Client struct {
	IP        string
	UserAgent string
	FirstSeen time.Time
	LastSeen  time.Time
	Kind      Kind
	Target    string // Container or Item name, when it applies
}

// ActiveSince is the window after which a Client stops being highlighted
// as active in the TUI.
const ActiveSince = 10 * time.Second

// Tracker collects client activity. Safe for concurrent use: SSDP and
// HTTP goroutines call Observe* while the TUI reads Snapshot.
type Tracker struct {
	mu    sync.Mutex
	byIP  map[string]*Client
	limit int
}

// NewTracker builds a Tracker remembering up to 64 clients.
func NewTracker() *Tracker {
	return &Tracker{byIP: make(map[string]*Client), limit: 64}
}

// The Observe* methods make *Tracker satisfy the observer interfaces that
// the httpd and dlna packages define structurally.
func (t *Tracker) ObserveSearch(ip, userAgent string) {
	t.observe(ip, userAgent, KindSearch, "")
}

func (t *Tracker) ObserveDescription(ip, userAgent string) {
	t.observe(ip, userAgent, KindDiscover, "")
}

func (t *Tracker) ObserveBrowse(ip, userAgent, target string) {
	t.observe(ip, userAgent, KindBrowse, target)
}

func (t *Tracker) ObserveStream(ip, userAgent, target string) {
	t.observe(ip, userAgent, KindStream, target)
}

func (t *Tracker) observe(ip, userAgent string, kind Kind, target string) {
	if ip == "" {
		return
	}
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.byIP[ip]
	if !ok {
		if t.limit > 0 && len(t.byIP) >= t.limit {
			t.evictStalestLocked()
		}
		c = &Client{IP: ip, FirstSeen: now}
		t.byIP[ip] = c
	}
	if userAgent != "" {
		c.UserAgent = userAgent
	}
	c.LastSeen = now
	c.Kind = kind
	c.Target = target
}

// evictStalestLocked drops the least recently seen client; called with the
// lock held when the table is full.
func (t *Tracker) evictStalestLocked() {
	stalestIP := ""
	var stalest time.Time
	for ip, c := range t.byIP {
		if stalestIP == "" || c.LastSeen.Before(stalest) {
			stalestIP, stalest = ip, c.LastSeen
		}
	}
	delete(t.byIP, stalestIP)
}

// Snapshot returns the clients, most recently seen first.
func (t *Tracker) Snapshot() []Client {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Client, 0, len(t.byIP))
	for _, c := range t.byIP {
		out = append(out, *c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	return out
}
