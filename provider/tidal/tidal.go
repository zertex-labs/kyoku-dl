package tidal

import (
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zertex-labs/kyoku-dl/provider"
	"github.com/zertex-labs/kyoku-dl/ui"
)

const (
	APIBaseURL     = "https://api.tidal.com/v1/"
	DefaultTimeout = 10 * time.Second
)

// URL patterns for matching Tidal links
var (
	trackPattern  = regexp.MustCompile(`tidal\.com.*?/track/(\d+)`)
	albumPattern  = regexp.MustCompile(`tidal\.com.*?/album/(\d+)`)
	artistPattern = regexp.MustCompile(`tidal\.com.*?/artist/(\d+)`)
)

// Tidal implements the provider.Provider interface for Tidal.
type Tidal struct {
	auth       Auth
	authPath   string
	config     provider.Config
	httpClient *http.Client
	debug      bool

	// OnAlbumsSelected is called after the user selects albums in an artist
	// download, before any downloads begin. The callback receives the album
	// URLs that will be downloaded. Callers can use this to create a session
	// file for crash recovery.
	OnAlbumsSelected func(albumURLs []string)

	// OnAlbumComplete is called after each album finishes downloading (success
	// or failure) during an artist download. The callback receives the album
	// URL that just completed. Callers can use this to update a session file.
	OnAlbumComplete func(albumURL string)
}

// New creates a new Tidal provider with the given auth and config.
func New(auth Auth, config provider.Config) *Tidal {
	return &Tidal{
		auth:   auth,
		config: config,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		debug: config.Debug,
	}
}

// NewWithPath creates a new Tidal provider with the given auth, config, and auth file path
// so that refreshed tokens can be persisted to disk.
func NewWithPath(auth Auth, authPath string, config provider.Config) *Tidal {
	t := New(auth, config)
	t.authPath = authPath
	return t
}

// NewFromFiles loads auth from a JSON file and creates a Tidal provider.
func NewFromFiles(authPath string, config provider.Config) (*Tidal, error) {
	auth, err := LoadAuth(authPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load auth: %w", err)
	}

	return NewWithPath(auth, authPath, config), nil
}

// LoadAuth reads authentication data from a JSON file.
func LoadAuth(path string) (Auth, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Auth{}, fmt.Errorf("failed to read auth file: %w", err)
	}

	var auth Auth
	if err := json.Unmarshal(data, &auth); err != nil {
		return Auth{}, fmt.Errorf("failed to parse auth file: %w", err)
	}

	return auth, nil
}

// --- Provider Interface Implementation ---

// Name returns the service name.
func (t *Tidal) Name() string {
	return "tidal"
}

// MatchesURL returns true if the URL belongs to Tidal.
func (t *Tidal) MatchesURL(rawURL string) bool {
	return trackPattern.MatchString(rawURL) || albumPattern.MatchString(rawURL) || artistPattern.MatchString(rawURL)
}

// URLType returns the type of content the URL points to ("track", "album", "artist", or "").
func (t *Tidal) URLType(rawURL string) string {
	if trackPattern.MatchString(rawURL) {
		return "track"
	}
	if albumPattern.MatchString(rawURL) {
		return "album"
	}
	if artistPattern.MatchString(rawURL) {
		return "artist"
	}
	return ""
}

// IsAuthenticated checks if the provider has valid credentials.
func (t *Tidal) IsAuthenticated() bool {
	return t.auth.Token != "" && t.auth.Token != "YOUR_TIDAL_AUTH_TOKEN_HERE"
}

// SetAuth updates the provider's authentication credentials and auth file path.
func (t *Tidal) SetAuth(auth Auth, authPath string) {
	t.auth = auth
	t.authPath = authPath
}

