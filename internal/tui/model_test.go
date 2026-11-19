package tui

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pandaCaffeine/diadem/internal/clients"
	"github.com/pandaCaffeine/diadem/internal/library"
	"github.com/pandaCaffeine/diadem/internal/logx"
	"github.com/pandaCaffeine/diadem/internal/watch"
)

// runToView drives the model through resize + scan like the real program
// would and returns the rendered view.
func runToView(t *testing.T, m Model, keys ...string) (Model, string) {
	t.Helper()
	m.host = "127.0.0.1" // deterministic in tests
	m.baseURL = "http://" + net.JoinHostPort(m.host, strconv.Itoa(m.port))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)

	lib, err := library.Scan(m.path)
	if err != nil {
		t.Fatal(err)
	}
	next, _ = m.Update(scannedMsg{lib: lib})
	m = next.(Model)

	for _, k := range keys {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		m = next.(Model)
	}
	return m, m.View()
}

func TestViewRendersLibrary(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "movies"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct {
		path string
		size int
	}{
		{"movies/Big Buck Bunny.mkv", 1 << 30},
		{"movies/s01e01.mp4", 1 << 20},
		{"bangarang.flac", 1 << 20},
		{"notes.txt", 100},
	} {
		if err := os.WriteFile(filepath.Join(dir, f.path), make([]byte, f.size), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m, view := runToView(t, New(Options{Name: "Diadem", Path: dir, Port: 8200}))
	for _, want := range []string{
		"Diadem", "http://127.0.0.1:8200", // header: title and serving URL
		"movies",                   // container row
		"Big Buck Bunny.mkv",       // item row
		"bangarang.flac", "1.0 MB", // sibling item and human size
		"rescan", "quit", // footer hints
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view does not contain %q\nview:\n%s", want, view)
		}
	}
	if strings.Contains(view, "notes.txt") {
		t.Error("view shows a non-media file")
	}
	if m.lib == nil || m.lib.Items != 3 {
		t.Errorf("Items = %v, want 3", m.lib)
	}
}

func TestNavigationAndPopup(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "a.mp4"), make([]byte, 42), 0o644); err != nil {
		t.Fatal(err)
	}

	// rows: root, sub, a.mp4 — walk down to the item and open details.
	m, view := runToView(t, New(Options{Name: "Diadem", Path: dir, Port: 8200}), "j", "j", "enter")
	for _, want := range []string{
		"Kind", "video", "42 B", "a.mp4",
		"URL", "http://127.0.0.1:8200/files/sub/a.mp4", // fetchable URL in the popup
	} {
		if !strings.Contains(view, want) {
			t.Errorf("popup does not contain %q\nview:\n%s", want, view)
		}
	}

	// Any key closes the popup; 'q' afterwards quits.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = next.(Model)
	if m.popup != nil {
		t.Error("popup still open after q")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("second q should quit, got nil command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("second q did not produce tea.QuitMsg")
	}
}

func TestHumanDuration(t *testing.T) {
	cases := map[time.Duration]string{
		0:                             "--:--",
		-1:                            "--:--",
		42 * time.Second:              "00:00",
		42 * time.Minute:              "00:42",
		90 * time.Minute:              "01:30",
		10 * time.Hour:                "10:00",
		25*time.Hour + 13*time.Minute: "25:13",
	}
	for d, want := range cases {
		if got := humanDuration(d); got != want {
			t.Errorf("humanDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestDurationColumnAndPopup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "movie.mp4"), make([]byte, 1<<20), 0o644); err != nil {
		t.Fatal(err)
	}

	// A fake file cannot be probed: the column shows the placeholder.
	m, view := runToView(t, New(Options{Name: "Diadem", Path: dir, Port: 8200}))
	if !strings.Contains(view, "--:--") {
		t.Errorf("view misses the --:-- placeholder:\n%s", view)
	}

	// With metadata known, the column renders HH:mm and the popup gains
	// Duration and Bitrate lines (1 MB over an hour ≈ 291 B/s).
	for _, c := range m.lib.Root.Children {
		if c.Kind == library.Item {
			c.Meta.Duration = time.Hour
			c.Meta.Title = "The Movie"
		}
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("enter")})
	m = next.(Model)
	view = m.View()
	for _, want := range []string{
		"01:00", // the row column…
		"Title", "The Movie",
		"Duration", "01:00",
		"Bitrate", "291 B/s",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("popup misses %q:\n%s", want, view)
		}
	}
}

func TestRescanKeepsTreeUsable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.mkv"), make([]byte, 1), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := runToView(t, New(Options{Name: "Diadem", Path: dir, Port: 8200}))

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = next.(Model)
	if !m.scanning || cmd == nil {
		t.Fatal("rescan key did not start a scan")
	}
	if err := os.WriteFile(filepath.Join(dir, "b.mkv"), make([]byte, 1), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := library.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	next, _ = m.Update(scannedMsg{lib: lib})
	m = next.(Model)
	if m.scanning || m.lib.Items != 2 {
		t.Errorf("after rescan: scanning=%v items=%d, want false/2", m.scanning, m.lib.Items)
	}
}

