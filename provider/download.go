package provider

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	// DownloadBufferSize is the buffer size for downloads (256KB)
	DownloadBufferSize = 256 * 1024
)

// NewHTTPClient creates an optimized HTTP client for downloads.
func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 0, // No timeout for large files
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			DisableCompression:  true, // Already compressed audio
			DisableKeepAlives:   false,
			MaxIdleConnsPerHost: 5,
		},
	}
}

// NewDownloadRequest creates an HTTP request configured for downloading.
func NewDownloadRequest(url string) (*http.Request, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "kyoku-dl/1.0")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Connection", "keep-alive")

	return req, nil
}

// EnsureDirectory creates the directory structure for a file path if needed.
func EnsureDirectory(filePath string) error {
	if dir := GetDirectory(filePath); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}
	return nil
}