// ensureValidToken checks if the token is expired and refreshes it if needed.
func (t *Tidal) ensureValidToken() error {
	if !t.auth.IsTokenExpired() {
		return nil
	}

	if t.auth.RefreshToken == "" {
		return fmt.Errorf("token expired and no refresh token available - please log in again")
	}

	if err := RefreshAccessToken(&t.auth); err != nil {
		return fmt.Errorf("failed to refresh token: %w", err)
	}

	// Persist refreshed token to disk
	if t.authPath != "" {
		if err := SaveAuth(t.authPath, t.auth); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not save refreshed token: %v\n", err)
		}
	}

	return nil
}

// SetDebug enables or disables debug output.
func (t *Tidal) SetDebug(enabled bool) {
	t.debug = enabled
}

// GetTrack fetches track metadata by ID.
func (t *Tidal) GetTrack(id string) (provider.Track, error) {
	metadata, err := t.getTrackMetadata(id)
	if err != nil {
		return provider.Track{}, err
	}

	return t.metadataToTrack(metadata), nil
}

// GetAlbum fetches album metadata and tracks by ID.
func (t *Tidal) GetAlbum(id string) (provider.Album, error) {
	// Get album metadata
	albumMeta, err := t.getAlbumMetadata(id)
	if err != nil {
		return provider.Album{}, fmt.Errorf("failed to get album metadata: %w", err)
	}

	// Get album tracks
	tracks, err := t.getAlbumTracks(id)
	if err != nil {
		return provider.Album{}, fmt.Errorf("failed to get album tracks: %w", err)
	}

	// Convert to generic tracks, enriching with album-level fields
	genericTracks := make([]provider.Track, len(tracks))
	for i, tr := range tracks {
		track := t.metadataToTrack(tr)
		// Enrich with album-level data not available on TrackMetadata
		track.Date = albumMeta.ReleaseDate
		track.TotalDiscs = albumMeta.NumberOfVolumes
		track.AlbumArtist = albumMeta.Artist.Name
		if track.CoverURL == "" {
			track.CoverURL = CoverURL(albumMeta.Cover, 1280)
		}
		genericTracks[i] = track
	}

	return provider.Album{
		ID:          strconv.Itoa(albumMeta.ID),
		Title:       albumMeta.Title,
		Artist:      albumMeta.Artist.Name,
		Tracks:      genericTracks,
		Date:        albumMeta.ReleaseDate,
		TotalTracks: albumMeta.NumberOfTracks,
		TotalDiscs:  albumMeta.NumberOfVolumes,
		CoverURL:    CoverURL(albumMeta.Cover, 1280),
	}, nil
}

// GetStreamURL returns the direct stream URL for a track.
func (t *Tidal) GetStreamURL(trackID string) (string, error) {
	// Get track metadata to find quality
	metadata, err := t.getTrackMetadata(trackID)
	if err != nil {
		return "", fmt.Errorf("failed to get track metadata: %w", err)
	}

	quality := metadata.AudioQuality
	if t.config.Quality != "" {
		quality = t.config.Quality
	}
	if quality == "" {
		quality = "LOSSLESS"
	}

	// Get playback info
	playbackInfo, err := t.getPlaybackInfo(trackID, quality)
	if err != nil {
		return "", fmt.Errorf("failed to get playback info: %w", err)
	}

	// Decode manifest
	manifest, err := t.decodeManifest(playbackInfo)
	if err != nil {
		return "", fmt.Errorf("failed to decode manifest: %w", err)
	}

	// Check encryption
	if manifest.EncryptionType != "NONE" {
		return "", fmt.Errorf("stream is encrypted (type: %s)", manifest.EncryptionType)
	}

	if len(manifest.URLs) == 0 {
		return "", fmt.Errorf("no stream URLs found in manifest")
	}

	return manifest.URLs[0], nil
}

