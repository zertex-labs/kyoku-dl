package ui

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zertex-labs/kyoku-dl/provider"
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
	Metadata  provider.Track // Full track metadata for tagging
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

	if dm.model.IsQuitting() {
		return fmt.Errorf("download cancelled by user")
	}

	return nil
}

func (dm *DownloadManager) downloadAll(tasks []DownloadTask) {
	// Create MusicBrainz client if enrichment is enabled
	var mbClient *provider.MBClient
	if dm.config.MusicBrainz {
		mbClient = provider.NewMBClient()
	}

	// mbResult channels for parallel lookahead: start MB lookup for the
	// current task while downloading, so the ~1s API latency is hidden
	// behind download time.
	type mbResult struct {
		err error
	}

	// Start the first MB lookup in parallel with the first download
	var mbCh chan mbResult
	if mbClient != nil && len(tasks) > 0 && tasks[0].Metadata.ISRC != "" {
		mbCh = make(chan mbResult, 1)
		go func(t *provider.Track) {
			mbCh <- mbResult{err: mbClient.Enrich(t)}
		}(&tasks[0].Metadata)
	}

	for i, task := range tasks {
		if dm.model.IsQuitting() {
			break
		}

		// Skip if file already exists with non-zero size
		if info, err := os.Stat(task.FilePath); err == nil && info.Size() > 0 {
			dm.program.Send(TrackSkippedMsg{Index: task.Index})
			// Drain the current MB result if it was started for this track
			if mbCh != nil {
				<-mbCh
				mbCh = nil
			}
			continue
		}

		// Send start message
		dm.program.Send(TrackStartMsg{Index: task.Index})

		// Start MB lookup for the NEXT task in parallel with this download
		var nextMBCh chan mbResult
		if mbClient != nil && i+1 < len(tasks) && tasks[i+1].Metadata.ISRC != "" {
			nextMBCh = make(chan mbResult, 1)
			go func(t *provider.Track) {
				nextMBCh <- mbResult{err: mbClient.Enrich(t)}
			}(&tasks[i+1].Metadata)
		}

		// Download the file
		err := dm.downloadFile(task)
		if err != nil {
			dm.program.Send(TrackErrorMsg{Index: task.Index, Error: err.Error()})
			// Drain the current MB result if it was started
			if mbCh != nil {
				<-mbCh
			}
			mbCh = nextMBCh
			continue
		}

		// Wait for the current track's MB enrichment to finish (if started)
		if mbCh != nil {
			<-mbCh
			// task.Metadata was mutated in-place by the goroutine via pointer
		}

		// Tag the downloaded file with metadata (now potentially enriched)
		if tagErr := provider.TagAudioFile(task.FilePath, tasks[i].Metadata); tagErr != nil {
			// Tagging failure is non-fatal -- the file is still playable
			_ = tagErr
		}

		// Probe audio info
		var audioInfo *provider.AudioInfo
		if info, probeErr := provider.ProbeAudioFile(task.FilePath); probeErr == nil {
			info.Provider = dm.model.Provider
			audioInfo = info
		}

		// Send complete message with audio info
		dm.program.Send(TrackCompleteMsg{Index: task.Index, AudioInfo: audioInfo})

		// Shift the next MB channel to current for the next iteration
		mbCh = nextMBCh
	}

	// Signal all downloads complete
	dm.program.Send(AllCompleteMsg{})
}

func (dm *DownloadManager) downloadFile(task DownloadTask) error {
	// Create HTTP request
	req, err := provider.NewDownloadRequest(task.StreamURL)
	if err != nil {
		return err
	}

	client := provider.NewHTTPClient()

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed with status: %s", resp.Status)
	}

	// Create directory structure if needed
	if err := provider.EnsureDirectory(task.FilePath); err != nil {
		return err
	}

	// Create output file
	out, err := os.Create(task.FilePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	// Track whether the download completed successfully
	downloadOK := false
	defer func() {
		out.Close()
		if !downloadOK {
			// Clean up partial file on failure or cancellation
			os.Remove(task.FilePath)
		}
	}()

	// Set up progress tracking
	total := resp.ContentLength
	var written int64
	startTime := time.Now()
	lastUpdate := time.Now()
	buffer := make([]byte, provider.DownloadBufferSize)

	for {
		if dm.model.IsQuitting() {
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

	downloadOK = true
	return nil
}
