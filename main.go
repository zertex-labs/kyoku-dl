package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/zertex-labs/kyoku-dl/provider"
	"github.com/zertex-labs/kyoku-dl/provider/tidal"
)

const (
	ConfigFilename    = "config.json"
	TidalAuthFilename = "tidal-auth.json"
)

func loadConfig(path string) (provider.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return provider.DefaultConfig(), nil // Config is optional
	}

	var config provider.Config
	if err := json.Unmarshal(data, &config); err != nil {
		return provider.DefaultConfig(), fmt.Errorf("failed to parse config: %w", err)
	}

	return config, nil
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <url>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "A command-line tool to download music from various streaming services.\n\n")
		fmt.Fprintf(os.Stderr, "Supported providers:\n")
		fmt.Fprintf(os.Stderr, "  - Tidal (tracks and albums)\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s https://tidal.com/browse/track/123456789\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s https://tidal.com/browse/album/123456789\n", os.Args[0])
	}

	debug := flag.Bool("debug", false, "Enable debug output")
	showInfo := flag.Bool("info", false, "Show audio metadata after download (requires ffprobe)")
	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}

	// Load config
	config, err := loadConfig(ConfigFilename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	if *debug {
		config.Debug = true
	}
	if *showInfo {
		config.ShowInfo = true
	}

	// Create registry and register providers
	registry := provider.NewRegistry()

	// Register Tidal provider
	tidalProvider, err := tidal.NewFromFiles(TidalAuthFilename, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Tidal provider not available: %v\n", err)
	} else {
		registry.Register(tidalProvider)
	}

	// Get URL argument
	url := flag.Arg(0)

	// Find matching provider and download
	p, err := registry.GetProviderForURL(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintf(os.Stderr, "\nSupported providers: %v\n", registry.ListProviders())
		os.Exit(1)
	}

	if !p.IsAuthenticated() {
		fmt.Fprintf(os.Stderr, "Error: Not authenticated for %s. Please check your auth file.\n", p.Name())
		os.Exit(1)
	}

	// Try to download (track first, then album)
	if err := p.DownloadTrack(url); err != nil {
		// If track download fails, try album
		if albumErr := p.DownloadAlbum(url); albumErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}