// DownloadTrack downloads a track by its Tidal URL.
func (t *Tidal) DownloadTrack(tidalURL string) error {
	trackID := ExtractTrackID(tidalURL)
	if trackID == "" {
		return fmt.Errorf("could not extract track ID from URL: %s", tidalURL)
	}

	if !t.IsAuthenticated() {
		return fmt.Errorf("not authenticated - please check auth.json")
	}

	// Fetch track metadata with loading spinner
	var track provider.Track
	var streamURL, fileName string
	var streamURLs []string
	var trackInfo ui.TrackInfo

	err := ui.RunLoading("Fetching track metadata...", func() error {
		// Get track metadata
		metadata, err := t.getTrackMetadata(trackID)
		if err != nil {
			return fmt.Errorf("failed to get track metadata: %w", err)
		}

		track = t.metadataToTrack(metadata)

		// Fetch album-level metadata for fields not on TrackMetadata
		// (release date, total discs, album artist)
		albumMeta, albumErr := t.getAlbumMetadata(strconv.Itoa(metadata.Album.ID))
		if albumErr == nil {
			track.Date = albumMeta.ReleaseDate
			track.TotalDiscs = albumMeta.NumberOfVolumes
			track.AlbumArtist = albumMeta.Artist.Name
			if track.CoverURL == "" {
				track.CoverURL = CoverURL(albumMeta.Cover, 1280)
			}
		}

		// Get stream URL
		streamInfo, err := t.getStreamInfo(trackID, metadata.AudioQuality)
		if err != nil {
			return fmt.Errorf("failed to get stream URL: %w", err)
		}

		streamURL = streamInfo.URL
		streamURLs = streamInfo.URLs
		track.StreamURL = streamInfo.URL
		track.MimeType = streamInfo.MimeType

		// Determine extension
		ext := provider.MimeToExtension(streamInfo.MimeType)

		// Build filename
		fileName = provider.FormatFilename(t.config.Format, track, ext, t.config.OutputDir)

		// Create track info for UI
		trackInfo = ui.TrackInfo{
			ID:               track.ID,
			Title:            track.Title,
			Artist:           track.Artist,
			Album:            track.Album,
			RequestedQuality: streamInfo.RequestedQuality,
			ActualQuality:    streamInfo.ActualQuality,
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Create download task
	tasks := []ui.DownloadTask{{
		Index:      0,
		TrackInfo:  trackInfo,
		StreamURL:  streamURL,
		StreamURLs: streamURLs,
		FilePath:   fileName,
		Metadata:   track,
	}}

	// Create and run download manager with TUI
	dm := ui.NewDownloadManager(
		track.Title,
		track.Artist,
		t.Name(),
		false, // not an album
		[]ui.TrackInfo{trackInfo},
		t.config,
	)

	return dm.Run(tasks)
}

// DownloadAlbum downloads all tracks from an album by its Tidal URL.
func (t *Tidal) DownloadAlbum(tidalURL string) error {
	albumID := ExtractAlbumID(tidalURL)
	if albumID == "" {
		return fmt.Errorf("could not extract album ID from URL: %s", tidalURL)
	}

	if !t.IsAuthenticated() {
		return fmt.Errorf("not authenticated - please check auth.json")
	}

	// Fetch album metadata with loading spinner
	var album provider.Album
	var trackInfos []ui.TrackInfo
	var tasks []ui.DownloadTask

	err := ui.RunLoading("Fetching album metadata...", func() error {
		var err error
		album, err = t.GetAlbum(albumID)
		if err != nil {
			return fmt.Errorf("failed to get album: %w", err)
		}

		// Build track info list and download tasks
		trackInfos = make([]ui.TrackInfo, len(album.Tracks))
		tasks = make([]ui.DownloadTask, 0, len(album.Tracks))

		for i, track := range album.Tracks {
			// Get stream URL for each track
			streamInfo, err := t.getStreamInfo(track.ID, track.Quality)
			if err != nil {
				trackInfos[i] = ui.TrackInfo{
					ID:     track.ID,
					Title:  track.Title,
					Artist: track.Artist,
					Album:  album.Title,
					Status: ui.StatusError,
					Error:  err.Error(),
				}
				continue
			}

			trackInfos[i] = ui.TrackInfo{
				ID:               track.ID,
				Title:            track.Title,
				Artist:           track.Artist,
				Album:            album.Title,
				RequestedQuality: streamInfo.RequestedQuality,
				ActualQuality:    streamInfo.ActualQuality,
			}

			ext := provider.MimeToExtension(streamInfo.MimeType)
			track.Album = album.Title
			fileName := provider.FormatFilename(t.config.Format, track, ext, t.config.OutputDir)

			tasks = append(tasks, ui.DownloadTask{
				Index:      i,
				TrackInfo:  trackInfos[i],
				StreamURL:  streamInfo.URL,
				StreamURLs: streamInfo.URLs,
				FilePath:   fileName,
				Metadata:   track,
			})
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Create and run download manager with TUI
	dm := ui.NewDownloadManager(
		album.Title,
		album.Artist,
		t.Name(),
		true, // is an album
		trackInfos,
		t.config,
	)

	if err := dm.Run(tasks); err != nil {
		return err
	}

	// Check for partial failures (e.g. 429 rate limits on individual tracks)
	if dm.HasFailedTracks() {
		return &provider.PartialFailureError{
			URL:         tidalURL,
			FailedCount: dm.FailedTrackCount(),
			TotalCount:  len(album.Tracks),
		}
	}

	return nil
}

// DownloadArtist fetches all albums for an artist, presents a release-type
// filter and then an album selection UI, and downloads the chosen albums.
func (t *Tidal) DownloadArtist(tidalURL string) error {
	artistID := ExtractArtistID(tidalURL)
	if artistID == "" {
		return fmt.Errorf("could not extract artist ID from URL: %s", tidalURL)
	}

	if !t.IsAuthenticated() {
		return fmt.Errorf("not authenticated - please check auth.json")
	}

	// Step 1: Fetch artist metadata and release type counts
	var artistMeta ArtistMetadata
	var typeCounts map[string]int

	err := ui.RunLoading("Fetching artist info...", func() error {
		var err error
		artistMeta, err = t.getArtistMetadata(artistID)
		if err != nil {
			return fmt.Errorf("failed to get artist metadata: %w", err)
		}

		typeCounts, err = t.getArtistReleaseCounts(artistID)
		if err != nil {
			return fmt.Errorf("failed to get release counts: %w", err)
		}

		return nil
	})

	if err != nil {
		return err
	}

	totalReleases := 0
	for _, c := range typeCounts {
		totalReleases += c
	}
	if totalReleases == 0 {
		fmt.Println("No releases found for this artist.")
		return nil
	}

	// Step 2: Let user pick which release types to include
	selectedFilters, cancelled, err := ui.RunFilter(artistMeta.Name, typeCounts)
	if err != nil {
		return fmt.Errorf("release type selection failed: %w", err)
	}
	if cancelled || len(selectedFilters) == 0 {
		fmt.Println("No release types selected.")
		return nil
	}

	// Step 3: Fetch albums for the selected release types
	var albums []AlbumMetadata

	err = ui.RunLoading("Fetching discography...", func() error {
		var err error
		albums, err = t.getArtistAlbums(artistID, selectedFilters)
		if err != nil {
			return fmt.Errorf("failed to get artist albums: %w", err)
		}
		return nil
	})

	if err != nil {
		return err
	}

	if len(albums) == 0 {
		fmt.Println("No albums found for the selected release types.")
		return nil
	}

	// Build album choices for the selector UI
	choices := make([]ui.AlbumChoice, len(albums))
	for i, album := range albums {
		choices[i] = ui.AlbumChoice{
			ID:           strconv.Itoa(album.ID),
			Title:        album.Title,
			Artist:       album.Artist.Name,
			ReleaseType:  formatReleaseType(album.Type),
			ReleaseDate:  album.ReleaseDate,
			TrackCount:   album.NumberOfTracks,
			AudioQuality: album.AudioQuality,
		}
	}

	// Step 4: Run the interactive album selector
	selected, cancelled, err := ui.RunSelector(artistMeta.Name, choices)
	if err != nil {
		return fmt.Errorf("album selection failed: %w", err)
	}

	if cancelled || len(selected) == 0 {
		fmt.Println("No albums selected.")
		return nil
	}

	// Download each selected album sequentially
	fmt.Printf("\nDownloading %d album(s) from %s\n\n", len(selected), artistMeta.Name)

	// Build album URLs for the selected albums
	albumURLs := make([]string, len(selected))
	for i, album := range selected {
		albumURLs[i] = fmt.Sprintf("https://tidal.com/album/%s", album.ID)
	}

	// Notify caller of selected albums (for session creation)
	if t.OnAlbumsSelected != nil {
		t.OnAlbumsSelected(albumURLs)
	}

	var failedURLs []string
	var failedDetails []string
	var otherErrors []string

	for i, album := range selected {
		fmt.Printf("Album %d/%d: %s\n", i+1, len(selected), album.Title)

		albumURL := albumURLs[i]
		if err := t.DownloadAlbum(albumURL); err != nil {
			if pfe, ok := err.(*provider.PartialFailureError); ok {
				failedURLs = append(failedURLs, pfe.URL)
				failedDetails = append(failedDetails, fmt.Sprintf("  %s: %s", album.Title, pfe.Error()))
			} else {
				otherErrors = append(otherErrors, fmt.Sprintf("  %s: %v", album.Title, err))
			}
		}

		// Notify caller that this album is done (for session update)
		if t.OnAlbumComplete != nil {
			t.OnAlbumComplete(albumURL)
		}

		fmt.Println()
	}

	// Return partial failure error if any albums had track failures
	if len(failedURLs) > 0 {
		return &provider.FailedAlbumsError{
			FailedURLs: failedURLs,
			Details:    failedDetails,
		}
	}

	if len(otherErrors) > 0 {
		return fmt.Errorf("some albums failed to download:\n%s", strings.Join(otherErrors, "\n"))
	}

	return nil
}

// --- Internal Methods ---

func (t *Tidal) performAPICall(path string, query url.Values) ([]byte, int, error) {
	// Auto-refresh token if expired
	if err := t.ensureValidToken(); err != nil {
		return nil, 0, err
	}

	fullURL := APIBaseURL + path + "?" + query.Encode()

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+t.auth.Token)
	req.Header.Set("User-Agent", "Go-Tidal-Downloader/1.0")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("error executing request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("error reading response body: %w", err)
	}

	if t.debug {
		fmt.Printf("\n--- DEBUG: API Response for %s ---\n%s\n--- END DEBUG ---\n\n", path, string(body))
	}

	return body, resp.StatusCode, nil
}

func (t *Tidal) getTrackMetadata(trackID string) (TrackMetadata, error) {
	apiPath := fmt.Sprintf("tracks/%s", trackID)

	query := url.Values{}
	query.Set("countryCode", t.auth.CountryCode)

	body, status, err := t.performAPICall(apiPath, query)
	if err != nil {
		return TrackMetadata{}, err
	}

	if status != http.StatusOK {
		return TrackMetadata{}, fmt.Errorf("metadata request failed with status %d: %s", status, string(body))
	}

	var metadata TrackMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		return TrackMetadata{}, fmt.Errorf("failed to parse track metadata: %w", err)
	}

	return metadata, nil
}

func (t *Tidal) getAlbumMetadata(albumID string) (AlbumMetadata, error) {
	apiPath := fmt.Sprintf("albums/%s", albumID)

	query := url.Values{}
	query.Set("countryCode", t.auth.CountryCode)

	body, status, err := t.performAPICall(apiPath, query)
	if err != nil {
		return AlbumMetadata{}, err
	}

	if status != http.StatusOK {
		return AlbumMetadata{}, fmt.Errorf("album metadata request failed with status %d: %s", status, string(body))
	}

	var metadata AlbumMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		return AlbumMetadata{}, fmt.Errorf("failed to parse album metadata: %w", err)
	}

	return metadata, nil
}

func (t *Tidal) getAlbumTracks(albumID string) ([]TrackMetadata, error) {
	apiPath := fmt.Sprintf("albums/%s/tracks", albumID)

	query := url.Values{}
	query.Set("countryCode", t.auth.CountryCode)
	query.Set("limit", "100")

	body, status, err := t.performAPICall(apiPath, query)
	if err != nil {
		return nil, err
	}

	if status != http.StatusOK {
		return nil, fmt.Errorf("album tracks request failed with status %d: %s", status, string(body))
	}

	var response AlbumTracksResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse album tracks: %w", err)
	}

	return response.Items, nil
}

func (t *Tidal) getArtistMetadata(artistID string) (ArtistMetadata, error) {
	apiPath := fmt.Sprintf("artists/%s", artistID)

	query := url.Values{}
	query.Set("countryCode", t.auth.CountryCode)

	body, status, err := t.performAPICall(apiPath, query)
	if err != nil {
		return ArtistMetadata{}, err
	}

	if status != http.StatusOK {
		return ArtistMetadata{}, fmt.Errorf("artist metadata request failed with status %d: %s", status, string(body))
	}

	var metadata ArtistMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		return ArtistMetadata{}, fmt.Errorf("failed to parse artist metadata: %w", err)
	}

	return metadata, nil
}

// getArtistReleaseCounts returns the total number of releases per display
// category for the given artist. The keys match the UI section labels:
// "Albums", "EP & Singles", "Compilations".
func (t *Tidal) getArtistReleaseCounts(artistID string) (map[string]int, error) {
	type filterLabel struct {
		filter string
		label  string
	}
	mapping := []filterLabel{
		{"ALBUMS", "Albums"},
		{"EPSANDSINGLES", "EP & Singles"},
		{"COMPILATIONS", "Compilations"},
	}

	counts := make(map[string]int)
	for _, fl := range mapping {
		apiPath := fmt.Sprintf("artists/%s/albums", artistID)

		query := url.Values{}
		query.Set("countryCode", t.auth.CountryCode)
		query.Set("filter", fl.filter)
		query.Set("limit", "1") // We only need totalNumberOfItems
		query.Set("offset", "0")

		body, status, err := t.performAPICall(apiPath, query)
		if err != nil {
			return nil, err
		}

		if status != http.StatusOK {
			return nil, fmt.Errorf("artist albums count request failed with status %d: %s", status, string(body))
		}

		var response ArtistAlbumsResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("failed to parse artist albums count: %w", err)
		}

		counts[fl.label] = response.TotalNumberOfItems
	}

	return counts, nil
}

