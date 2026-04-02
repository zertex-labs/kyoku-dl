package cmd

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/zertex-labs/kyoku-dl/provider"
	"github.com/zertex-labs/kyoku-dl/provider/deezer"
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
	sessionID   string
	retryID     string
)

var downloadCmd = &cobra.Command{
	Use:   "download <url> [<url>...]",
	Short: "Download tracks or albums from a URL",
	Long: `Download one or more tracks, albums, or artist discographies from supported streaming services.

The URL type (track, album, or artist) is detected automatically from the URL pattern.
For artist URLs, you'll be presented with a selection UI to choose which albums to download.

Quality levels:
  normal    Low quality (96 kbps)
  high      High quality (320 kbps)
  lossless  CD quality (1411 kbps) [default]
  master    Master/Hi-Res quality (up to 9216 kbps)
  hires     Alias for master

Format placeholders:
  %artist%       Album artist (keeps compilations in one folder)
  %albumartist%  Same as %artist%
  %album%        Album title
  %title%        Track title
  %track%        Track number (zero-padded)
  %disc%         Disc number
  %year%         Release year (e.g. "2024")
  %date%         Full release date (e.g. "2024-01-15")
  %id%           Track ID
  %quality%      Audio quality
  %extension%    File extension

Default format: %artist%/%album%/%track% - %title%.%extension%

You can also provide URLs via a file using the -i flag:
  kyoku-dl download -i urls.txt

The file should contain URLs separated by whitespace (spaces, tabs, or newlines).

Sessions:
  If you quit mid-download (Ctrl+C / q), the remaining URLs are saved to a
  session file and a session ID is printed. Resume later with:
    kyoku-dl download -s <session-id>

Failed downloads:
  When downloads finish with track failures (e.g. 429 rate limits), you'll be
  asked whether to retry, save, or ignore. Saved failures can be retried with:
    kyoku-dl download -r <session-id>`,
	Args: cobra.ArbitraryArgs,
	RunE: runDownload,
}

func init() {
	downloadCmd.Flags().StringVarP(&outputDir, "output-dir", "o", "", "output directory for downloaded files")
	downloadCmd.Flags().StringVarP(&format, "format", "f", "", "filename format template (overrides config)")
	downloadCmd.Flags().StringVarP(&quality, "quality", "q", "", "audio quality: normal, high, lossless, master, hires")
	downloadCmd.Flags().BoolVarP(&musicbrainz, "musicbrainz", "m", false, "enrich metadata with genre and MusicBrainz IDs via ISRC lookup")
	downloadCmd.Flags().StringVarP(&inputFile, "input-file", "i", "", "file containing URLs separated by whitespace")
	downloadCmd.Flags().StringVarP(&sessionID, "session", "s", "", "resume a saved session by ID")
	downloadCmd.Flags().StringVarP(&retryID, "retry", "r", "", "retry failed albums from a previous session")
}

