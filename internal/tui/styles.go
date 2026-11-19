// Package tui renders the Diadem terminal interface.
package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/pandaCaffeine/diadem/internal/library"
)

var (
	colorStar   = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FBBF24"}
	colorTitle  = lipgloss.AdaptiveColor{Light: "#6D28D9", Dark: "#C4B5FD"}
	colorDim    = lipgloss.AdaptiveColor{Light: "#64748B", Dark: "#7C8394"}
	colorFolder = lipgloss.AdaptiveColor{Light: "#0369A1", Dark: "#7DD3FC"}
	colorVideo  = lipgloss.AdaptiveColor{Light: "#4338CA", Dark: "#A5B4FC"}
	colorAudio  = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#86EFAC"}
	colorPhoto  = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FCD34D"}
	colorError  = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#FCA5A5"}
	colorSelBg  = lipgloss.AdaptiveColor{Light: "#EDE9FE", Dark: "#3B3355"}
	colorSelFg  = lipgloss.AdaptiveColor{Light: "#1E1B4B", Dark: "#F5F3FF"}
)

var (
	styleStar    = lipgloss.NewStyle().Bold(true).Foreground(colorStar)
	styleTitle   = lipgloss.NewStyle().Bold(true).Foreground(colorTitle)
	styleDim     = lipgloss.NewStyle().Foreground(colorDim)
	styleKey     = lipgloss.NewStyle().Bold(true).Foreground(colorTitle)
	styleError   = lipgloss.NewStyle().Foreground(colorError)
	styleFolder  = lipgloss.NewStyle().Bold(true).Foreground(colorFolder)
	styleVideo   = lipgloss.NewStyle().Foreground(colorVideo)
	styleAudio   = lipgloss.NewStyle().Foreground(colorAudio)
	stylePhoto   = lipgloss.NewStyle().Foreground(colorPhoto)
	styleWatched = lipgloss.NewStyle().Foreground(colorAudio) // green: the Item has been Watched
	styleSel     = lipgloss.NewStyle().Background(colorSelBg).Foreground(colorSelFg)
	popupStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorTitle).Padding(1, 3)
	spinnerStyle = lipgloss.NewStyle().Foreground(colorTitle)
)

// kindStyles maps a Media Kind to its row color.
var kindStyles = map[library.MediaKind]lipgloss.Style{
	library.MediaVideo: styleVideo,
	library.MediaAudio: styleAudio,
	library.MediaPhoto: stylePhoto,
}

// kindIcons maps a Media Kind to the friendly glyph shown before an
// Item's name.
var kindIcons = map[library.MediaKind]string{
	library.MediaVideo: "🎬",
	library.MediaAudio: "🎵",
	library.MediaPhoto: "🏞️",
}

// kindStyle picks the row color for an Item by its Media Kind, dim for
// anything unrecognized.
func kindStyle(k library.MediaKind) lipgloss.Style {
	if s, ok := kindStyles[k]; ok {
		return s
	}
	return styleDim
}

// icon returns the friendly glyph shown before an Item's name.
func icon(k library.MediaKind) string {
	if s, ok := kindIcons[k]; ok {
		return s
	}
	return "·"
}
