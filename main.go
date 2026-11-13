// Diadem — a console DLNA media server.
//
// Step 1: scan the Library folder and browse it in the TUI.
// The network stack (HTTP, UPnP, SSDP) arrives in the next steps; see PLAN.md.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pandaCaffeine/diadem/internal/tui"
)

func main() {
	name := flag.String("name", "Diadem", "server name shown to clients")
	path := flag.String("path", ".", "folder to share (default: current folder)")
	port := flag.Int("port", 8200, "HTTP port to serve on")
	flag.Parse()

	if err := run(*name, *path, *port); err != nil {
		fmt.Fprintln(os.Stderr, "diadem:", err)
		os.Exit(1)
	}
}

func run(name, path string, port int) error {
	if name == "" {
		return fmt.Errorf("--name must not be empty")
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("--port must be between 1 and 65535, got %d", port)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("cannot share %s: %w", abs, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a folder", abs)
	}

	program := tea.NewProgram(tui.New(name, abs, port), tea.WithAltScreen())
	_, err = program.Run()
	return err
}
