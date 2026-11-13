package dlna

import (
	"math/rand/v2"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
)

// SSDP makes the server discoverable: periodic ssdp:alive notifications,
// M-SEARCH replies and ssdp:byebye on shutdown (UPnP Device Architecture
// §1, "Discovery"). Transport is UDP multicast to 239.255.255.250:1900.
type SSDP struct {
	conn     *net.UDPConn
	group    *net.UDPAddr
	location string // description.xml URL advertised to clients
	uuid     string // UDN, "uuid:..."
	server   string // SERVER header value
	observer ActivityObserver

	stop    chan struct{}
	stopped chan struct{}
	closeMu sync.Once
}

const (
	ssdpAliveEvery = 30 * time.Second
	ssdpMaxAge     = "max-age=1800" // clients may cache us for 30 minutes
)

// startSSDP binds the multicast socket, announces the server and keeps
// re-announcing until Close.
func startSSDP(location, uuid string, observer ActivityObserver) (*SSDP, error) {
	group := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}
	conn, err := net.ListenMulticastUDP("udp4", nil, group)
	if err != nil {
		return nil, err
	}
	packetConn := ipv4.NewPacketConn(conn)
	// UPnP recommends a multicast TTL of 4: enough for mesh Wi-Fi with
	// IGMP proxying, still contained to the local network.
	_ = packetConn.SetMulticastTTL(4)
	// ListenMulticastUDP joins only the default interface; join the rest
	// too, so M-SEARCH arriving on Wi-Fi (or Ethernet) is seen either way.
	ifaces, _ := net.Interfaces()
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		_ = packetConn.JoinGroup(&ifi, group)
	}

	s := &SSDP{
		conn:     conn,
		group:    group,
		location: location,
		uuid:     uuid,
		server:   "Diadem/1.0 UPnP/1.0",
		observer: observer,
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
	}
	go s.receiveLoop()
	s.alive()
	go s.announceLoop()
	return s, nil
}

// Close sends ssdp:byebye so clients drop the server from their lists
// immediately, then shuts the socket down.
func (s *SSDP) Close() error {
	var err error
	s.closeMu.Do(func() {
		close(s.stop)
		s.byebye()
		err = s.conn.Close()
		<-s.stopped
	})
	return err
}

// ssdpTarget is one NT/USN pair a MediaServer advertises.
type ssdpTarget struct{ nt, usn string }

// targets lists everything we advertise: the root device, our UUID, the
// MediaServer device type and both services. Per spec the USN is
// "uuid::nt", except for the uuid notification itself.
func (s *SSDP) targets() []ssdpTarget {
	urns := []string{
		"upnp:rootdevice",
		s.uuid,
		"urn:schemas-upnp-org:device:MediaServer:1",
		"urn:schemas-upnp-org:service:ContentDirectory:1",
		"urn:schemas-upnp-org:service:ConnectionManager:1",
	}
	out := make([]ssdpTarget, 0, len(urns))
	for _, nt := range urns {
		usn := nt
		if nt != s.uuid {
			usn = s.uuid + "::" + nt
		}
		out = append(out, ssdpTarget{nt: nt, usn: usn})
	}
	return out
}

func (s *SSDP) alive() {
	for _, t := range s.targets() {
		s.send(buildNotify(t.nt, t.usn, "ssdp:alive", s.location, s.server))
	}
}

func (s *SSDP) byebye() {
	for _, t := range s.targets() {
		s.send(buildNotify(t.nt, t.usn, "ssdp:byebye", "", ""))
	}
}

func (s *SSDP) announceLoop() {
	ticker := time.NewTicker(ssdpAliveEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.alive()
		case <-s.stop:
			return
		}
	}
}

func (s *SSDP) receiveLoop() {
	defer close(s.stopped)
	buf := make([]byte, 4096)
	for {
		n, src, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			return // socket closed (normal shutdown) or dead
		}
		if req, ok := parseMSearch(string(buf[:n])); ok {
			if s.observer != nil {
				headers := parseHeaders(string(buf[:n]))
				s.observer.ObserveSearch(src.IP.String(), headers["user-agent"])
			}
			go s.respond(src, req)
		}
	}
}

