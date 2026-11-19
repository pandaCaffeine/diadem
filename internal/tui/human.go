package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// humanAgo renders a coarse "5s ago"-style age.
func humanAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < 2*time.Second:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

// humanDuration renders a video duration for the duration column: HH:mm
// floored to minutes, "--:--" when unknown.
func humanDuration(d time.Duration) string {
	if d <= 0 {
		return "--:--"
	}
	total := int(d / time.Minute)
	h, m := total/60, total%60
	if h > 99 {
		h, m = 99, 59
	}
	return fmt.Sprintf("%02d:%02d", h, m)
}

// humanBitrate renders a file's average rate: bytes per second, in
// humanSize units. res@bitrate in DIDL-Lite uses the same unit (the
// UPnP spec counts bytes/second, unlike common usage).
func humanBitrate(size int64, d time.Duration) string {
	sec := int64(d / time.Second)
	if sec < 1 {
		return "0 B/s"
	}
	if bps := size / sec; bps > 0 {
		return humanSize(bps) + "/s"
	}
	return "0 B/s"
}

// tildePath shortens the user's home directory to "~".
func tildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

// humanSize renders a byte count for humans: 1.4 GB, 12 B, 320 KB.
func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	f := float64(b)
	var u string
	for _, u = range []string{"KB", "MB", "GB", "TB", "PB"} {
		f /= unit
		if f < unit {
			break
		}
	}
	return fmt.Sprintf("%.1f %s", f, u)
}

// truncate cuts s to at most w terminal columns, appending "…" when cut.
func truncate(s string, w int) string {
	if w < 1 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if lipgloss.Width(b.String())+lipgloss.Width(string(r)) > w-1 {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + "…"
}
