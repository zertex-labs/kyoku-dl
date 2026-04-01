package deezer

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zertex-labs/kyoku-dl/provider"
)

// Compile-time interface check.
var _ provider.Provider = (*Deezer)(nil)

const (
	PublicAPIBase = "https://api.deezer.com"
	GWLightBase   = "https://www.deezer.com/ajax/gw-light.php"
	MediaAPIBase  = "https://media.deezer.com/v1/get_url"

	DefaultTimeout = 10 * time.Second
)

// URL patterns for matching Deezer links (support optional language prefix like /en/)
var (
	trackPattern  = regexp.MustCompile(`deezer\.com(?:/[a-z]{2})?/track/(\d+)`)
	albumPattern  = regexp.MustCompile(`deezer\.com(?:/[a-z]{2})?/album/(\d+)`)
	artistPattern = regexp.MustCompile(`deezer\.com(?:/[a-z]{2})?/artist/(\d+)`)
)

// Deezer implements the provider.Provider interface for Deezer.
type Deezer struct {
	auth       Auth
	authPath   string
	config     provider.Config
	httpClient *http.Client
	debug      bool

	// OnAlbumsSelected is called after the user selects albums in an artist
	// download, before any downloads begin. The callback receives the album
	// URLs that will be downloaded.
	OnAlbumsSelected func(albumURLs []string)

	// OnAlbumComplete is called after each album finishes downloading during
	// an artist download.
	OnAlbumComplete func(albumURL string)
}

// New creates a new Deezer provider with the given auth and config.
func New(auth Auth, config provider.Config) *Deezer {
	return &Deezer{
		auth:   auth,
		config: config,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		debug: config.Debug,
	}
}

// NewWithPath creates a new Deezer provider with auth, config, and auth file path.
func NewWithPath(auth Auth, authPath string, config provider.Config) *Deezer {
	d := New(auth, config)
	d.authPath = authPath
	return d
}

// NewFromFile loads auth from a JSON file and creates a Deezer provider.
func NewFromFile(authPath string, config provider.Config) (*Deezer, error) {
	auth, err := LoadAuth(authPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load auth: %w", err)
	}

	return NewWithPath(auth, authPath, config), nil
}

// --- Provider Interface Implementation ---

// Name returns the service name.
func (d *Deezer) Name() string {
	return "deezer"
}

// MatchesURL returns true if the URL belongs to Deezer.
func (d *Deezer) MatchesURL(rawURL string) bool {
	return trackPattern.MatchString(rawURL) || albumPattern.MatchString(rawURL) || artistPattern.MatchString(rawURL)
}

// URLType returns the type of content the URL points to ("track", "album", "artist", or "").
func (d *Deezer) URLType(rawURL string) string {
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
func (d *Deezer) IsAuthenticated() bool {
	return d.auth.ARL != "" && d.auth.APIToken != ""
}

// SetDebug enables or disables debug output.
func (d *Deezer) SetDebug(enabled bool) {
	d.debug = enabled
}

// SetAuth updates the provider's authentication credentials and auth file path.
func (d *Deezer) SetAuth(auth Auth, authPath string) {
	d.auth = auth
	d.authPath = authPath
}

// GetTrack fetches track metadata by ID.
func (d *Deezer) GetTrack(id string) (provider.Track, error) {
	body, err := d.publicAPICall("/track/" + id)
	if err != nil {
		return provider.Track{}, fmt.Errorf("failed to get track: %w", err)
	}

	var resp TrackResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return provider.Track{}, fmt.Errorf("failed to parse track response: %w", err)
	}

	return d.metadataToTrack(resp), nil
}