// respond answers an M-SEARCH. The reply is delayed by a random fraction
// of MX so that a burst of simultaneous searches does not stampede the
// network (UPnP UDA §1.3.3).
func (s *SSDP) respond(src *net.UDPAddr, req msearch) {
	if req.mx > 0 {
		time.Sleep(time.Duration(rand.IntN(800)) * time.Millisecond)
	}
	for _, t := range s.targets() {
		if req.st == "ssdp:all" || req.st == t.nt {
			s.sendTo(buildMSearchResponse(t.nt, t.usn, s.location, s.server), src)
		}
	}
}

func (s *SSDP) send(msg string) { s.sendTo(msg, s.group) }
func (s *SSDP) sendTo(msg string, addr *net.UDPAddr) {
	_, _ = s.conn.WriteToUDP([]byte(msg), addr) // best effort by design
}

// msearch is a parsed M-SEARCH request.
type msearch struct {
	st string
	mx int // seconds, 0 when absent
}

// parseMSearch recognizes an M-SEARCH method line and pulls out ST and MX.
// Header names are case-insensitive; NOTIFY and junk are rejected.
func parseMSearch(msg string) (msearch, bool) {
	first := msg
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		first = msg[:i]
	}
	if !strings.HasPrefix(strings.TrimSpace(first), "M-SEARCH") {
		return msearch{}, false
	}
	headers := parseHeaders(msg)
	st := headers["st"]
	if st == "" {
		return msearch{}, false
	}
	mx := 0
	if v, err := strconv.Atoi(headers["mx"]); err == nil {
		mx = min(max(v, 0), 5)
	}
	return msearch{st: st, mx: mx}, true
}

// parseHeaders returns lowercase-keyed header values of an HTTP-like
// SSDP message.
func parseHeaders(msg string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(msg, "\r\n") {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(value)
	}
	return out
}

// buildNotify renders a NOTIFY message. alive carries CACHE-CONTROL,
// LOCATION and SERVER; byebye is minimal per spec.
func buildNotify(nt, usn, nts, location, server string) string {
	var b strings.Builder
	b.WriteString("NOTIFY * HTTP/1.1\r\n")
	b.WriteString("HOST: 239.255.255.250:1900\r\n")
	if nts == "ssdp:alive" {
		b.WriteString("CACHE-CONTROL: " + ssdpMaxAge + "\r\n")
		b.WriteString("LOCATION: ")
		b.WriteString(location)
		b.WriteString("\r\n")
		b.WriteString("SERVER: ")
		b.WriteString(server)
		b.WriteString("\r\n")
	}
	b.WriteString("NT: ")
	b.WriteString(nt)
	b.WriteString("\r\n")
	b.WriteString("NTS: ")
	b.WriteString(nts)
	b.WriteString("\r\n")
	b.WriteString("USN: ")
	b.WriteString(usn)
	b.WriteString("\r\n")
	b.WriteString("\r\n")
	return b.String()
}

// buildMSearchResponse renders the unicast 200 OK answer to an M-SEARCH.
func buildMSearchResponse(st, usn, location, server string) string {
	var b strings.Builder
	b.WriteString("HTTP/1.1 200 OK\r\n")
	b.WriteString("CACHE-CONTROL: " + ssdpMaxAge + "\r\n")
	b.WriteString("EXT:\r\n")
	b.WriteString("LOCATION: ")
	b.WriteString(location)
	b.WriteString("\r\n")
	b.WriteString("SERVER: ")
	b.WriteString(server)
	b.WriteString("\r\n")
	b.WriteString("ST: ")
	b.WriteString(st)
	b.WriteString("\r\n")
	b.WriteString("USN: ")
	b.WriteString(usn)
	b.WriteString("\r\n")
	b.WriteString("CONTENT-LENGTH: 0\r\n")
	b.WriteString("\r\n")
	return b.String()
}

// StartSSDP advertises this server on the LAN: the SSDP location and UUID
// always match the device description this Server serves.
func (s *Server) StartSSDP() (*SSDP, error) {
	return startSSDP(s.cfg.BaseURL+"/description.xml", s.cfg.UUID, s.cfg.Observer)
}
