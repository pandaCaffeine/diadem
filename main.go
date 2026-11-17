// Diadem — a console DLNA media server.
//
// Run it in a folder of media files and DLNA clients on the LAN (VLC on a
// TV or laptop) discover it, browse the folder tree and stream the files.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pandaCaffeine/diadem/internal/clients"
	"github.com/pandaCaffeine/diadem/internal/dlna"
	"github.com/pandaCaffeine/diadem/internal/httpd"
	"github.com/pandaCaffeine/diadem/internal/library"
	"github.com/pandaCaffeine/diadem/internal/logx"
	"github.com/pandaCaffeine/diadem/internal/tui"
)

// version is stamped at release time:
//
//	go build -ldflags "-X main.version=v0.1.0"
//
// Empty means a plain build; versionString picks the best fallback.
var version string

// versionString knows where each channel gets its version from: the
// ldflags value (brew, release builds), the module version (go install
// …@v0.1.0), or the git revision of a working-copy build.
func versionString() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		// A pseudo-version (v0.0.0-…) means "no real tag" — fall through
		// to the nicer git-revision form below.
		if v := info.Main.Version; v != "" && v != "(devel)" && !strings.HasPrefix(v, "v0.0.0-") {
			return v
		}
		var rev, dirty string
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if len(s.Value) >= 7 {
					rev = s.Value[:7]
				}
			case "vcs.modified":
				if s.Value == "true" {
					dirty = "-dirty"
				}
			}
		}
		if rev != "" {
			return "dev-" + rev + dirty
		}
	}
	return "dev"
}

func main() {
	name := flag.String("name", "Diadem", "server name shown to clients")
	path := flag.String("path", ".", "folder to share (default: current folder)")
	port := flag.Int("port", 8200, "HTTP port to serve on")
	debug := flag.Bool("debug", false, "write a verbose log to diadem.log in the temp folder")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("★ Diadem", versionString())
		return
	}

	if err := run(*name, *path, *port, *debug); err != nil {
		fmt.Fprintln(os.Stderr, "diadem:", err)
		os.Exit(1)
	}
}

func run(name, path string, port int, debug bool) error {
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

	// The in-memory ring feeds the Logs tab; --debug adds a file mirror.
	logPath := ""
	if debug {
		logPath = filepath.Join(os.TempDir(), "diadem.log")
	}
	logs, err := logx.New(logPath)
	if err != nil {
		return fmt.Errorf("cannot open log file %s: %w", logPath, err)
	}

	// Bind the port before the TUI takes over the screen, so a busy port
	// fails fast with a readable message instead of a silent dead server.
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			return fmt.Errorf("port %d is busy — another diadem or media server running? try --port %d", port, port+1)
		}
		return fmt.Errorf("cannot listen on port %d: %w", port, err)
	}

	// One mux, one port: files for media bytes, everything UPnP for
	// control. The advertised BaseURL must match what clients will dial.
	// One UUID everywhere: description.xml, SSDP advertisements and
	// M-SEARCH replies must speak in the same voice. One Store: the
	// scanned Library tree feeds both the TUI and ContentDirectory.
	tracker := clients.NewTracker()
	baseURL := "http://" + net.JoinHostPort(httpd.LocalIPv4(), strconv.Itoa(port))
	uuid := dlna.NewUUID()
	store := library.NewStore(abs, logs)
	dlnaServer := dlna.New(dlna.Config{
		Root:         abs,
		BaseURL:      baseURL,
		FriendlyName: name,
		UUID:         uuid,
		Source:       store,
		Observer:     tracker,
		Logger:       logs,
	})

	filesServer := httpd.New(abs)
	filesServer.Observer = tracker
	filesServer.Logger = logs
	mux := http.NewServeMux()
	mux.Handle("/files/", filesServer.Handler())
	mux.Handle("/", dlnaServer)

	httpServer := &http.Server{Handler: mux}
	go func() { _ = httpServer.Serve(listener) }()
	logs.Logf("[http] listening on :%d, library %s", port, abs)

	// Advertise on the LAN so clients like VLC find us without typing URLs.
	ssdp, err := dlnaServer.StartSSDP()
	if err != nil {
		return fmt.Errorf("cannot start SSDP discovery: %w", err)
	}

	// Watch the Library tree so new files appear without a restart.
	watcher, err := library.NewWatcher(abs)
	if err != nil {
		return fmt.Errorf("cannot watch %s: %w", abs, err)
	}

	program := tea.NewProgram(tui.New(tui.Options{
		Name:    name,
		Path:    abs,
		Port:    port,
		Store:   store,
		Tracker: tracker,
		Watcher: watcher,
		Logs:    logs,
		LogFile: logPath,
	}), tea.WithAltScreen())
	_, err = program.Run()

	// Graceful wind-down: tell clients goodbye, let transfers drain for a
	// couple of seconds, stop watching, flush the log.
	byeErr := ssdp.Close() // ssdp:byebye, so clients drop us immediately
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
	watchErr := watcher.Close()
	logs.Logf("[main] stopped")
	closeErr := logs.Close()
	if err != nil {
		return err
	}

	fmt.Println("★ Diadem stopped.")
	if logPath != "" {
		fmt.Println("Debug log:", logPath)
	}
	if byeErr != nil {
		return byeErr
	}
	if watchErr != nil {
		return watchErr
	}
	return closeErr
}
