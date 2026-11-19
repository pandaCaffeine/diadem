package dlna

import (
	"math/rand/v2"
	"net"
	"sync"
	"time"

	"github.com/pandaCaffeine/diadem/internal/logx"
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
	logger   logx.Logger

	stop    chan struct{}
	stopped chan struct{}
	closeMu sync.Once
}

const ssdpAliveEvery = 30 * time.Second

// startSSDP binds the multicast socket, announces the server and keeps
// re-announcing until Close.
func startSSDP(location, uuid string, observer ActivityObserver, logger logx.Logger) (*SSDP, error) {
	if logger == nil {
		logger = logx.NopLogger{}
	}
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
		logger:   logger,
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
	}
	go s.receiveLoop()
	s.alive()
	s.logger.Logf("[ssdp] advertising %s as %s", location, uuid)
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
		s.logger.Logf("[ssdp] byebye sent")
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
			s.logger.Logf("[ssdp] alive announced to 239.255.255.250:1900")
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
			headers := parseHeaders(string(buf[:n]))
			s.logger.Logf("[ssdp] M-SEARCH from %s ST=%s UA=%q", src.IP, req.st, headers["user-agent"])
			if s.observer != nil {
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

// StartSSDP advertises this server on the LAN: the SSDP location and UUID
// always match the device description this Server serves.
func (s *Server) StartSSDP() (*SSDP, error) {
	return startSSDP(s.cfg.BaseURL+"/description.xml", s.cfg.UUID, s.cfg.Observer, s.cfg.Logger)
}