func (t *Tidal) getArtistAlbums(artistID string, filters []string) ([]AlbumMetadata, error) {
	var allAlbums []AlbumMetadata
	seen := make(map[int]bool)

	for _, filter := range filters {
		offset := 0
		limit := 50

		for {
			apiPath := fmt.Sprintf("artists/%s/albums", artistID)

			query := url.Values{}
			query.Set("countryCode", t.auth.CountryCode)
			query.Set("filter", filter)
			query.Set("limit", strconv.Itoa(limit))
			query.Set("offset", strconv.Itoa(offset))

			body, status, err := t.performAPICall(apiPath, query)
			if err != nil {
				return nil, err
			}

			if status != http.StatusOK {
				return nil, fmt.Errorf("artist albums request failed with status %d: %s", status, string(body))
			}

			var response ArtistAlbumsResponse
			if err := json.Unmarshal(body, &response); err != nil {
				return nil, fmt.Errorf("failed to parse artist albums: %w", err)
			}

			for _, album := range response.Items {
				if !seen[album.ID] {
					seen[album.ID] = true
					allAlbums = append(allAlbums, album)
				}
			}

			// Check if we've fetched all albums for this filter
			if offset+limit >= response.TotalNumberOfItems {
				break
			}
			offset += limit
		}
	}

	return allAlbums, nil
}

