// Diadem — a console DLNA media server.
//
// Step 1: scan the Library folder and browse it in the TUI.
// The network stack (HTTP, UPnP, SSDP) arrives in the next steps; see PLAN.md.
package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pandaCaffeine/diadem/internal/clients"
	"github.com/pandaCaffeine/diadem/internal/dlna"
	"github.com/pandaCaffeine/diadem/internal/httpd"
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

	// Bind the port before the TUI takes over the screen, so a busy port
	// fails fast with a readable message instead of a silent dead server.
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("cannot listen on port %d: %w", port, err)
	}

	// One mux, one port: files for media bytes, everything UPnP for
	// control. The advertised BaseURL must match what clients will dial.
	// One UUID everywhere: description.xml, SSDP advertisements and
	// M-SEARCH replies must speak in the same voice.
	tracker := clients.NewTracker()
	baseURL := "http://" + net.JoinHostPort(httpd.LocalIPv4(), strconv.Itoa(port))
	uuid := dlna.NewUUID()
	dlnaServer := dlna.New(dlna.Config{
		Root:         abs,
		BaseURL:      baseURL,
		FriendlyName: name,
		UUID:         uuid,
		Observer:     tracker,
	})

	filesServer := httpd.New(abs)
	filesServer.Observer = tracker
	mux := http.NewServeMux()
	mux.Handle("/files/", filesServer.Handler())
	mux.Handle("/", dlnaServer)

	httpServer := &http.Server{Handler: mux}
	go func() { _ = httpServer.Serve(listener) }()

	// Advertise on the LAN so clients like VLC find us without typing URLs.
	ssdp, err := dlnaServer.StartSSDP()
	if err != nil {
		return fmt.Errorf("cannot start SSDP discovery: %w", err)
	}

	program := tea.NewProgram(tui.New(name, abs, port, tracker), tea.WithAltScreen())
	_, err = program.Run()

	byeErr := ssdp.Close() // ssdp:byebye, so clients drop us immediately
	_ = httpServer.Close()
	if err != nil {
		return err
	}
	return byeErr
}