func runDownload(cmd *cobra.Command, args []string) error {
	// Collect URLs from args and input file
	urls := append([]string{}, args...)

	// If resuming a session, load URLs from the session file
	if sessionID != "" {
		sessionPath := sessionFilePath(sessionID)
		data, err := os.ReadFile(sessionPath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("session %q not found (looked in %s)", sessionID, sessionPath)
			}
			return fmt.Errorf("failed to read session file: %w", err)
		}
		sessionURLs := strings.Fields(string(data))
		urls = append(urls, sessionURLs...)

		// Remove the session file since we're resuming it
		os.Remove(sessionPath)
	}

	// If retrying failed albums
	if retryID != "" {
		failedPath := failedFilePath(retryID)
		data, err := os.ReadFile(failedPath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("failed session %q not found (looked in %s)", retryID, failedPath)
			}
			return fmt.Errorf("failed to read failed file: %w", err)
		}
		failedURLs := strings.Fields(string(data))
		urls = append(urls, failedURLs...)

		// Remove the failed file since we're retrying
		os.Remove(failedPath)
	}

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
		return fmt.Errorf("no URLs provided; specify URLs as arguments or use -i/--input-file, -s/--session, or -r/--retry")
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
		fmt.Fprintf(os.Stderr, "Warning: Tidal authentication failed: %v\n", err)
	}

	// Authenticate with Deezer
	deezerAuth, deezerLoadErr := deezer.LoadAuth(deezerAuthFile)
	if deezerLoadErr != nil {
		deezerAuth = deezer.Auth{}
	}

	// Only init session if we have an ARL (don't force login for Deezer if user only uses Tidal)
	if deezerAuth.ARL != "" {
		deezerAuth, err = deezer.EnsureAuth(deezerAuthFile, deezerAuth)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Deezer authentication failed: %v\n", err)
		}
	}

	// Create registry and register providers
	registry := provider.NewRegistry()
	tidalProvider := tidal.NewWithPath(tidalAuth, authFile, config)

	// Set up artist session tracking: when the user selects albums from an
	// artist, we immediately save them to a session file so they can be
	// resumed if the process crashes. As each album completes, we update
	// the file to remove it. When all finish, the file is cleaned up.
	var artistSessionID string
	var artistRemaining []string
	var artistMu sync.Mutex

	tidalProvider.OnAlbumsSelected = func(albumURLs []string) {
		artistMu.Lock()
		defer artistMu.Unlock()

		artistSessionID = generateSessionID()
		artistRemaining = make([]string, len(albumURLs))
		copy(artistRemaining, albumURLs)

		saveSession := func() {
			dir := sessionsDir()
			if err := os.MkdirAll(dir, 0755); err != nil {
				return
			}
			path := sessionFilePath(artistSessionID)
			content := strings.Join(artistRemaining, "\n") + "\n"
			os.WriteFile(path, []byte(content), 0644)
		}
		saveSession()

		fmt.Fprintf(os.Stderr, "Artist session: %s (%d album(s))\n", artistSessionID, len(albumURLs))
		fmt.Fprintf(os.Stderr, "Resume with:    kyoku-dl download -s %s\n\n", artistSessionID)
	}

	tidalProvider.OnAlbumComplete = func(albumURL string) {
		artistMu.Lock()
		defer artistMu.Unlock()

		// Remove the completed URL from the remaining list
		remaining := make([]string, 0, len(artistRemaining))
		for _, u := range artistRemaining {
			if u != albumURL {
				remaining = append(remaining, u)
			}
		}
		artistRemaining = remaining

		path := sessionFilePath(artistSessionID)
		if len(artistRemaining) == 0 {
			// All done — remove the session file
			os.Remove(path)
		} else {
			// Update the session file with remaining URLs
			content := strings.Join(artistRemaining, "\n") + "\n"
			os.WriteFile(path, []byte(content), 0644)
		}
	}

	registry.Register(tidalProvider)

	// Create and register Deezer provider
	deezerProvider := deezer.NewWithPath(deezerAuth, deezerAuthFile, config)

	deezerProvider.OnAlbumsSelected = func(albumURLs []string) {
		artistMu.Lock()
		defer artistMu.Unlock()

		artistSessionID = generateSessionID()
		artistRemaining = make([]string, len(albumURLs))
		copy(artistRemaining, albumURLs)

		saveArtistSession := func() {
			dir := sessionsDir()
			if err := os.MkdirAll(dir, 0755); err != nil {
				return
			}
			path := sessionFilePath(artistSessionID)
			content := strings.Join(artistRemaining, "\n") + "\n"
			os.WriteFile(path, []byte(content), 0644)
		}
		saveArtistSession()

		fmt.Fprintf(os.Stderr, "Artist session: %s (%d album(s))\n", artistSessionID, len(albumURLs))
		fmt.Fprintf(os.Stderr, "Resume with:    kyoku-dl download -s %s\n\n", artistSessionID)
	}

	deezerProvider.OnAlbumComplete = func(albumURL string) {
		artistMu.Lock()
		defer artistMu.Unlock()

		remaining := make([]string, 0, len(artistRemaining))
		for _, u := range artistRemaining {
			if u != albumURL {
				remaining = append(remaining, u)
			}
		}
		artistRemaining = remaining

		path := sessionFilePath(artistSessionID)
		if len(artistRemaining) == 0 {
			os.Remove(path)
		} else {
			content := strings.Join(artistRemaining, "\n") + "\n"
			os.WriteFile(path, []byte(content), 0644)
		}
	}

	registry.Register(deezerProvider)

	// Process each URL, tracking failures
	var downloadErrors []string
	var failedAlbumURLs []string // album URLs with partial track failures

	for i, url := range urls {
		select {
		case <-ctx.Done():
			// Save remaining URLs (including the current one) to a session
			remaining := urls[i:]
			saveSession(remaining)
			// Also save any failed albums collected so far
			if len(failedAlbumURLs) > 0 {
				sid := generateSessionID()
				saveFailedAlbums(sid, failedAlbumURLs)
			}
			return fmt.Errorf("interrupted")
		default:
		}

		if err := downloadURL(registry, url); err != nil {
			errStr := err.Error()

			// Collect failed album URLs from partial failures
			var pfe *provider.PartialFailureError
			var fae *provider.FailedAlbumsError

			if errors.As(err, &pfe) {
				failedAlbumURLs = append(failedAlbumURLs, pfe.URL)
				downloadErrors = append(downloadErrors, fmt.Sprintf("  %s", pfe.Error()))
			} else if errors.As(err, &fae) {
				failedAlbumURLs = append(failedAlbumURLs, fae.FailedURLs...)
				for _, d := range fae.Details {
					downloadErrors = append(downloadErrors, d)
				}
			} else if strings.Contains(errStr, "cancelled") || strings.Contains(errStr, "interrupted") {
				// User quit the TUI — save remaining URLs
				remaining := urls[i:]
				saveSession(remaining)
				if len(failedAlbumURLs) > 0 {
					sid := generateSessionID()
					saveFailedAlbums(sid, failedAlbumURLs)
				}
				return err
			} else {
				downloadErrors = append(downloadErrors, fmt.Sprintf("  %s: %v", url, err))
			}
		}
	}

	// If we were resuming a session and finished everything, tell the user
	if sessionID != "" && len(failedAlbumURLs) == 0 {
		fmt.Fprintf(os.Stderr, "Session %s complete — all URLs processed.\n", sessionID)
	}

	// Handle failed albums: prompt user for retry / save / ignore
	if len(failedAlbumURLs) > 0 {
		fmt.Fprintf(os.Stderr, "\n%d album(s) had failed tracks:\n", len(failedAlbumURLs))
		for _, d := range downloadErrors {
			fmt.Fprintf(os.Stderr, "%s\n", d)
		}
		fmt.Fprintln(os.Stderr)

		handleFailedAlbums(registry, failedAlbumURLs)
		return nil
	}

	if len(downloadErrors) > 0 {
		return fmt.Errorf("some downloads failed:\n%s", strings.Join(downloadErrors, "\n"))
	}

	return nil
}

