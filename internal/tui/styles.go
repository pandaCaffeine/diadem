// Package tui renders the Diadem terminal interface.
package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/pandaCaffeine/diadem/internal/library"
)

var (
	colorCrown  = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FBBF24"}
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
	styleCrown   = lipgloss.NewStyle().Bold(true).Foreground(colorCrown)
	styleTitle   = lipgloss.NewStyle().Bold(true).Foreground(colorTitle)
	styleDim     = lipgloss.NewStyle().Foreground(colorDim)
	styleKey     = lipgloss.NewStyle().Bold(true).Foreground(colorTitle)
	styleError   = lipgloss.NewStyle().Foreground(colorError)
	styleFolder  = lipgloss.NewStyle().Bold(true).Foreground(colorFolder)
	styleVideo   = lipgloss.NewStyle().Foreground(colorVideo)
	styleAudio   = lipgloss.NewStyle().Foreground(colorAudio)
	stylePhoto   = lipgloss.NewStyle().Foreground(colorPhoto)
	styleSel     = lipgloss.NewStyle().Background(colorSelBg).Foreground(colorSelFg)
	popupStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorTitle).Padding(1, 3)
	spinnerStyle = lipgloss.NewStyle().Foreground(colorTitle)
)

// kindStyle picks the row color for an Item by its MediaKind.
func kindStyle(k library.MediaKind) lipgloss.Style {
	switch k {
	case library.MediaVideo:
		return styleVideo
	case library.MediaAudio:
		return styleAudio
	case library.MediaPhoto:
		return stylePhoto
	}
	return styleDim
}

// icon returns the friendly glyph shown before an Item's name.
func icon(k library.MediaKind) string {
	switch k {
	case library.MediaVideo:
		return "🎬"
	case library.MediaAudio:
		return "🎵"
	case library.MediaPhoto:
		return "🏞️"
	}
	return "·"
}