// GetAlbum fetches album metadata and tracks by ID.
func (d *Deezer) GetAlbum(id string) (provider.Album, error) {
	body, err := d.publicAPICall("/album/" + id)
	if err != nil {
		return provider.Album{}, fmt.Errorf("failed to get album: %w", err)
	}

	var resp AlbumResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return provider.Album{}, fmt.Errorf("failed to parse album response: %w", err)
	}

	// Convert tracks
	tracks := make([]provider.Track, len(resp.Tracks.Data))
	for i, t := range resp.Tracks.Data {
		track := d.metadataToTrack(t)
		// Enrich with album-level data
		track.Date = resp.ReleaseDate
		track.AlbumArtist = resp.Artist.Name
		track.TotalTracks = resp.NbTracks
		if track.Album == "" {
			track.Album = resp.Title
		}
		if track.CoverURL == "" {
			track.CoverURL = CoverURL(resp.MD5Image, 1000)
		}
		tracks[i] = track
	}

	return provider.Album{
		ID:          strconv.Itoa(resp.ID),
		Title:       resp.Title,
		Artist:      resp.Artist.Name,
		Tracks:      tracks,
		Date:        resp.ReleaseDate,
		TotalTracks: resp.NbTracks,
		CoverURL:    CoverURL(resp.MD5Image, 1000),
	}, nil
}

// GetStreamURL returns the direct stream URL for a track. (stub — implemented in Plan 02)
func (d *Deezer) GetStreamURL(trackID string) (string, error) {
	return "", fmt.Errorf("deezer: GetStreamURL not yet implemented")
}

// DownloadTrack downloads a track by its service URL. (stub — implemented in Plan 02)
func (d *Deezer) DownloadTrack(url string) error {
	return fmt.Errorf("deezer: DownloadTrack not yet implemented")
}

// DownloadAlbum downloads all tracks from an album by its service URL. (stub — implemented in Plan 02)
func (d *Deezer) DownloadAlbum(url string) error {
	return fmt.Errorf("deezer: DownloadAlbum not yet implemented")
}

// DownloadArtist fetches all albums for an artist and downloads selected ones. (stub — implemented in Plan 02)
func (d *Deezer) DownloadArtist(url string) error {
	return fmt.Errorf("deezer: DownloadArtist not yet implemented")
}

// --- Internal Methods ---

