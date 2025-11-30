package ui

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zertex-labs/kyoku-dl/provider"
)

const (
	downloadBufferSize = 256 * 1024 // 256KB
)

// DownloadManager handles downloading with a TUI
type DownloadManager struct {
	program *tea.Program
	model   *DownloadModel
	config  provider.Config
}

// NewDownloadManager creates a new download manager
func NewDownloadManager(title, artist, providerName string, isAlbum bool, tracks []TrackInfo, config provider.Config) *DownloadManager {
	model := NewDownloadModel(title, artist, providerName, isAlbum, tracks)

	return &DownloadManager{
		model:  model,
		config: config,
	}
}

// DownloadTask represents a single download task
type DownloadTask struct {
	Index     int
	TrackInfo TrackInfo
	StreamURL string
	FilePath  string
}

// Run starts the TUI and downloads
func (dm *DownloadManager) Run(tasks []DownloadTask) error {
	// Create the program
	dm.program = tea.NewProgram(dm.model)

	// Start downloads in a goroutine
	go dm.downloadAll(tasks)

	// Run the TUI
	if _, err := dm.program.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	if dm.model.Quitting {
		return fmt.Errorf("download cancelled by user")
	}

	return nil
}

func (dm *DownloadManager) downloadAll(tasks []DownloadTask) {
	for _, task := range tasks {
		if dm.model.Quitting {
			break
		}

		// Send start message
		dm.program.Send(TrackStartMsg{Index: task.Index})

		// Download the file
		err := dm.downloadFile(task)
		if err != nil {
			dm.program.Send(TrackErrorMsg{Index: task.Index, Error: err.Error()})
			continue
		}

		// Send complete message
		dm.program.Send(TrackCompleteMsg{Index: task.Index})

		// Show audio info if enabled
		if dm.config.ShowInfo {
			if info, probeErr := provider.ProbeAudioFile(task.FilePath); probeErr == nil {
				info.Provider = dm.model.Provider
				provider.PrintAudioInfo(info)
			}
		}
	}

	// Signal all downloads complete
	dm.program.Send(AllCompleteMsg{})
}

func (dm *DownloadManager) downloadFile(task DownloadTask) error {
	// Create HTTP request
	req, err := http.NewRequest("GET", task.StreamURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "kyoku-dl/1.0")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Connection", "keep-alive")

	client := &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			DisableCompression:  true,
			DisableKeepAlives:   false,
			MaxIdleConnsPerHost: 5,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %s", resp.Status)
	}

	// Create directory structure if needed
	if dir := provider.GetDirectory(task.FilePath); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}

	// Create output file
	out, err := os.Create(task.FilePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	// Set up progress tracking
	total := resp.ContentLength
	var written int64
	startTime := time.Now()
	lastUpdate := time.Now()
	buffer := make([]byte, downloadBufferSize)

	for {
		if dm.model.Quitting {
			return fmt.Errorf("cancelled")
		}

		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			_, writeErr := out.Write(buffer[:n])
			if writeErr != nil {
				return fmt.Errorf("failed to write: %w", writeErr)
			}
			atomic.AddInt64(&written, int64(n))

			// Update progress every 100ms
			now := time.Now()
			if now.Sub(lastUpdate) >= 100*time.Millisecond {
				lastUpdate = now
				elapsed := now.Sub(startTime).Seconds()
				if elapsed > 0 {
					speed := float64(written) / elapsed
					progress := float64(0)
					if total > 0 {
						progress = float64(written) / float64(total) * 100
					}
					dm.program.Send(TrackProgressMsg{
						Index:      task.Index,
						Progress:   progress,
						Downloaded: written,
						Size:       total,
						Speed:      speed,
					})
				}
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("failed to read: %w", readErr)
		}
	}

	return nil
}

// RunSimple runs downloads without TUI (for non-interactive mode)
func RunSimple(tasks []DownloadTask, config provider.Config, providerName string) error {
	for i, task := range tasks {
		fmt.Printf("\n[%d/%d] Downloading: %s\n", i+1, len(tasks), task.TrackInfo.Title)

		written, err := provider.DownloadFile(task.StreamURL, task.FilePath)
		if err != nil {
			fmt.Printf("  Error: %v\n", err)
			continue
		}

		fmt.Printf("  Saved: %s (%s)\n", task.FilePath, formatBytes(written))

		if config.ShowInfo {
			if info, probeErr := provider.ProbeAudioFile(task.FilePath); probeErr == nil {
				info.Provider = providerName
				provider.PrintAudioInfo(info)
			}
		}
	}

	return nil
}