func (t *Tidal) getPlaybackInfo(trackID, quality string) (PlaybackInfo, error) {
	apiPath := fmt.Sprintf("tracks/%s/playbackinfopostpaywall", trackID)

	query := url.Values{}
	query.Set("audioquality", quality)
	query.Set("playbackmode", "STREAM")
	query.Set("assetpresentation", "FULL")
	query.Set("countryCode", t.auth.CountryCode)

	body, status, err := t.performAPICall(apiPath, query)
	if err != nil {
		return PlaybackInfo{}, err
	}

	if status != http.StatusOK {
		return PlaybackInfo{}, fmt.Errorf("playback info request failed with status %d: %s", status, string(body))
	}

	var playbackInfo PlaybackInfo
	if err := json.Unmarshal(body, &playbackInfo); err != nil {
		return PlaybackInfo{}, fmt.Errorf("failed to parse playback info: %w", err)
	}

	return playbackInfo, nil
}

func (t *Tidal) decodeManifest(playbackInfo PlaybackInfo) (StreamManifest, error) {
	decoded, err := base64.StdEncoding.DecodeString(playbackInfo.Manifest)
	if err != nil {
		return StreamManifest{}, fmt.Errorf("failed to base64 decode manifest: %w", err)
	}

	switch playbackInfo.ManifestMimeType {
	case "application/dash+xml":
		return parseDASHManifest(decoded)
	default:
		// Default: BTS JSON manifest (application/vnd.tidal.bts)
		var manifest StreamManifest
		if err := json.Unmarshal(decoded, &manifest); err != nil {
			return StreamManifest{}, fmt.Errorf("failed to parse manifest JSON: %w", err)
		}
		return manifest, nil
	}
}

