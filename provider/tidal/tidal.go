package tidal

import (
	"encoding/base64"
	"encoding/json"
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
	trackPattern = regexp.MustCompile(`tidal\.com.*?/track/(\d+)`)
	albumPattern = regexp.MustCompile(`tidal\.com.*?/album/(\d+)`)
)

// Tidal implements the provider.Provider interface for Tidal.
type Tidal struct {
	auth       Auth
	authPath   string
	config     provider.Config
	httpClient *http.Client
	debug      bool
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
	return trackPattern.MatchString(rawURL) || albumPattern.MatchString(rawURL)
}

// URLType returns the type of content the URL points to ("track", "album", or "").
func (t *Tidal) URLType(rawURL string) string {
	if trackPattern.MatchString(rawURL) {
		return "track"
	}
	if albumPattern.MatchString(rawURL) {
		return "album"
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
		var mimeType string
		streamURL, mimeType, err = t.getStreamURLWithMime(trackID, metadata.AudioQuality)
		if err != nil {
			return fmt.Errorf("failed to get stream URL: %w", err)
		}

		track.StreamURL = streamURL
		track.MimeType = mimeType

		// Determine extension
		ext := provider.MimeToExtension(mimeType)

		// Build filename
		fileName = provider.FormatFilename(t.config.Format, track, ext, t.config.OutputDir)

		// Create track info for UI
		trackInfo = ui.TrackInfo{
			ID:     track.ID,
			Title:  track.Title,
			Artist: track.Artist,
			Album:  track.Album,
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Create download task
	tasks := []ui.DownloadTask{{
		Index:     0,
		TrackInfo: trackInfo,
		StreamURL: streamURL,
		FilePath:  fileName,
		Metadata:  track,
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
			trackInfos[i] = ui.TrackInfo{
				ID:     track.ID,
				Title:  track.Title,
				Artist: track.Artist,
				Album:  album.Title,
			}

			// Get stream URL for each track
			streamURL, mimeType, err := t.getStreamURLWithMime(track.ID, track.Quality)
			if err != nil {
				trackInfos[i].Status = ui.StatusError
				trackInfos[i].Error = err.Error()
				continue
			}

			ext := provider.MimeToExtension(mimeType)
			track.Album = album.Title
			fileName := provider.FormatFilename(t.config.Format, track, ext, t.config.OutputDir)

			tasks = append(tasks, ui.DownloadTask{
				Index:     i,
				TrackInfo: trackInfos[i],
				StreamURL: streamURL,
				FilePath:  fileName,
				Metadata:  track,
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

	return dm.Run(tasks)
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

	var manifest StreamManifest
	if err := json.Unmarshal(decoded, &manifest); err != nil {
		return StreamManifest{}, fmt.Errorf("failed to parse manifest JSON: %w", err)
	}

	return manifest, nil
}

func (t *Tidal) getStreamURLWithMime(trackID, quality string) (string, string, error) {
	// Config quality override takes precedence
	if t.config.Quality != "" {
		quality = t.config.Quality
	}
	if quality == "" {
		quality = "LOSSLESS"
	}

	playbackInfo, err := t.getPlaybackInfo(trackID, quality)
	if err != nil {
		return "", "", err
	}

	manifest, err := t.decodeManifest(playbackInfo)
	if err != nil {
		return "", "", err
	}

	if manifest.EncryptionType != "NONE" {
		return "", "", fmt.Errorf("stream is encrypted (type: %s)", manifest.EncryptionType)
	}

	if len(manifest.URLs) == 0 {
		return "", "", fmt.Errorf("no stream URLs found in manifest")
	}

	return manifest.URLs[0], manifest.MimeType, nil
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
