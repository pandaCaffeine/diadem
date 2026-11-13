package httpd

import (
	"net"
	"net/url"
	"path/filepath"
	"strings"
)

// FilePathToURL escapes a file path under the Library root into its
// /files/... URL path: each segment is percent-encoded, so spaces,
// cyrillic and other friendly filenames survive the trip.
func FilePathToURL(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return ""
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "" // outside the Library: no URL for you
	}
	var b strings.Builder
	b.WriteString("/files")
	for seg := range strings.SplitSeq(rel, string(filepath.Separator)) {
		b.WriteString("/")
		b.WriteString(url.PathEscape(seg))
	}
	return b.String()
}

// LocalIPv4 returns the machine's primary local IPv4 — the source address
// a new outbound connection would use. UDP dialing consults the routing
// table without sending any packets. Falls back to loopback when there is
// no route at all.
func LocalIPv4() string {
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			if ip := addr.IP.To4(); ip != nil {
				return ip.String()
			}
		}
	}
	return "127.0.0.1"
}