// parseDASHManifest extracts stream URLs and metadata from a DASH MPD manifest.
// Tidal's HI_RES_LOSSLESS MPD uses SegmentTemplate with initialization/media
// attributes and a SegmentTimeline to define numbered segments. This function
// builds the full ordered list of segment URLs: init segment first, then all
// numbered media segments derived from the timeline.
func parseDASHManifest(data []byte) (StreamManifest, error) {
	var mpd MPD
	if err := xml.Unmarshal(data, &mpd); err != nil {
		return StreamManifest{}, fmt.Errorf("failed to parse DASH manifest XML: %w", err)
	}

	var urls []string
	var mimeType, codecs string

	for _, period := range mpd.Periods {
		for _, adaptSet := range period.AdaptationSets {
			for _, rep := range adaptSet.Representations {
				// Capture codec and mime info from the first representation
				if codecs == "" && rep.Codecs != "" {
					codecs = rep.Codecs
				}
				if mimeType == "" && adaptSet.MimeType != "" {
					mimeType = adaptSet.MimeType
				}

				// Try SegmentTemplate first (Tidal HI_RES_LOSSLESS uses this)
				if rep.SegmentTemplate != nil {
					tmpl := rep.SegmentTemplate

					// Add initialization segment URL
					if tmpl.Initialization != "" {
						urls = append(urls, tmpl.Initialization)
					}

					// Compute segment count from SegmentTimeline
					if tmpl.Media != "" && tmpl.Timeline != nil {
						startNum := tmpl.StartNumber
						segNum := startNum
						for _, s := range tmpl.Timeline.Segments {
							// Each <S> element represents 1 + r segments
							count := 1 + s.R
							for i := 0; i < count; i++ {
								segURL := strings.Replace(tmpl.Media, "$Number$", strconv.Itoa(segNum), 1)
								urls = append(urls, segURL)
								segNum++
							}
						}
					}
				}

				// Fallback: collect BaseURLs (direct download URLs)
				if len(urls) == 0 {
					for _, baseURL := range rep.BaseURLs {
						if baseURL != "" {
							urls = append(urls, baseURL)
						}
					}
				}

				// Use only the first representation that yields URLs
				if len(urls) > 0 {
					break
				}
			}
			if len(urls) > 0 {
				break
			}
		}
		if len(urls) > 0 {
			break
		}
	}

	if len(urls) == 0 {
		return StreamManifest{}, fmt.Errorf("no stream URLs found in DASH manifest")
	}

	return StreamManifest{
		MimeType:       mimeType,
		Codecs:         codecs,
		EncryptionType: "NONE",
		URLs:           urls,
	}, nil
}