// publicAPICall performs a GET request to the Deezer public API.
func (d *Deezer) publicAPICall(path string) ([]byte, error) {
	fullURL := PublicAPIBase + path

	resp, err := d.httpClient.Get(fullURL)
	if err != nil {
		return nil, fmt.Errorf("public API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if d.debug {
		fmt.Printf("\n--- DEBUG: Deezer Public API %s ---\n%s\n--- END DEBUG ---\n\n", path, string(body))
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("public API returned status %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// gwAPICall performs a POST request to the Deezer private gw-light API.
func (d *Deezer) gwAPICall(method string, params interface{}) (json.RawMessage, error) {
	// Build query string
	q := url.Values{}
	q.Set("method", method)
	q.Set("api_version", "1.0")
	q.Set("api_token", d.auth.APIToken)
	q.Set("input", "3")
	q.Set("output", "3")

	fullURL := GWLightBase + "?" + q.Encode()

	// Encode params as JSON body
	var bodyReader io.Reader
	if params != nil {
		paramBytes, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		bodyReader = strings.NewReader(string(paramBytes))
	} else {
		bodyReader = strings.NewReader("{}")
	}

	req, err := http.NewRequest("POST", fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Set cookies for authentication
	if d.auth.ARL != "" {
		req.AddCookie(&http.Cookie{Name: "arl", Value: d.auth.ARL})
	}
	if d.auth.SessionID != "" {
		req.AddCookie(&http.Cookie{Name: "sid", Value: d.auth.SessionID})
	}

	// Use a client with cookie jar for gw-light calls
	jar, _ := cookiejar.New(nil)
	gwClient := &http.Client{
		Timeout: DefaultTimeout,
		Jar:     jar,
	}

	resp, err := gwClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GW API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read GW API response: %w", err)
	}

	if d.debug {
		fmt.Printf("\n--- DEBUG: Deezer GW API %s ---\n%s\n--- END DEBUG ---\n\n", method, string(body))
	}

	var gwResp GWResponse
	if err := json.Unmarshal(body, &gwResp); err != nil {
		return nil, fmt.Errorf("failed to parse GW API response: %w", err)
	}

	// Check for errors in the GW response
	if len(gwResp.Error) > 0 {
		// Check if any error is non-empty
		for _, e := range gwResp.Error {
			if e != nil {
				errStr := fmt.Sprintf("%v", e)
				if errStr != "" && errStr != "map[]" {
					return nil, fmt.Errorf("GW API error: %v", e)
				}
			}
		}
	}

	return gwResp.Results, nil
}

// getSongData fetches private song data using the GW API.
func (d *Deezer) getSongData(trackID string) (GWSongData, error) {
	params := map[string]string{"sng_id": trackID}

	results, err := d.gwAPICall("song.getData", params)
	if err != nil {
		return GWSongData{}, fmt.Errorf("failed to get song data: %w", err)
	}

	var songData GWSongData
	if err := json.Unmarshal(results, &songData); err != nil {
		return GWSongData{}, fmt.Errorf("failed to parse song data: %w", err)
	}

	return songData, nil
}

// getArtistAlbums fetches all albums for an artist from the public API with pagination.
func (d *Deezer) getArtistAlbums(artistID string) ([]AlbumResponse, error) {
	var allAlbums []AlbumResponse
	offset := 0
	limit := 50

	for {
		path := fmt.Sprintf("/artist/%s/albums?limit=%d&index=%d", artistID, limit, offset)
		body, err := d.publicAPICall(path)
		if err != nil {
			return nil, fmt.Errorf("failed to get artist albums: %w", err)
		}

		var resp ArtistAlbumsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse artist albums: %w", err)
		}

		allAlbums = append(allAlbums, resp.Data...)

		// Check if there are more pages
		if resp.Next == "" || len(resp.Data) < limit {
			break
		}

		offset += limit
	}

	return allAlbums, nil
}

// metadataToTrack converts a public API TrackResponse to a provider.Track.
func (d *Deezer) metadataToTrack(resp TrackResponse) provider.Track {
	return provider.Track{
		ID:          strconv.Itoa(resp.ID),
		Title:       resp.Title,
		Artist:      resp.Artist.Name,
		Album:       resp.Album.Title,
		Duration:    resp.Duration,
		TrackNumber: resp.TrackPosition,
		DiscNumber:  resp.DiskNumber,
		ISRC:        resp.ISRC,
		CoverURL:    CoverURL(resp.Album.MD5Image, 1000),
	}
}

// CoverURL builds a Deezer CDN URL for cover art.
// Common sizes: 56, 250, 500, 1000.
func CoverURL(md5 string, size int) string {
	if md5 == "" {
		return ""
	}
	return fmt.Sprintf("https://cdn-images.dzcdn.net/images/cover/%s/%dx%d-000000-80-0-0.jpg", md5, size, size)
}

// --- URL Parsing Helpers ---

// ExtractTrackID parses a Deezer URL to extract the track ID.
func ExtractTrackID(deezerURL string) string {
	match := trackPattern.FindStringSubmatch(deezerURL)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

// ExtractAlbumID parses a Deezer URL to extract the album ID.
func ExtractAlbumID(deezerURL string) string {
	match := albumPattern.FindStringSubmatch(deezerURL)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

// ExtractArtistID parses a Deezer URL to extract the artist ID.
func ExtractArtistID(deezerURL string) string {
	match := artistPattern.FindStringSubmatch(deezerURL)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

// --- Quality Helpers ---

// deezerQuality maps configuration quality strings to Deezer format names.
func deezerQuality(configQuality string) string {
	switch configQuality {
	case "FLAC", "LOSSLESS", "HI_RES_LOSSLESS":
		return "FLAC"
	case "HIGH", "MP3_320":
		return "MP3_320"
	default:
		return "MP3_128"
	}
}

// formatReleaseType converts Deezer record type constants to human-readable format.
func formatReleaseType(recordType string) string {
	switch strings.ToLower(recordType) {
	case "album":
		return "Album"
	case "single":
		return "Single"
	case "ep":
		return "EP"
	case "compile":
		return "Compilation"
	default:
		return recordType
	}
}
