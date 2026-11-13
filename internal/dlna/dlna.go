// Package dlna implements the UPnP AV side of Diadem: the MediaServer
// device description, ContentDirectory:1 (Browse over SOAP, DIDL-Lite
// results) and the mandatory ConnectionManager stub. SSDP discovery, which
// makes clients find all of this, arrives in step 4.
package dlna

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Config describes one running Diadem server.
type Config struct {
	Root         string // absolute Library path
	BaseURL      string // http://host:port — where the Library is served
	FriendlyName string // name clients display (CONTEXT.md: Friendly Name)
	UUID         string // full UDN "uuid:..."; generated when empty
}

// Server serves the UPnP endpoints: device description, SCPDs and SOAP
// control URLs. Mount it next to the /files/ handler on the same port.
type Server struct {
	cfg Config
}

// New builds a Server, generating a UUID when none is given.
func New(cfg Config) *Server {
	if cfg.UUID == "" {
		cfg.UUID = NewUUID()
	}
	return &Server{cfg: cfg}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/description.xml":
		s.serveXML(w, s.description())
	case "/scpd/ContentDirectory.xml":
		s.serveXML(w, scpdContentDirectory)
	case "/scpd/ConnectionManager.xml":
		s.serveXML(w, scpdConnectionManager)
	case "/ctl/ContentDirectory":
		s.control(w, r, s.handleContentDirectory)
	case "/ctl/ConnectionManager":
		s.control(w, r, s.handleConnectionManager)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) serveXML(w http.ResponseWriter, doc string) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(doc))
}

// description renders the MediaServer:1 device description. URLBase pins
// the address clients resolve all control and content URLs against — it
// must be an address actually reachable from the LAN.
func (s *Server) description() string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?>` + "\n")
	b.WriteString(`<root xmlns="urn:schemas-upnp-org:device-1-0">`)
	b.WriteString(`<specVersion><major>1</major><minor>0</minor></specVersion>`)
	b.WriteString(`<URLBase>` + escString(s.cfg.BaseURL+"/") + `</URLBase>`)
	b.WriteString(`<device>`)
	b.WriteString(`<deviceType>urn:schemas-upnp-org:device:MediaServer:1</deviceType>`)
	b.WriteString(`<friendlyName>` + escString(s.cfg.FriendlyName) + `</friendlyName>`)
	b.WriteString(`<manufacturer>pandaCaffeine</manufacturer>`)
	b.WriteString(`<manufacturerURL>https://github.com/pandaCaffeine/diadem</manufacturerURL>`)
	b.WriteString(`<modelDescription>Diadem console DLNA media server</modelDescription>`)
	b.WriteString(`<modelName>Diadem</modelName>`)
	b.WriteString(`<modelNumber>1</modelNumber>`)
	b.WriteString(`<UDN>` + escString(s.cfg.UUID) + `</UDN>`)
	b.WriteString(`<serviceList>`)
	b.WriteString(serviceXML(
		"urn:schemas-upnp-org:service:ContentDirectory:1",
		"urn:upnp-org:serviceId:ContentDirectory",
		"/scpd/ContentDirectory.xml", "/ctl/ContentDirectory", "/evt/ContentDirectory"))
	b.WriteString(serviceXML(
		"urn:schemas-upnp-org:service:ConnectionManager:1",
		"urn:upnp-org:serviceId:ConnectionManager",
		"/scpd/ConnectionManager.xml", "/ctl/ConnectionManager", "/evt/ConnectionManager"))
	b.WriteString(`</serviceList></device></root>`)
	return b.String()
}

func serviceXML(serviceType, serviceID, scpd, control, event string) string {
	return `<service><serviceType>` + escString(serviceType) + `</serviceType>` +
		`<serviceId>` + escString(serviceID) + `</serviceId>` +
		`<SCPDURL>` + scpd + `</SCPDURL>` +
		`<controlURL>` + control + `</controlURL>` +
		`<eventSubURL>` + event + `</eventSubURL></service>`
}

// NewUUID returns a random RFC 4122 version 4 UUID in UPnP UDN form.
// The UUID is per-run: clients see a "new" server after each restart,
// which VLC handles without complaints.
func NewUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Practically unreachable on supported platforms; degrade to a
		// time-derived value rather than crashing a media server.
		now := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(now >> (i % 8 * 8))
		}
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("uuid:%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7],
		b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15])
}