func downloadURL(registry *provider.Registry, url string) error {
	// Resolve share/short links (e.g. link.deezer.com/s/XXX) before matching
	resolved, resolveErr := provider.ResolveURL(url)
	if resolveErr != nil {
		return fmt.Errorf("failed to resolve URL %s: %w", url, resolveErr)
	}
	if resolved != url {
		fmt.Fprintf(os.Stderr, "Resolved: %s\n      → %s\n\n", url, resolved)
		url = resolved
	}

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
	case "artist":
		return p.DownloadArtist(url)
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

// --- Session helpers ---

// sessionsDir returns the directory where session files are stored.
func sessionsDir() string {
	return filepath.Join(DefaultConfigDir(), "sessions")
}

// sessionFilePath returns the full path for a session file given its ID.
func sessionFilePath(id string) string {
	return filepath.Join(sessionsDir(), id+".txt")
}

// generateSessionID creates a short random hex ID (8 chars).
func generateSessionID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// Fallback: use timestamp
		return fmt.Sprintf("%x", os.Getpid())
	}
	return fmt.Sprintf("%x", b)
}

// saveSession writes the remaining URLs to a session file and prints the
// session ID so the user can resume later.
func saveSession(remaining []string) {
	if len(remaining) == 0 {
		return
	}

	dir := sessionsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not create sessions directory: %v\n", err)
		return
	}

	id := generateSessionID()
	path := sessionFilePath(id)

	content := strings.Join(remaining, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save session: %v\n", err)
		return
	}

	fmt.Fprintf(os.Stderr, "\nSession saved: %s (%d URL(s) remaining)\n", id, len(remaining))
	fmt.Fprintf(os.Stderr, "Resume with:  kyoku-dl download -s %s\n", id)
}

