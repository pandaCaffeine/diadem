package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/pandaCaffeine/diadem/internal/httpd"
	"github.com/pandaCaffeine/diadem/internal/library"
)

func (m Model) popupView(n *library.Node) string {
	lines := []string{
		fmt.Sprintf("%s  %s", styleDim.Render("Kind"), n.Media),
	}
	if t := n.Meta.Title; t != "" && t != n.Name {
		lines = append(lines, fmt.Sprintf("%s  %s", styleDim.Render("Title"), truncate(t, 60)))
	}
	if n.Meta.Duration > 0 {
		lines = append(lines, fmt.Sprintf("%s  %s", styleDim.Render("Duration"), humanDuration(n.Meta.Duration)))
		if n.Size > 0 {
			lines = append(lines, fmt.Sprintf("%s  %s", styleDim.Render("Bitrate"), humanBitrate(n.Size, n.Meta.Duration)))
		}
	}
	if at, ok := m.watchedAt(n); ok {
		lines = append(lines, fmt.Sprintf("%s  %s", styleDim.Render("Watched"),
			time.Unix(at, 0).Format("2006-01-02 15:04")))
	}
	lines = append(lines,
		fmt.Sprintf("%s  %s", styleDim.Render("Size"), humanSize(n.Size)),
		fmt.Sprintf("%s  %s", styleDim.Render("Path"), truncate(tildePath(n.Path), 60)),
		fmt.Sprintf("%s  %s", styleDim.Render("URL"), truncate(m.baseURL+httpd.FilePathToURL(m.path, n.Path), 60)),
	)
	body := icon(n.Media) + " " + kindStyle(n.Media).Render(n.Name) + "\n\n" + strings.Join(lines, "\n")
	return popupStyle.Render(body)
}

// watchedAt reports when this Node became Watched, for the details popup.
func (m Model) watchedAt(n *library.Node) (int64, bool) {
	if m.watch == nil || n.Kind != library.Item || n.Media != library.MediaVideo || n.Size <= 0 {
		return 0, false
	}
	rel, err := filepath.Rel(m.path, n.Path)
	if err != nil {
		return 0, false
	}
	return m.watch.MarkedAt(rel, n.Size)
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
