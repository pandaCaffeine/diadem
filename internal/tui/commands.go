package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pandaCaffeine/diadem/internal/library"
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

// scanCmd runs the scan off the render loop, so big folders never
// freeze the interface, and logs how long it took. The Store version
// keeps the probe cache warm across rescans; the fallback without one
// scans ad hoc (tests).
func (m Model) scanCmd() tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		var lib *library.Library
		var err error
		if m.store != nil {
			lib, err = m.store.Scan()
		} else {
			lib, err = library.Scan(m.path)
		}
		if err == nil {
			m.logs.Logf("[scan] %d items · %d folders · %s in %s",
				lib.Items, lib.Containers, humanSize(lib.Bytes), time.Since(start).Round(time.Millisecond))
		}
		return scannedMsg{lib: lib, err: err}
	}
}
