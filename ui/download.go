package ui

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/zertex-labs/kyoku-dl/provider"
)

// TrackStatus represents the download status of a track
type TrackStatus int

const (
	StatusPending TrackStatus = iota
	StatusDownloading
	StatusComplete
	StatusError
	StatusSkipped
)

// TrackInfo holds information about a track being downloaded
type TrackInfo struct {
	ID         string
	Title      string
	Artist     string
	Album      string
	Status     TrackStatus
	Progress   float64
	Size       int64
	Downloaded int64
	Speed      float64
	Error      string
	AudioInfo  *provider.AudioInfo // Audio metadata after download
}

// DownloadModel is the bubbletea model for the download UI
type DownloadModel struct {
	// Content info
	Title    string
	Artist   string
	IsAlbum  bool
	Provider string

	// Tracks
	Tracks       []TrackInfo
	CurrentTrack int

	// UI components
	spinner  spinner.Model
	progress progress.Model

	// State
	Done      bool
	quitting  atomic.Bool
	StartTime time.Time

	// Callbacks
	mu sync.Mutex
}

// IsQuitting returns whether the user has requested cancellation.
// Safe to call from any goroutine.
func (m *DownloadModel) IsQuitting() bool {
	return m.quitting.Load()
}

// Messages
type TickMsg time.Time
type TrackStartMsg struct {
	Index int
}
type TrackProgressMsg struct {
	Index      int
	Progress   float64
	Downloaded int64
	Size       int64
	Speed      float64
}
type TrackCompleteMsg struct {
	Index     int
	AudioInfo *provider.AudioInfo
}
type TrackErrorMsg struct {
	Index int
	Error string
}
type TrackSkippedMsg struct {
	Index int
}
type AllCompleteMsg struct{}

// NewDownloadModel creates a new download UI model
func NewDownloadModel(title, artist, provider string, isAlbum bool, tracks []TrackInfo) *DownloadModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = SpinnerStyle

	p := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(40),
		progress.WithoutPercentage(),
	)

	return &DownloadModel{
		Title:     title,
		Artist:    artist,
		IsAlbum:   isAlbum,
		Provider:  provider,
		Tracks:    tracks,
		spinner:   s,
		progress:  p,
		StartTime: time.Now(),
	}
}

// Init initializes the model
func (m *DownloadModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// Update handles messages
func (m *DownloadModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting.Store(true)
			return m, tea.Quit
		}

	case TickMsg:
		return m, tickCmd()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case TrackStartMsg:
		m.mu.Lock()
		if msg.Index < len(m.Tracks) {
			m.Tracks[msg.Index].Status = StatusDownloading
			m.CurrentTrack = msg.Index
		}
		m.mu.Unlock()

	case TrackProgressMsg:
		m.mu.Lock()
		if msg.Index < len(m.Tracks) {
			m.Tracks[msg.Index].Progress = msg.Progress
			m.Tracks[msg.Index].Downloaded = msg.Downloaded
			m.Tracks[msg.Index].Size = msg.Size
			m.Tracks[msg.Index].Speed = msg.Speed
		}
		m.mu.Unlock()

	case TrackCompleteMsg:
		m.mu.Lock()
		if msg.Index < len(m.Tracks) {
			m.Tracks[msg.Index].Status = StatusComplete
			m.Tracks[msg.Index].Progress = 100
			m.Tracks[msg.Index].AudioInfo = msg.AudioInfo
		}
		m.mu.Unlock()

	case TrackErrorMsg:
		m.mu.Lock()
		if msg.Index < len(m.Tracks) {
			m.Tracks[msg.Index].Status = StatusError
			m.Tracks[msg.Index].Error = msg.Error
		}
		m.mu.Unlock()

	case TrackSkippedMsg:
		m.mu.Lock()
		if msg.Index < len(m.Tracks) {
			m.Tracks[msg.Index].Status = StatusSkipped
		}
		m.mu.Unlock()

	case AllCompleteMsg:
		m.Done = true
		return m, tea.Quit

	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd
	}

	return m, nil
}

