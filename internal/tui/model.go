package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pandaCaffeine/diadem/internal/library"
)

// scannedMsg carries a library.Scan result into the TUI event loop.
type scannedMsg struct {
	lib *library.Library
	err error
}

// ScanCmd runs library.Scan off the render loop, so big folders never
// freeze the interface.
func ScanCmd(path string) tea.Cmd {
	return func() tea.Msg {
		lib, err := library.Scan(path)
		return scannedMsg{lib: lib, err: err}
	}
}

const (
	headerLines = 3 // title, stats, rule
	footerLines = 2 // rule, key hints
)

// Model is the root Bubble Tea model of Diadem.
type Model struct {
	name string // Friendly Name (server name, CONTEXT.md)
	path string // absolute Library path
	port int    // HTTP port; the server itself arrives in step 2

	lib      *library.Library
	tree     *TreeView
	scanning bool
	scanErr  error
	spinner  spinner.Model

	popup *library.Node // Item whose details are open; nil = none

	width, height int
}

// New builds the model. path must be absolute.
func New(name, path string, port int) Model {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(spinnerStyle))
	return Model{
		name:     name,
		path:     path,
		port:     port,
		tree:     NewTreeView(nil),
		scanning: true,
		spinner:  sp,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, ScanCmd(m.path))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.tree.Width, m.tree.Height = m.treeSize()
		return m, nil

	case spinner.TickMsg:
		if !m.scanning {
			return m, nil // stop ticking until the next rescan
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case scannedMsg:
		m.scanning = false
		m.scanErr = msg.err
		if msg.err == nil {
			m.lib = msg.lib
			m.tree.SetRoot(msg.lib.Root)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.popup != nil {
		switch msg.String() {
		case "esc", "enter", "q", " ", "tab":
			m.popup = nil
		}
		return m, nil
	}

	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "r":
		if !m.scanning {
			m.scanning = true
			return m, tea.Batch(m.spinner.Tick, ScanCmd(m.path))
		}
	case "up", "k":
		m.tree.Move(-1)
	case "down", "j":
		m.tree.Move(1)
	case "pgup", "b":
		m.tree.Page(-1)
	case "pgdown", "f", " ":
		m.tree.Page(1)
	case "home", "g":
		m.tree.Home()
	case "end", "G":
		m.tree.End()
	case "enter":
		if cur := m.tree.Current(); cur != nil {
			if cur.Node.Kind == library.Container {
				m.tree.Toggle()
			} else {
				m.popup = cur.Node
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return styleDim.Render("starting…")
	}
	var b strings.Builder
	b.WriteString(m.headerView())
	b.WriteByte('\n')
	b.WriteString(m.bodyView())
	b.WriteByte('\n')
	b.WriteString(m.footerView())
	view := b.String()
	if m.popup != nil {
		view = m.overlayPopup(view, m.popupView(m.popup))
	}
	return view
}

func (m Model) treeSize() (w, h int) {
	return max(m.width, 1), max(m.height-headerLines-footerLines, 1)
}

func (m Model) headerView() string {
	line1 := styleCrown.Render("♛ ") + styleTitle.Render("Diadem")
	if m.name != "Diadem" {
		line1 += styleDim.Render(" as ") + styleTitle.Render(m.name)
	}
	line1 += "  " + styleDim.Render(fmt.Sprintf("%s · :%d", tildePath(m.path), m.port))

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
		lines = append(lines, styleDim.Render("no media files found in the library — drop videos, music or photos into the folder"))
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
		right = humanSize(r.Node.Size)
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
	hints := strings.Join([]string{
		keyHint("↑/↓", "move"),
		keyHint("⏎", "open"),
		keyHint("r", "rescan"),
		keyHint("q", "quit"),
	}, styleDim.Render("   ·   "))
	return styleDim.Render(strings.Repeat("─", m.width)) + "\n" + hints
}

func keyHint(key, desc string) string {
	return styleKey.Render(key) + styleDim.Render(" "+desc)
}

func (m Model) popupView(n *library.Node) string {
	body := icon(n.Media) + " " + kindStyle(n.Media).Render(n.Name) + "\n\n" + strings.Join([]string{
		fmt.Sprintf("%s  %s", styleDim.Render("Kind"), n.Media),
		fmt.Sprintf("%s  %s", styleDim.Render("Size"), humanSize(n.Size)),
		fmt.Sprintf("%s  %s", styleDim.Render("Path"), truncate(tildePath(n.Path), 60)),
	}, "\n")
	return popupStyle.Render(body)
}

// overlayPopup centers the popup over the view as a modal band.
func (m Model) overlayPopup(base, popup string) string {
	baseLines := strings.Split(base, "\n")
	popupLines := strings.Split(popup, "\n")
	top := max((len(baseLines)-len(popupLines))/2, headerLines)
	for i, pl := range popupLines {
		if top+i >= len(baseLines) {
			break
		}
		baseLines[top+i] = lipgloss.PlaceHorizontal(m.width, lipgloss.Center, pl)
	}
	return strings.Join(baseLines, "\n")
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
