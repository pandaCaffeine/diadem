package tui

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pandaCaffeine/diadem/internal/library"
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

	m, view := runToView(t, New("Diadem", dir, 8200))
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
	m, view := runToView(t, New("Diadem", dir, 8200), "j", "j", "enter")
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

func TestRescanKeepsTreeUsable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.mkv"), make([]byte, 1), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := runToView(t, New("Diadem", dir, 8200))

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