// StreamInfo holds stream URL, MIME type, and quality information.
// For DASH streams, URLs contains all segment URLs (init + media segments)
// that must be downloaded and concatenated in order.
// URL is always set to the first URL for backwards compatibility.
type StreamInfo struct {
	URL              string
	URLs             []string
	MimeType         string
	RequestedQuality string
	ActualQuality    string
}

func (t *Tidal) getStreamInfo(trackID, quality string) (StreamInfo, error) {
	// Config quality override takes precedence
	requestedQuality := quality
	if t.config.Quality != "" {
		requestedQuality = t.config.Quality
	}
	if requestedQuality == "" {
		requestedQuality = "LOSSLESS"
	}

	playbackInfo, err := t.getPlaybackInfo(trackID, requestedQuality)
	if err != nil {
		return StreamInfo{}, err
	}

	manifest, err := t.decodeManifest(playbackInfo)
	if err != nil {
		return StreamInfo{}, err
	}

	if manifest.EncryptionType != "NONE" {
		return StreamInfo{}, fmt.Errorf("stream is encrypted (type: %s)", manifest.EncryptionType)
	}

	if len(manifest.URLs) == 0 {
		return StreamInfo{}, fmt.Errorf("no stream URLs found in manifest")
	}

	return StreamInfo{
		URL:              manifest.URLs[0],
		URLs:             manifest.URLs,
		MimeType:         manifest.MimeType,
		RequestedQuality: requestedQuality,
		ActualQuality:    playbackInfo.AudioQuality,
	}, nil
}