// --- Failed album helpers ---

// failedDir returns the directory where failed-album files are stored.
func failedDir() string {
	return filepath.Join(DefaultConfigDir(), "failed")
}

// failedFilePath returns the full path for a failed-albums file given its ID.
func failedFilePath(id string) string {
	return filepath.Join(failedDir(), id+".txt")
}

// saveFailedAlbums writes the failed album URLs to a file and prints the
// session ID so the user can retry later with -r.
func saveFailedAlbums(id string, urls []string) {
	if len(urls) == 0 {
		return
	}

	dir := failedDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not create failed directory: %v\n", err)
		return
	}

	path := failedFilePath(id)
	content := strings.Join(urls, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save failed albums: %v\n", err)
		return
	}

	fmt.Fprintf(os.Stderr, "Failed albums saved: %s (%d album(s))\n", id, len(urls))
	fmt.Fprintf(os.Stderr, "Retry with:          kyoku-dl download -r %s\n", id)
}

// handleFailedAlbums prompts the user to retry, save, or ignore albums that
// had partial track failures. If retry is chosen, the albums are re-downloaded
// and any still-failing albums are handled recursively.
func handleFailedAlbums(registry *provider.Registry, failedURLs []string) {
	fmt.Fprintf(os.Stderr, "What would you like to do?\n")
	fmt.Fprintf(os.Stderr, "  [R] Retry now (default)\n")
	fmt.Fprintf(os.Stderr, "  [S] Save for later\n")
	fmt.Fprintf(os.Stderr, "  [I] Ignore\n")
	fmt.Fprintf(os.Stderr, "> ")

	scanner := bufio.NewScanner(os.Stdin)
	choice := "r" // default
	if scanner.Scan() {
		text := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if text != "" {
			choice = text
		}
	} else {
		// EOF or interrupt (Ctrl+C) — treat as ignore and exit cleanly
		fmt.Fprintf(os.Stderr, "\n")
		return
	}

	switch choice {
	case "s", "save":
		sid := generateSessionID()
		saveFailedAlbums(sid, failedURLs)

	case "i", "ignore":
		fmt.Fprintf(os.Stderr, "Ignored %d failed album(s).\n", len(failedURLs))

	default: // "r", "retry", or enter
		fmt.Fprintf(os.Stderr, "\nRetrying %d album(s)...\n\n", len(failedURLs))

		var stillFailed []string
		for _, url := range failedURLs {
			if err := downloadURL(registry, url); err != nil {
				var pfe *provider.PartialFailureError
				var fae *provider.FailedAlbumsError

				if errors.As(err, &pfe) {
					stillFailed = append(stillFailed, pfe.URL)
					fmt.Fprintf(os.Stderr, "  Still failing: %s\n", pfe.Error())
				} else if errors.As(err, &fae) {
					stillFailed = append(stillFailed, fae.FailedURLs...)
					for _, d := range fae.Details {
						fmt.Fprintf(os.Stderr, "  Still failing: %s\n", d)
					}
				} else {
					fmt.Fprintf(os.Stderr, "  Error: %s: %v\n", url, err)
				}
			}
		}

		if len(stillFailed) > 0 {
			fmt.Fprintf(os.Stderr, "\n%d album(s) still have failures.\n", len(stillFailed))
			handleFailedAlbums(registry, stillFailed)
		} else {
			fmt.Fprintf(os.Stderr, "\nAll retries succeeded!\n")
		}
	}
}