// View renders the UI
func (m *DownloadModel) View() string {
	if m.quitting.Load() {
		return MutedStyle.Render("Download cancelled.\n")
	}

	var b strings.Builder

	// Header
	icon := IconMusic
	if m.IsAlbum {
		icon = IconAlbum
	}
	header := fmt.Sprintf("%s %s", icon, TitleStyle.Render(m.Title))
	if m.Artist != "" {
		header += " " + MutedStyle.Render("by") + " " + SubtitleStyle.Render(m.Artist)
	}
	b.WriteString(header + "\n")
	b.WriteString(MutedStyle.Render(fmt.Sprintf("Provider: %s", m.Provider)) + "\n\n")

	// Track list
	completedCount := 0
	skippedCount := 0
	for i, track := range m.Tracks {
		completedCount += m.renderTrack(&b, i, track)
		if track.Status == StatusSkipped {
			skippedCount++
		}
	}
	downloadedCount := completedCount - skippedCount

	// Summary
	b.WriteString("\n")
	elapsed := time.Since(m.StartTime).Round(time.Second)

	if m.Done {
		summaryParts := fmt.Sprintf("%s Downloaded %d/%d tracks in %s",
			SuccessStyle.Render(IconCheck),
			downloadedCount,
			len(m.Tracks),
			elapsed,
		)
		if skippedCount > 0 {
			summaryParts += fmt.Sprintf(" (%d skipped)", skippedCount)
		}
		b.WriteString(BoxStyle.Render(summaryParts))
		b.WriteString("\n")
	} else {
		// Overall progress
		overallProgress := float64(completedCount) / float64(len(m.Tracks))
		progressBar := m.progress.ViewAs(overallProgress)

		summary := fmt.Sprintf("Progress: %s %d/%d tracks • Elapsed: %s",
			progressBar,
			completedCount,
			len(m.Tracks),
			elapsed,
		)
		b.WriteString(MutedStyle.Render(summary))
		b.WriteString("\n")
	}

	// Help
	if !m.Done {
		b.WriteString(HelpStyle.Render("Press q to quit"))
	}

	return b.String()
}

func (m *DownloadModel) renderTrack(b *strings.Builder, index int, track TrackInfo) int {
	completed := 0

	// Track number
	num := TrackNumberStyle.Render(fmt.Sprintf("[%d]", index+1))

	// Status icon and styling
	var status, title string
	switch track.Status {
	case StatusPending:
		status = MutedStyle.Render(IconDot)
		title = MutedStyle.Render(track.Title)
	case StatusDownloading:
		status = m.spinner.View()
		title = TrackTitleStyle.Render(track.Title)
	case StatusComplete:
		status = SuccessStyle.Render(IconCheck)
		title = TrackTitleStyle.Render(track.Title)
		completed = 1
	case StatusError:
		status = ErrorStyle.Render(IconCross)
		title = ErrorStyle.Render(track.Title)
	case StatusSkipped:
		status = WarningStyle.Render(IconSkip)
		title = MutedStyle.Render(track.Title)
		completed = 1
	}

	// First line: status, number, title
	line := fmt.Sprintf("%s %s %s", status, num, title)

	// If downloading, show progress on same line
	if track.Status == StatusDownloading {
		progressBar := RenderProgressBar(track.Progress, 20)
		speedStr := formatSpeed(track.Speed)
		sizeStr := formatBytes(track.Downloaded)
		if track.Size > 0 {
			sizeStr = fmt.Sprintf("%s/%s", formatBytes(track.Downloaded), formatBytes(track.Size))
		}
		line += " " + progressBar + " " + MutedStyle.Render(fmt.Sprintf("%s • %s", sizeStr, speedStr))
	}

	// If error, show error message
	if track.Status == StatusError && track.Error != "" {
		line += " " + ErrorStyle.Render(fmt.Sprintf("(%s)", track.Error))
	}

	// If skipped, show reason
	if track.Status == StatusSkipped {
		line += " " + MutedStyle.Render("(already exists)")
	}

	// If complete and has audio info, append to the same line
	if track.Status == StatusComplete && track.AudioInfo != nil {
		info := track.AudioInfo

		// Format: codec • sample rate • bit depth • bitrate • size
		details := []string{}

		if info.Codec != "" {
			details = append(details, strings.ToUpper(info.Codec))
		}
		if info.SampleRate != "" {
			details = append(details, fmt.Sprintf("%s Hz", info.SampleRate))
		}
		if info.BitDepth != "" && info.BitDepth != "0" {
			details = append(details, fmt.Sprintf("%s-bit", info.BitDepth))
		}
		if info.Channels > 0 {
			layout := info.ChannelLayout
			if layout == "" {
				if info.Channels == 2 {
					layout = "stereo"
				} else if info.Channels == 1 {
					layout = "mono"
				}
			}
			details = append(details, layout)
		}
		if info.Bitrate > 0 {
			details = append(details, fmt.Sprintf("%d kbps", info.Bitrate/1000))
		}
		if info.FileSize > 0 {
			details = append(details, formatBytes(info.FileSize))
		}

		if len(details) > 0 {
			line += " " + MutedStyle.Render("→ "+strings.Join(details, " • "))
		}
	}

	b.WriteString(line + "\n")

	return completed
}

// Helper functions
func formatSpeed(bytesPerSec float64) string {
	switch {
	case bytesPerSec >= 1024*1024:
		return fmt.Sprintf("%.1f MB/s", bytesPerSec/(1024*1024))
	case bytesPerSec >= 1024:
		return fmt.Sprintf("%.1f KB/s", bytesPerSec/1024)
	default:
		return fmt.Sprintf("%.0f B/s", bytesPerSec)
	}
}

func formatBytes(bytes int64) string {
	switch {
	case bytes >= 1024*1024*1024:
		return fmt.Sprintf("%.2f GB", float64(bytes)/(1024*1024*1024))
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	case bytes >= 1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
