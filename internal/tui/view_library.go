package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/pandaCaffeine/diadem/internal/library"
)

const (
	headerLines = 3 // title, stats, rule
	footerLines = 2 // rule, key hints
)

func (m Model) treeSize() (w, h int) {
	return max(m.width, 1), max(m.height-headerLines-footerLines, 1)
}

func (m Model) headerView() string {
	line1 := styleStar.Render("★ ") + styleTitle.Render("Diadem")
	if m.name != "Diadem" {
		line1 += styleDim.Render(" as ") + styleTitle.Render(m.name)
	}
	line1 += "  " + styleDim.Render(tildePath(m.path)+" · "+m.baseURL)

	var line2 string
	switch {
	case m.scanning:
		line2 = m.spinner.View() + styleDim.Render(" scanning…")
	case m.scanErr != nil:
		line2 = styleError.Render("✗ " + m.scanErr.Error())
	case m.lib != nil:
		line2 = styleDim.Render(fmt.Sprintf("%d folders · %d items · %s",
			m.lib.Containers, m.lib.Items, humanSize(m.lib.Bytes)))
	}

	return line1 + "\n" + line2 + "\n" + styleDim.Render(strings.Repeat("─", m.width))
}

func (m Model) bodyView() string {
	switch m.tab {
	case tabClients:
		return m.clientsView()
	case tabLogs:
		return m.logsView()
	}
	h := m.tree.Height
	lines := make([]string, 0, h)
	switch {
	case m.scanErr != nil && m.lib == nil:
		lines = append(lines,
			styleError.Render("✗ "+m.scanErr.Error()),
			styleDim.Render("check that the folder exists and is readable"),
		)
	case m.lib == nil:
		lines = append(lines, m.spinner.View()+styleDim.Render(" scanning library…"))
	case m.lib.Items == 0:
		lines = append(lines, styleDim.Render(
			"no media files found — put videos, music or photos into the folder (mkv, mp4, flac, jpg, …)"))
	default:
		rows := m.tree.Visible()
		end := min(m.tree.offset+h, len(rows))
		for i := m.tree.offset; i < end; i++ {
			lines = append(lines, m.rowView(rows[i], i == m.tree.cursor))
		}
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines[:h], "\n")
}

func (m Model) rowView(r Row, selected bool) string {
	indent := strings.Repeat("  ", r.Depth)
	width := max(m.tree.Width, 20)

	var left, right string
	if r.Node.Kind == library.Container {
		arrow := "▾ "
		if r.Collapsed {
			arrow = "▸ "
		}
		left = indent + arrow + styleFolder.Render(truncate(r.Node.Name, width-lipgloss.Width(indent)-2))
	} else {
		glyph := icon(r.Node.Media) + " "
		if m.isWatched(r.Node) {
			glyph = styleWatched.Render("✓") + " " // watched video: the check replaces the icon
		}
		right = humanSize(r.Node.Size)
		if r.Node.Media == library.MediaVideo {
			right = humanDuration(r.Node.Meta.Duration) + "  " + right
		}
		nameWidth := width - lipgloss.Width(indent) - lipgloss.Width(glyph) - lipgloss.Width(right) - 2
		left = indent + glyph + kindStyle(r.Node.Media).Render(truncate(r.Node.Name, nameWidth))
	}

	line := left
	if right != "" {
		gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
		line = left + strings.Repeat(" ", gap) + styleDim.Render(right)
	}
	if selected {
		line = styleSel.Width(width).Render(line)
	}
	return line
}

func (m Model) footerView() string {
	var hints string
	if m.tab == tabLibrary {
		hints = strings.Join([]string{
			keyHint("↑/↓", "move"),
			keyHint("⏎", "open"),
			keyHint("w", "watched"),
			keyHint("tab", "next"),
			keyHint("r", "rescan"),
			keyHint("q", "quit"),
		}, styleDim.Render("   ·   "))
	} else {
		hints = strings.Join([]string{
			keyHint("tab", "next"),
			keyHint("q", "quit"),
		}, styleDim.Render("   ·   "))
	}
	return styleDim.Render(strings.Repeat("─", m.width)) + "\n" + hints
}

func keyHint(key, desc string) string {
	return styleKey.Render(key) + styleDim.Render(" "+desc)
}
