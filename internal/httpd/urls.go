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

// LocalIPv4 returns the machine's primary local IPv4 — the address
// clients on the LAN should use. UDP dialing consults the routing table
// without sending any packets, but a VPN tunnel can own the default route
// and hijack that answer, so tunnel and other virtual interface addresses
// are rejected in favour of the first hardware (en*) address. Falls back
// to loopback when there is no route at all.
func LocalIPv4() string {
	if ip := udpDialIP(); ip != "" && !ipOnVirtualInterface(ip) {
		return ip
	}
	if ip := firstHardwareIPv4(); ip != "" {
		return ip
	}
	if ip := udpDialIP(); ip != "" {
		return ip // only virtual interfaces exist: better than nothing
	}
	return "127.0.0.1"
}

// virtualPrefixes are interface names whose addresses must not be
// advertised to the LAN: VPN tunnels, virtual bridges, loopback.
var virtualPrefixes = []string{"utun", "tun", "tap", "bridge", "veth", "lo", "gif", "stf", "awdl", "llw", "docker"}

func isVirtualInterface(name string) bool {
	n := strings.ToLower(name)
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// ipOnVirtualInterface reports whether ip belongs to a tunnel/bridge-style
// interface.
func ipOnVirtualInterface(ip string) bool {
	want := net.ParseIP(ip)
	if want == nil {
		return false
	}
	ifaces, _ := net.Interfaces()
	for _, ifi := range ifaces {
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			if netIP, ok := a.(*net.IPNet); ok && netIP.IP.Equal(want) {
				return isVirtualInterface(ifi.Name)
			}
		}
	}
	return false
}

// firstHardwareIPv4 returns the first usable IPv4 of a physical, up
// interface (en0 first on macOS: Wi-Fi or Ethernet).
func firstHardwareIPv4() string {
	ifaces, _ := net.Interfaces()
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || isVirtualInterface(ifi.Name) {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ip := ipnet.IP.To4(); ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
				return ip.String()
			}
		}
	}
	return ""
}

// udpDialIP is the source address a new outbound connection would use.
func udpDialIP() string {
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			if ip := addr.IP.To4(); ip != nil {
				return ip.String()
			}
		}
	}
	return ""
}
