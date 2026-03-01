package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/zertex-labs/kyoku-dl/provider"
	"github.com/zertex-labs/kyoku-dl/provider/tidal"
)

// Quality mapping from friendly names to Tidal API values.
var qualityMap = map[string]string{
	"normal":   "LOW",
	"high":     "HIGH",
	"lossless": "LOSSLESS",
	"master":   "HI_RES_LOSSLESS",
	"hires":    "HI_RES_LOSSLESS",
}

var (
	outputDir   string
	format      string
	quality     string
	musicbrainz bool
	inputFile   string
)

var downloadCmd = &cobra.Command{
	Use:   "download <url> [<url>...]",
	Short: "Download tracks or albums from a URL",
	Long: `Download one or more tracks or albums from supported streaming services.

The URL type (track or album) is detected automatically from the URL pattern.

Quality levels:
  normal    Low quality (96 kbps)
  high      High quality (320 kbps)
  lossless  CD quality (1411 kbps) [default]
  master    Master/Hi-Res quality (up to 9216 kbps)
  hires     Alias for master

Format placeholders:
  %artist%     Track artist
  %album%      Album title
  %title%      Track title
  %track%      Track number (zero-padded)
  %disc%       Disc number
  %year%       Release year (e.g. "2024")
  %date%       Full release date (e.g. "2024-01-15")
  %id%         Track ID
  %quality%    Audio quality
  %extension%  File extension

Default format: %artist%/%album%/%track% - %title%.%extension%

You can also provide URLs via a file using the -i flag:
  kyoku-dl download -i urls.txt

The file should contain URLs separated by whitespace (spaces, tabs, or newlines).`,
	Args: cobra.ArbitraryArgs,
	RunE: runDownload,
}

func init() {
	downloadCmd.Flags().StringVarP(&outputDir, "output-dir", "o", "", "output directory for downloaded files")
	downloadCmd.Flags().StringVarP(&format, "format", "f", "", "filename format template (overrides config)")
	downloadCmd.Flags().StringVarP(&quality, "quality", "q", "", "audio quality: normal, high, lossless, master, hires")
	downloadCmd.Flags().BoolVarP(&musicbrainz, "musicbrainz", "m", false, "enrich metadata with genre and MusicBrainz IDs via ISRC lookup")
	downloadCmd.Flags().StringVarP(&inputFile, "input-file", "i", "", "file containing URLs separated by whitespace")
}

func runDownload(cmd *cobra.Command, args []string) error {
	// Collect URLs from args and input file
	urls := append([]string{}, args...)

	if inputFile != "" {
		data, err := os.ReadFile(inputFile)
		if err != nil {
			return fmt.Errorf("failed to read input file: %w", err)
		}
		// Split by whitespace (spaces, tabs, newlines)
		fileURLs := strings.Fields(string(data))
		urls = append(urls, fileURLs...)
	}

	if len(urls) == 0 {
		return fmt.Errorf("no URLs provided; specify URLs as arguments or use -i/--input-file")
	}

	// Check for ffprobe
	checkFfprobe()

	// Build config with flag overrides
	config := appConfig
	if format != "" {
		config.Format = format
	}
	if outputDir != "" {
		config.OutputDir = outputDir
	}
	if quality != "" {
		mapped, ok := qualityMap[strings.ToLower(quality)]
		if !ok {
			validKeys := make([]string, 0, len(qualityMap))
			for k := range qualityMap {
				validKeys = append(validKeys, k)
			}
			return fmt.Errorf("invalid quality %q, valid options: %s", quality, strings.Join(validKeys, ", "))
		}
		config.Quality = mapped
	}
	if musicbrainz {
		config.MusicBrainz = true
	}

	// Set up signal handling for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Ensure config directory exists (for auth file)
	if err := EnsureConfigDir(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not create config directory: %v\n", err)
	}

	// Authenticate with Tidal
	tidalAuth, loadErr := tidal.LoadAuth(authFile)
	if loadErr != nil {
		tidalAuth = tidal.Auth{}
	}

	tidalAuth, err := tidal.EnsureAuth(authFile, tidalAuth)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Create registry and register providers
	registry := provider.NewRegistry()
	tidalProvider := tidal.NewWithPath(tidalAuth, authFile, config)
	registry.Register(tidalProvider)

	// Process each URL
	var errors []string
	for _, url := range urls {
		select {
		case <-ctx.Done():
			fmt.Fprintf(os.Stderr, "\nInterrupted, stopping downloads...\n")
			return fmt.Errorf("interrupted")
		default:
		}

		if err := downloadURL(registry, url); err != nil {
			errors = append(errors, fmt.Sprintf("  %s: %v", url, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("some downloads failed:\n%s", strings.Join(errors, "\n"))
	}

	return nil
}

func downloadURL(registry *provider.Registry, url string) error {
	p, err := registry.GetProviderForURL(url)
	if err != nil {
		return err
	}

	if !p.IsAuthenticated() {
		return fmt.Errorf("not authenticated for %s", p.Name())
	}

	// Determine URL type and call the appropriate method
	switch p.URLType(url) {
	case "track":
		return p.DownloadTrack(url)
	case "album":
		return p.DownloadAlbum(url)
	default:
		return fmt.Errorf("could not determine content type for URL: %s", url)
	}
}

func checkFfprobe() {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: ffprobe not found in PATH. Audio metadata will not be displayed after download.\n")
		fmt.Fprintf(os.Stderr, "         Install ffmpeg/ffprobe to see codec, sample rate, and bit depth info.\n\n")
	}
}
