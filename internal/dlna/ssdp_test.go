package dlna

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestParseMSearch(t *testing.T) {
	valid := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 3\r\nST: upnp:rootdevice\r\nUSER-AGENT: VLC/3.0\r\n\r\n"
	req, ok := parseMSearch(valid)
	if !ok || req.st != "upnp:rootdevice" || req.mx != 3 {
		t.Errorf("parseMSearch(valid) = %+v %v", req, ok)
	}

	// Header names are case-insensitive; no MX on this one.
	lower := "M-SEARCH * HTTP/1.1\r\nSt: ssdp:all\r\n\r\n"
	if req, ok = parseMSearch(lower); !ok || req.st != "ssdp:all" || req.mx != 0 {
		t.Errorf("parseMSearch(lower) = %+v %v", req, ok)
	}

	// MX above 5 clamps, negative goes to 0.
	if req, _ = parseMSearch("M-SEARCH * HTTP/1.1\r\nST: x\r\nMX: 99\r\n\r\n"); req.mx != 5 {
		t.Errorf("mx clamp = %d, want 5", req.mx)
	}

	for _, bad := range []string{
		"NOTIFY * HTTP/1.1\r\nNT: upnp:rootdevice\r\n\r\n",      // wrong method
		"HTTP/1.1 200 OK\r\nST: x\r\n\r\n",                      // a response, not a request
		"M-SEARCH * HTTP/1.1\r\nMAN: \"ssdp:discover\"\r\n\r\n", // no ST
		"garbage",
	} {
		if _, ok := parseMSearch(bad); ok {
			t.Errorf("parseMSearch(%q) accepted", bad)
		}
	}
}

func TestBuildNotify(t *testing.T) {
	got := buildNotify("upnp:rootdevice", "uuid:u1::upnp:rootdevice", "ssdp:alive", "http://h:1/description.xml", "S/1")
	for _, want := range []string{
		"NOTIFY * HTTP/1.1\r\n",
		"HOST: 239.255.255.250:1900\r\n",
		"CACHE-CONTROL: max-age=1800\r\n",
		"LOCATION: http://h:1/description.xml\r\n",
		"NT: upnp:rootdevice\r\n",
		"NTS: ssdp:alive\r\n",
		"USN: uuid:u1::upnp:rootdevice\r\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("alive notify misses %q:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(got, "\r\n\r\n") {
		t.Error("notify must end with a blank line")
	}

	bye := buildNotify("uuid:u1", "uuid:u1", "ssdp:byebye", "", "")
	if strings.Contains(bye, "LOCATION") || strings.Contains(bye, "CACHE-CONTROL") {
		t.Errorf("byebye must be minimal:\n%s", bye)
	}
	if !strings.Contains(bye, "NTS: ssdp:byebye\r\n") {
		t.Errorf("byebye misses NTS:\n%s", bye)
	}
}

func TestBuildMSearchResponse(t *testing.T) {
	got := buildMSearchResponse("urn:schemas-upnp-org:device:MediaServer:1", "uuid:u1::urn:x", "http://h:1/description.xml", "S/1")
	for _, want := range []string{
		"HTTP/1.1 200 OK\r\n",
		"EXT:\r\n",
		"ST: urn:schemas-upnp-org:device:MediaServer:1\r\n",
		"USN: uuid:u1::urn:x\r\n",
		"LOCATION: http://h:1/description.xml\r\n",
		"CONTENT-LENGTH: 0\r\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("M-SEARCH response misses %q:\n%s", want, got)
		}
	}
}

func TestSSDPTargets(t *testing.T) {
	s := &SSDP{uuid: "uuid:u1"}
	ts := s.targets()
	if len(ts) != 5 {
		t.Fatalf("targets = %d, want 5", len(ts))
	}
	wantNTs := map[string]string{
		"upnp:rootdevice": "uuid:u1::upnp:rootdevice",
		"uuid:u1":         "uuid:u1",
		"urn:schemas-upnp-org:device:MediaServer:1":        "uuid:u1::urn:schemas-upnp-org:device:MediaServer:1",
		"urn:schemas-upnp-org:service:ContentDirectory:1":  "uuid:u1::urn:schemas-upnp-org:service:ContentDirectory:1",
		"urn:schemas-upnp-org:service:ConnectionManager:1": "uuid:u1::urn:schemas-upnp-org:service:ConnectionManager:1",
	}
	for _, target := range ts {
		if wantNTs[target.nt] != target.usn {
			t.Errorf("target %q has USN %q, want %q", target.nt, target.usn, wantNTs[target.nt])
		}
	}
}

// TestSSDPLive does a real SSDP round trip over multicast: start the
// announcer, fire an M-SEARCH at the group and expect a unicast 200 OK
// back. The search targets this instance's UUID, so other Diadems (or any
// UPnP servers) on the same LAN cannot steal the reply. Skipped when the
// environment has no multicast (CI containers).
func TestSSDPLive(t *testing.T) {
	const testUUID = "uuid:cafe1234-0000-4000-8000-000000000001"
	obs := &fakeActivityObserver{}
	s, err := startSSDP("http://127.0.0.1:59999/description.xml", testUUID, obs)
	if err != nil {
		t.Skipf("cannot bind SSDP socket: %v", err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	// Not 127.0.0.1: a loopback-bound socket has no route to the
	// multicast group; any-interface works and the reply is unicast.
	client, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		t.Skipf("cannot bind client socket: %v", err)
	}
	defer client.Close()

	search := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 1\r\nUSER-AGENT: VLC-test/3.0\r\nST: " + testUUID + "\r\n\r\n"
	groupAddr := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}
	if _, err := client.WriteTo([]byte(search), groupAddr); err != nil {
		t.Skipf("cannot send multicast: %v", err)
	}

	// The reply is delayed randomly (0–800 ms) per spec; allow slack.
	if err := client.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	n, _, err := client.ReadFrom(buf)
	if err != nil {
		t.Skipf("no SSDP reply (multicast unavailable?): %v", err)
	}
	reply := string(buf[:n])
	for _, want := range []string{
		"HTTP/1.1 200 OK",
		"LOCATION: http://127.0.0.1:59999/description.xml",
		"ST: " + testUUID,
		"USN: " + testUUID, // UUID notifications carry the bare UUID
	} {
		if !strings.Contains(reply, want) {
			t.Errorf("reply misses %q:\n%s", want, reply)
		}
	}

	// The search itself must have been reported to the observer by the
	// time the (delayed) reply arrived.
	if len(obs.searches) == 0 || obs.searches[0].userAgent != "VLC-test/3.0" {
		t.Errorf("observer searches = %+v, want one with our User-Agent", obs.searches)
	}
}

// fakeActivityObserver records activity for tests.
type fakeActivityObserver struct {
	searches     []observeCall
	descriptions []observeCall
	browses      []browseCall
}

type observeCall struct {
	ip, userAgent string
}

type browseCall struct {
	ip, userAgent, target string
}

func (f *fakeActivityObserver) ObserveSearch(ip, userAgent string) {
	f.searches = append(f.searches, observeCall{ip, userAgent})
}

func (f *fakeActivityObserver) ObserveDescription(ip, userAgent string) {
	f.descriptions = append(f.descriptions, observeCall{ip, userAgent})
}

func (f *fakeActivityObserver) ObserveBrowse(ip, userAgent, target string) {
	f.browses = append(f.browses, browseCall{ip, userAgent, target})
}
