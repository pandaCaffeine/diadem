package dlna

import (
	"strconv"
	"strings"
)

// ssdpMaxAge is the CACHE-CONTROL window advertised in alive notifies
// and M-SEARCH responses.
const ssdpMaxAge = "max-age=1800" // clients may cache us for 30 minutes

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
