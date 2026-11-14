package tui

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pandaCaffeine/diadem/internal/clients"
	"github.com/pandaCaffeine/diadem/internal/httpd"
	"github.com/pandaCaffeine/diadem/internal/library"
	"github.com/pandaCaffeine/diadem/internal/logx"
)

// tab is the screen currently on display.
type tab uint8

const (
	tabLibrary tab = iota
	tabClients
	tabLogs
)

// tickMsg refreshes time-driven views (Clients, Logs) from their sources.
type tickMsg time.Time

const tickEvery = 500 * time.Millisecond

// tick polls tracker and log snapshots on a timer — simple and "almost
// instant" for human eyes.
func tick() tea.Cmd {
	return tea.Tick(tickEvery, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// libraryChangedMsg arrives when the Watcher saw the Library change on
// disk; the reaction is a rescan.
type libraryChangedMsg struct{}

// watchLibraryCmd blocks until the watcher reports a change, then returns
// it as a message. Re-issued after every change to keep watching.
func watchLibraryCmd(w *library.Watcher) tea.Cmd {
	if w == nil {
		return nil
	}
	return func() tea.Msg {
		<-w.Changed()
		return libraryChangedMsg{}
	}
}

// scannedMsg carries a library.Scan result into the TUI event loop.
type scannedMsg struct {
	lib *library.Library
	err error
}

const (
	headerLines = 3 // title, stats, rule
	footerLines = 2 // rule, key hints
)

// Model is the root Bubble Tea model of Diadem.
type Model struct {
	name string // Friendly Name (server name, CONTEXT.md)
	path string // absolute Library path
	port int    // HTTP port

	tab        tab
	tracker    *clients.Tracker
	clientRows []clients.Client
	watcher    *library.Watcher
	dirty      bool // change seen while a scan was already running
	logs       *logx.Recorder
	logFile    string // where --debug writes, shown in the Logs tab

	lib      *library.Library
	tree     *TreeView
	scanning bool
	scanErr  error
	spinner  spinner.Model

	popup *library.Node // Item whose details are open; nil = none

	host    string // primary local IPv4 clients can reach us at
	baseURL string // http://host:port — where the Library is served
	width   int
	height  int
}

// Options is everything the TUI needs from main.
type Options struct {
	Name    string // Friendly Name
	Path    string // absolute Library path
	Port    int
	Tracker *clients.Tracker // nil leaves the Clients tab empty
	Watcher *library.Watcher // nil disables live updates
	Logs    *logx.Recorder   // nil leaves the Logs tab empty
	LogFile string           // --debug file path, "" when off
}

// New builds the model.
func New(opts Options) Model {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(spinnerStyle))
	host := httpd.LocalIPv4()
	return Model{
		name:     opts.Name,
		path:     opts.Path,
		port:     opts.Port,
		tab:      tabLibrary,
		tracker:  opts.Tracker,
		watcher:  opts.Watcher,
		logs:     opts.Logs,
		logFile:  opts.LogFile,
		tree:     NewTreeView(nil),
		scanning: true,
		spinner:  sp,
		host:     host,
		baseURL:  "http://" + net.JoinHostPort(host, strconv.Itoa(opts.Port)),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.scanCmd(), tick(), watchLibraryCmd(m.watcher))
}

// scanCmd runs library.Scan off the render loop, so big folders never
// freeze the interface, and logs how long it took.
func (m Model) scanCmd() tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		lib, err := library.Scan(m.path)
		if err == nil {
			m.logs.Logf("[scan] %d items · %d folders · %s in %s",
				lib.Items, lib.Containers, humanSize(lib.Bytes), time.Since(start).Round(time.Millisecond))
		}
		return scannedMsg{lib: lib, err: err}
	}
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
		if m.dirty { // a change arrived mid-scan: scan again right away
			m.dirty = false
			m.scanning = true
			return m, tea.Batch(m.spinner.Tick, m.scanCmd())
		}
		return m, nil

	case libraryChangedMsg:
		cmds := []tea.Cmd{watchLibraryCmd(m.watcher)} // keep watching either way
		if !m.scanning {
			m.scanning = true
			cmds = append(cmds, m.spinner.Tick, m.scanCmd())
		} else {
			m.dirty = true
		}
		return m, tea.Batch(cmds...)

	case tickMsg:
		if m.tracker != nil {
			m.clientRows = m.tracker.Snapshot()
		}
		return m, tick()

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
	case "tab":
		m.tab = (m.tab + 1) % 3
		return m, nil
	case "shift+tab":
		m.tab = (m.tab + 2) % 3 // back one tab
		return m, nil
	case "r":
		if !m.scanning {
			m.scanning = true
			return m, tea.Batch(m.spinner.Tick, m.scanCmd())
		}
	}

	if m.tab != tabLibrary {
		return m, nil // Clients and Logs are view-only for now
	}

	switch msg.String() {
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

func (m Model) footerView() string {
	var hints string
	if m.tab == tabLibrary {
		hints = strings.Join([]string{
			keyHint("↑/↓", "move"),
			keyHint("⏎", "open"),
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

func (m Model) popupView(n *library.Node) string {
	body := icon(n.Media) + " " + kindStyle(n.Media).Render(n.Name) + "\n\n" + strings.Join([]string{
		fmt.Sprintf("%s  %s", styleDim.Render("Kind"), n.Media),
		fmt.Sprintf("%s  %s", styleDim.Render("Size"), humanSize(n.Size)),
		fmt.Sprintf("%s  %s", styleDim.Render("Path"), truncate(tildePath(n.Path), 60)),
		fmt.Sprintf("%s  %s", styleDim.Render("URL"), truncate(m.baseURL+httpd.FilePathToURL(m.path, n.Path), 60)),
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
