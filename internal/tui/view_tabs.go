package tui

import (
	"strings"
	"time"

	"github.com/pandaCaffeine/diadem/internal/clients"
)

// clientsView renders the Clients tab: who is talking to us and what they
// are doing right now.
func (m Model) clientsView() string {
	h := m.tree.Height
	lines := make([]string, 0, h)
	if len(m.clientRows) == 0 {
		lines = append(lines, styleDim.Render("no clients yet — open VLC on your TV or laptop and it will appear here"))
	} else {
		lines = append(lines, styleDim.Render("CLIENTS"))
		for _, c := range m.clientRows {
			if len(lines) >= h {
				break
			}
			lines = append(lines, m.clientRow(c))
		}
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines[:h], "\n")
}

func (m Model) clientRow(c clients.Client) string {
	width := max(m.tree.Width, 40)

	dot := styleDim.Render("○")
	active := time.Since(c.LastSeen) < clients.ActiveSince
	if active {
		dot = styleAudio.Render("●") // green: spoken to us just now
	}

	name := c.IP
	if ua := shortUserAgent(c.UserAgent); ua != "" {
		name = ua + " " + styleDim.Render(c.IP)
	}

	activity := c.Kind.String()
	if c.Target != "" {
		activity += " " + c.Target
	}

	row := dot + " " + styleTitle.Render(truncate(name, 30)) + "  " +
		truncate(activity, width-46) + "  " + styleDim.Render(humanAgo(c.LastSeen))
	return row
}

// shortUserAgent picks the product token out of a User-Agent like
// "VLC/3.0.21 LibVLC/3.0.21" → "VLC/3.0.21".
func shortUserAgent(ua string) string {
	if ua == "" {
		return ""
	}
	token := ua
	if i := strings.IndexByte(ua, ' '); i > 0 {
		token = ua[:i]
	}
	if len(token) > 24 {
		token = token[:24]
	}
	return token
}

// logsView renders the Logs tab: the recent activity ring, newest at the
// bottom, auto-scrolled like a tail -f.
func (m Model) logsView() string {
	h := m.tree.Height
	lines := make([]string, 0, h)
	title := "LOGS"
	if m.logFile != "" {
		title += " · " + m.logFile
	}
	lines = append(lines, styleTitle.Render(title))

	recent := m.logs.Recent() // nil-safe on a nil Recorder
	start := max(0, len(recent)-(h-1))
	for _, ln := range recent[start:] {
		lines = append(lines, styleDim.Render(truncate(ln, m.tree.Width)))
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines[:h], "\n")
}
