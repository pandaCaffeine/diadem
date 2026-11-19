package tui

import (
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/pandaCaffeine/diadem/internal/clients"
	"github.com/pandaCaffeine/diadem/internal/httpd"
	"github.com/pandaCaffeine/diadem/internal/library"
	"github.com/pandaCaffeine/diadem/internal/logx"
	"github.com/pandaCaffeine/diadem/internal/watch"
)

// tab is the screen currently on display.
type tab uint8

const (
	tabLibrary tab = iota
	tabClients
	tabLogs
)

// Model is the root Bubble Tea model of Diadem.
type Model struct {
	name  string // Friendly Name (server name, CONTEXT.md)
	path  string // absolute Library path
	port  int    // HTTP port
	store *library.Store

	tab        tab
	tracker    *clients.Tracker
	clientRows []clients.Client
	watcher    *library.Watcher
	dirty      bool // change seen while a scan was already running
	logs       *logx.Recorder
	logFile    string // where --debug writes, shown in the Logs tab
	watch      *watch.Tracker

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
	Store   *library.Store   // the Library snapshot owner; nil scans ad hoc
	Tracker *clients.Tracker // nil leaves the Clients tab empty
	Watcher *library.Watcher // nil disables live updates
	Logs    *logx.Recorder   // nil leaves the Logs tab empty
	LogFile string           // --debug file path, "" when off
	Watch   *watch.Tracker   // nil leaves Items unmarked
}

// New builds the model.
func New(opts Options) Model {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(spinnerStyle))
	host := httpd.LocalIPv4()
	path := opts.Path
	if opts.Store != nil {
		path = opts.Store.Root()
	}
	return Model{
		name:     opts.Name,
		path:     path,
		port:     opts.Port,
		store:    opts.Store,
		tab:      tabLibrary,
		tracker:  opts.Tracker,
		watcher:  opts.Watcher,
		logs:     opts.Logs,
		logFile:  opts.LogFile,
		watch:    opts.Watch,
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

// handleKey is the keymap. While a popup is open it swallows every key
// but the closers (esc, enter, q, tab, space) — with a popup up, q
// closes it instead of quitting. Next come the global keys (quit, tab
// switch, rescan); the tree keys apply to the Library tab only.
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
	case "w":
		if cur := m.tree.Current(); cur != nil {
			m.toggleWatched(cur.Node)
		}
	}
	return m, nil
}

// toggleWatched flips the Watched mark of a video Item by hand
// (CONTEXT.md: Watched). Photos and audio are never marked.
func (m Model) toggleWatched(n *library.Node) {
	k, ok := m.watchKey(n)
	if !ok {
		return
	}
	m.watch.ToggleWatched(k)
}

// isWatched reports whether the tree should show the mark for this Node.
func (m Model) isWatched(n *library.Node) bool {
	k, ok := m.watchKey(n)
	if !ok {
		return false
	}
	return m.watch.Watched(k)
}

// watchKey maps a Node to its Watch State key (CONTEXT.md: Watched):
// only video Items with a known size can carry the mark, addressed as
// the path relative to the Library root plus the file size.
func (m Model) watchKey(n *library.Node) (watch.Key, bool) {
	if m.watch == nil || n.Kind != library.Item || n.Media != library.MediaVideo || n.Size <= 0 {
		return watch.Key{}, false
	}
	rel, err := filepath.Rel(m.path, n.Path)
	if err != nil {
		return watch.Key{}, false
	}
	return watch.Key{RelPath: rel, Size: n.Size}, true
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
