package provider

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// DownloadBufferSize is the buffer size for downloads (256KB)
	DownloadBufferSize = 256 * 1024
)

// ProgressWriter wraps an io.Writer to track and display download progress.
type ProgressWriter struct {
	Total      int64
	Written    int64
	StartTime  time.Time
	LastUpdate time.Time
	FileName   string
}

// Write implements io.Writer and updates progress.
func (pw *ProgressWriter) Write(p []byte) (int, error) {
	n := len(p)
	atomic.AddInt64(&pw.Written, int64(n))

	// Update progress every 100ms to avoid too frequent updates
	now := time.Now()
	if now.Sub(pw.LastUpdate) >= 100*time.Millisecond {
		pw.LastUpdate = now
		pw.printProgress()
	}

	return n, nil
}

func (pw *ProgressWriter) printProgress() {
	written := atomic.LoadInt64(&pw.Written)
	elapsed := time.Since(pw.StartTime).Seconds()

	if elapsed == 0 {
		elapsed = 0.001 // Avoid division by zero
	}

	speed := float64(written) / elapsed
	speedStr := formatSpeed(speed)

	if pw.Total > 0 {
		percent := float64(written) / float64(pw.Total) * 100
		bar := renderProgressBar(percent, 30)

		// Calculate ETA
		remaining := float64(pw.Total-written) / speed
		etaStr := formatETA(remaining)

		fmt.Printf("\r  %s %5.1f%% │ %s/%s │ %s │ ETA: %s",
			bar,
			percent,
			formatBytes(written),
			formatBytes(pw.Total),
			speedStr,
			etaStr,
		)
	} else {
		// Unknown total size
		fmt.Printf("\r  Downloaded: %s │ %s",
			formatBytes(written),
			speedStr,
		)
	}
}

// Finish prints the final progress line.
func (pw *ProgressWriter) Finish() {
	written := atomic.LoadInt64(&pw.Written)
	elapsed := time.Since(pw.StartTime).Seconds()
	speed := float64(written) / elapsed

	if pw.Total > 0 {
		bar := renderProgressBar(100, 30)
		fmt.Printf("\r  %s %5.1f%% │ %s │ %s │ Done!          \n",
			bar,
			100.0,
			formatBytes(written),
			formatSpeed(speed),
		)
	} else {
		fmt.Printf("\r  Downloaded: %s │ %s │ Done!          \n",
			formatBytes(written),
			formatSpeed(speed),
		)
	}
}

func renderProgressBar(percent float64, width int) string {
	filled := int(percent / 100 * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return bar
}

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
		return fmt.Sprintf("%.2f MB", float64(bytes)/(1024*1024))
	case bytes >= 1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func formatETA(seconds float64) string {
	if seconds < 0 || seconds > 86400 {
		return "--:--"
	}
	mins := int(seconds) / 60
	secs := int(seconds) % 60
	return fmt.Sprintf("%02d:%02d", mins, secs)
}

// DownloadFile downloads a file from a URL with optimized buffering and progress display.
func DownloadFile(streamURL, filePath string) (int64, error) {
	// Create HTTP request with headers
	req, err := http.NewRequest("GET", streamURL, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers that might help with download speed
	req.Header.Set("User-Agent", "kyoku-dl/1.0")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity") // Disable compression for streaming
	req.Header.Set("Connection", "keep-alive")

	// Use a client with optimized settings
	client := &http.Client{
		Timeout: 0, // No timeout for large files
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			DisableCompression:  true, // Already compressed audio
			DisableKeepAlives:   false,
			MaxIdleConnsPerHost: 5,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download failed with status: %s", resp.Status)
	}

	// Create directory structure if needed
	if dir := GetDirectory(filePath); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return 0, fmt.Errorf("failed to create directory: %w", err)
		}
	}

	// Create output file
	out, err := os.Create(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	// Set up progress tracking
	progress := &ProgressWriter{
		Total:      resp.ContentLength,
		StartTime:  time.Now(),
		LastUpdate: time.Now(),
		FileName:   filePath,
	}

	// Use a buffered writer for better performance
	buffer := make([]byte, DownloadBufferSize)

	// Create a TeeReader to track progress while copying
	reader := io.TeeReader(resp.Body, progress)

	// Copy with our large buffer
	written, err := io.CopyBuffer(out, reader, buffer)
	if err != nil {
		fmt.Println() // New line after progress bar
		return written, fmt.Errorf("failed to write file: %w", err)
	}

	// Print final progress
	progress.Finish()

	return written, nil
}

// DownloadFileSimple downloads without progress bar (for batch operations where you don't want spam).
func DownloadFileSimple(streamURL, filePath string) (int64, error) {
	req, err := http.NewRequest("GET", streamURL, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "kyoku-dl/1.0")
	req.Header.Set("Accept-Encoding", "identity")

	client := &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			DisableCompression: true,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download failed with status: %s", resp.Status)
	}

	if dir := GetDirectory(filePath); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return 0, fmt.Errorf("failed to create directory: %w", err)
		}
	}

	out, err := os.Create(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	buffer := make([]byte, DownloadBufferSize)
	written, err := io.CopyBuffer(out, resp.Body, buffer)
	if err != nil {
		return written, fmt.Errorf("failed to write file: %w", err)
	}

	return written, nil
}