func TestClientsTab(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "movie.mkv"), make([]byte, 1), 0o644); err != nil {
		t.Fatal(err)
	}

	tracker := clients.NewTracker()
	m, view := runToView(t, New(Options{Name: "Diadem", Path: dir, Port: 8200, Tracker: tracker}))

	// No clients yet: the empty state shows after switching tabs.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	view = m.View()
	if !strings.Contains(view, "no clients yet") {
		t.Errorf("clients tab should show the empty state:\n%s", view)
	}
	if strings.Contains(view, "↑/↓") {
		t.Error("clients tab footer should not advertise tree navigation")
	}

	// A client appears once activity happens and the tick fires.
	tracker.ObserveBrowse("192.0.2.9", "VLC/3.0.21 LibVLC/3.0.21", "movies")
	next, _ = m.Update(tickMsg{})
	m = next.(Model)
	view = m.View()
	for _, want := range []string{"192.0.2.9", "VLC/3.0.21", "browsing movies", "now"} {
		if !strings.Contains(view, want) {
			t.Errorf("clients tab missing %q:\n%s", want, view)
		}
	}

	// Shift+Tab toggles back to the library tree (Tab now cycles on to Logs).
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = next.(Model)
	if !strings.Contains(m.View(), "movie.mkv") {
		t.Error("switching back should show the library tree again")
	}
}

func TestLogsTab(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "movie.mkv"), make([]byte, 1), 0o644); err != nil {
		t.Fatal(err)
	}
	logs, err := logx.New("")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := runToView(t, New(Options{Name: "Diadem", Path: dir, Port: 8200, Logs: logs}))

	// The scan itself logged a summary line; two tabs over, it is visible.
	logs.Logf("[test] hello from the logger")
	next, _ := m.Update(tickMsg{})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	view := m.View()
	for _, want := range []string{"LOGS", "[test] hello from the logger"} {
		if !strings.Contains(view, want) {
			t.Errorf("logs tab missing %q:\n%s", want, view)
		}
	}

	// A scan run through the model's own command leaves a timing line.
	cmd := m.scanCmd()
	if msg, ok := cmd().(scannedMsg); ok && msg.err == nil {
		next, _ := m.Update(tickMsg{})
		m = next.(Model)
		view = m.View()
		if !strings.Contains(view, "[scan]") {
			t.Errorf("logs tab missing the [scan] line:\n%s", view)
		}
	}
}

func TestAutoRescanOnFileChange(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.mkv"), make([]byte, 1), 0o644); err != nil {
		t.Fatal(err)
	}
	watcher, err := library.NewWatcher(dir)
	if err != nil {
		t.Skipf("fsnotify unavailable: %v", err)
	}
	defer func() { _ = watcher.Close() }()

	m, _ := runToView(t, New(Options{Name: "Diadem", Path: dir, Port: 8200, Watcher: watcher}))
	if m.lib == nil || m.lib.Items != 1 {
		t.Fatalf("initial scan: %+v", m.lib)
	}

	// A file dropped into the library while the server runs…
	if err := os.WriteFile(filepath.Join(dir, "late-arrival.flac"), make([]byte, 1), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-watcher.Changed():
	case <-time.After(3 * time.Second):
		t.Fatal("watcher did not report the change")
	}

	// …triggers a rescan in the model…
	next, _ := m.Update(libraryChangedMsg{})
	m = next.(Model)
	if !m.scanning {
		t.Fatal("library change should trigger a rescan")
	}

	// …and the refreshed tree shows it, keeping the collapse state.
	lib, err := library.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	next, _ = m.Update(scannedMsg{lib: lib})
	m = next.(Model)
	if m.lib.Items != 2 || !strings.Contains(m.View(), "late-arrival.flac") {
		t.Errorf("rescan did not pick up the new file:\n%s", m.View())
	}
}

func TestToggleWatched(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "movies"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct {
		path string
		size int
	}{
		{"movies/s01e01.mkv", 11},
		{"song.flac", 12},
	} {
		if err := os.WriteFile(filepath.Join(dir, f.path), make([]byte, f.size), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tr := watch.New(filepath.Join(dir, ".diadem", "watched.json"), nil)
	tr.Load()
	m := New(Options{Path: dir, Watch: tr})
	m.host = "127.0.0.1"
	m.baseURL = "http://" + net.JoinHostPort(m.host, strconv.Itoa(m.port))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	lib, err := library.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	next, _ = m.Update(scannedMsg{lib: lib})
	m = next.(Model)

	// Find the video and the audio Node in the scanned tree.
	var video, audio *library.Node
	for _, c := range lib.Root.Children {
		if c.Kind == library.Container {
			video = c.Children[0]
		} else {
			audio = c
		}
	}

	// Audio is never marked.
	m.toggleWatched(audio)
	if m.isWatched(audio) {
		t.Error("audio got marked")
	}

	m.toggleWatched(video)
	if !m.isWatched(video) {
		t.Fatal("video not marked by the toggle")
	}
	if _, view := m, m.View(); !strings.Contains(view, "✓") {
		t.Errorf("view does not show the mark:\n%s", view)
	}

	// The mark persists and reloads under the path+size+mtime key.
	if err := tr.Flush(); err != nil {
		t.Fatal(err)
	}
	reloaded := watch.New(filepath.Join(dir, ".diadem", "watched.json"), nil)
	reloaded.Load()
	rel, err := filepath.Rel(dir, video.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Watched(watch.KeyFor(rel, video)) {
		t.Error("mark did not persist under the path+size+mtime key")
	}

	// The `w` keypress path marks whatever the cursor sits on.
	m.toggleWatched(video) // off again
	if m.isWatched(video) {
		t.Fatal("second toggle did not unmark")
	}
	m.tree.Move(1) // from the root onto the first child…
	for m.tree.Current() != nil && m.tree.Current().Node != video {
		m.tree.Move(1) // …and on until the video row
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	m = next.(Model)
	if !m.isWatched(video) {
		t.Error("`w` did not mark the item under the cursor")
	}

	// A nil tracker must not panic anywhere.
	bare := New(Options{Path: dir})
	bare.host, bare.baseURL = m.host, m.baseURL
	bare.toggleWatched(video)
	if bare.isWatched(video) {
		t.Error("nil tracker reported a mark")
	}
}