func (t *Tidal) metadataToTrack(m TrackMetadata) provider.Track {
	return provider.Track{
		ID:          strconv.Itoa(m.ID),
		Title:       m.Title,
		Artist:      m.Artist.Name,
		Album:       m.Album.Title,
		AlbumArtist: m.Artist.Name,
		Quality:     m.AudioQuality,
		Duration:    m.Duration,
		TrackNumber: m.TrackNumber,
		TotalTracks: m.Album.NumberOfTracks,
		DiscNumber:  m.VolumeNumber,
		Copyright:   m.Copyright,
		ISRC:        m.ISRC,
		CoverURL:    CoverURL(m.Album.Cover, 1280),
	}
}

// CoverURL builds a Tidal CDN URL for album cover art.
// The cover ID is a UUID; dashes are replaced with slashes in the URL path.
// Common sizes: 80, 160, 320, 640, 1280.
func CoverURL(coverID string, size int) string {
	if coverID == "" {
		return ""
	}
	// Replace dashes with slashes: e7877e55-3f00-4f1c-b6e2-7bba3e2a7f1b -> e7877e55/3f00/4f1c/b6e2/7bba3e2a7f1b
	path := strings.ReplaceAll(coverID, "-", "/")
	return fmt.Sprintf("https://resources.tidal.com/images/%s/%dx%d.jpg", path, size, size)
}

// --- URL Parsing Helpers ---

// ExtractTrackID parses a Tidal URL to extract the track ID.
func ExtractTrackID(tidalURL string) string {
	match := trackPattern.FindStringSubmatch(tidalURL)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

// ExtractAlbumID parses a Tidal URL to extract the album ID.
func ExtractAlbumID(tidalURL string) string {
	match := albumPattern.FindStringSubmatch(tidalURL)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

// ExtractArtistID parses a Tidal URL to extract the artist ID.
func ExtractArtistID(tidalURL string) string {
	match := artistPattern.FindStringSubmatch(tidalURL)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

// formatReleaseType converts Tidal album type constants to human-readable format.
func formatReleaseType(albumType string) string {
	switch strings.ToUpper(albumType) {
	case "ALBUM":
		return "Album"
	case "SINGLE":
		return "Single"
	case "EP":
		return "EP"
	case "COMPILATION":
		return "Compilation"
	default:
		if albumType != "" {
			// Title-case unknown types
			return strings.ToUpper(albumType[:1]) + strings.ToLower(albumType[1:])
		}
		return ""
	}
}
