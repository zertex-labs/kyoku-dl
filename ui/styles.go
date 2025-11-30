package ui

import (
	"github.com/charmbracelet/lipgloss"
)

// Colors
var (
	Primary    = lipgloss.Color("#7C3AED") // Purple
	Secondary  = lipgloss.Color("#06B6D4") // Cyan
	Success    = lipgloss.Color("#10B981") // Green
	Warning    = lipgloss.Color("#F59E0B") // Amber
	Error      = lipgloss.Color("#EF4444") // Red
	Muted      = lipgloss.Color("#6B7280") // Gray
	Background = lipgloss.Color("#1F2937") // Dark gray
	Foreground = lipgloss.Color("#F9FAFB") // White
)

// Text styles
var (
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Primary).
			MarginBottom(1)

	SubtitleStyle = lipgloss.NewStyle().
			Foreground(Secondary).
			Italic(true)

	BoldStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Foreground)

	MutedStyle = lipgloss.NewStyle().
			Foreground(Muted)

	SuccessStyle = lipgloss.NewStyle().
			Foreground(Success)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(Error)

	WarningStyle = lipgloss.NewStyle().
			Foreground(Warning)
)

// Component styles
var (
	// Header for the app
	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Primary).
			Background(Background).
			Padding(0, 1).
			MarginBottom(1)

	// Box for containing content
	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Primary).
			Padding(1, 2)

	// Progress bar styles
	ProgressBarComplete = lipgloss.NewStyle().
				Foreground(Success)

	ProgressBarIncomplete = lipgloss.NewStyle().
				Foreground(Muted)

	// Track item styles
	TrackNumberStyle = lipgloss.NewStyle().
				Foreground(Muted).
				Width(6)

	TrackTitleStyle = lipgloss.NewStyle().
			Foreground(Foreground).
			Bold(true)

	TrackArtistStyle = lipgloss.NewStyle().
				Foreground(Secondary)

	TrackStatusPending = lipgloss.NewStyle().
				Foreground(Muted)

	TrackStatusDownloading = lipgloss.NewStyle().
				Foreground(Warning)

	TrackStatusComplete = lipgloss.NewStyle().
				Foreground(Success)

	TrackStatusError = lipgloss.NewStyle().
				Foreground(Error)

	// Info panel styles
	InfoLabelStyle = lipgloss.NewStyle().
			Foreground(Muted).
			Width(12)

	InfoValueStyle = lipgloss.NewStyle().
			Foreground(Foreground)

	// Spinner
	SpinnerStyle = lipgloss.NewStyle().
			Foreground(Primary)

	// Help text
	HelpStyle = lipgloss.NewStyle().
			Foreground(Muted).
			MarginTop(1)
)

// Icons/Symbols
const (
	IconCheck    = "✓"
	IconCross    = "✗"
	IconArrow    = "→"
	IconMusic    = "♫"
	IconAlbum    = "💿"
	IconDownload = "↓"
	IconSpinner  = "◐"
	IconDot      = "•"
	IconPlay     = "▶"
	IconPause    = "⏸"
	IconProgress = "█"
	IconEmpty    = "░"
)

// Helper functions
func RenderProgressBar(percent float64, width int) string {
	filled := int(percent / 100 * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}

	bar := ""
	for i := 0; i < width; i++ {
		if i < filled {
			bar += ProgressBarComplete.Render(IconProgress)
		} else {
			bar += ProgressBarIncomplete.Render(IconEmpty)
		}
	}
	return bar
}

func StatusIcon(status string) string {
	switch status {
	case "complete":
		return SuccessStyle.Render(IconCheck)
	case "error":
		return ErrorStyle.Render(IconCross)
	case "downloading":
		return WarningStyle.Render(IconDownload)
	default:
		return MutedStyle.Render(IconDot)
	}
}
