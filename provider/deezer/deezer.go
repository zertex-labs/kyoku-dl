package deezer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zertex-labs/kyoku-dl/provider"
	"github.com/zertex-labs/kyoku-dl/ui"
)

// Compile-time interface check.
var _ provider.Provider = (*Deezer)(nil)

const (
	PublicAPIBase = "https://api.deezer.com"
	GWLightBase   = "https://www.deezer.com/ajax/gw-light.php"
	MediaAPIBase  = "https://media.deezer.com/v1/get_url"

	DefaultTimeout = 30 * time.Second

	// browserUA is a modern Chrome User-Agent sent on all Deezer API requests
	// so the server treats us like a browser session (required for valid responses).
	browserUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
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
	jar, _ := cookiejar.New(nil)
	d := &Deezer{
		auth:   auth,
		config: config,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
			Jar:     jar,
		},
		debug: config.Debug,
	}
	// If auth already has ARL, seed the cookie jar so all requests carry cookies.
	if auth.ARL != "" {
		d.initSessionClient(auth.ARL, auth.SessionID)
	}
	return d
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

// SetAuth updates the provider's authentication credentials and auth file path,
// and reinitializes the persistent HTTP client with the new auth cookies.
func (d *Deezer) SetAuth(auth Auth, authPath string) {
	d.auth = auth
	d.authPath = authPath
	if auth.ARL != "" {
		d.initSessionClient(auth.ARL, auth.SessionID)
	}
}

// initSessionClient creates a new persistent http.Client with a cookie jar
// seeded with the ARL (and optionally sid) cookies on https://www.deezer.com.
// All subsequent API calls through d.httpClient will carry these cookies
// automatically, matching streamrip's requests.Session behaviour.
func (d *Deezer) initSessionClient(arl, sessionID string) {
	jar, _ := cookiejar.New(nil)

	deezerURL, _ := url.Parse("https://www.deezer.com")
	cookies := []*http.Cookie{
		{Name: "arl", Value: arl, Domain: ".deezer.com", Path: "/"},
	}
	if sessionID != "" {
		cookies = append(cookies, &http.Cookie{Name: "sid", Value: sessionID, Domain: ".deezer.com", Path: "/"})
	}
	jar.SetCookies(deezerURL, cookies)

	d.httpClient = &http.Client{
		Timeout: DefaultTimeout,
		Jar:     jar,
	}
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

	// Extract primary genre from album
	albumGenre := ""
	if len(resp.Genres.Data) > 0 {
		albumGenre = resp.Genres.Data[0].Name
	}

	for i, t := range resp.Tracks.Data {
		track := d.metadataToTrack(t)
		// Enrich with album-level data
		track.Date = resp.ReleaseDate
		track.AlbumArtist = resp.Artist.Name
		track.TotalTracks = resp.NbTracks
		track.Genre = albumGenre
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

// StreamInfo holds Deezer stream URL and quality information.
type StreamInfo struct {
	URL              string
	MimeType         string
	IsEncrypted      bool
	SongData         GWSongData
	RequestedQuality string
	ActualQuality    string
	Copyright        string
}

// GeoRestrictedError indicates a track is geo-restricted (media API error code 2002).
type GeoRestrictedError struct {
	Code    int
	Message string
}

func (e *GeoRestrictedError) Error() string {
	return fmt.Sprintf("geo-restricted (code %d): %s", e.Code, e.Message)
}

// GetStreamURL returns the direct stream URL for a track.
func (d *Deezer) GetStreamURL(trackID string) (string, error) {
	quality := deezerQuality(d.config.Quality)
	info, err := d.getStreamURL(trackID, quality, GWSongData{})
	if err != nil {
		return "", err
	}
	return info.URL, nil
}

// DownloadTrack downloads a track by its Deezer URL.
func (d *Deezer) DownloadTrack(deezerURL string) error {
	trackID := ExtractTrackID(deezerURL)
	if trackID == "" {
		return fmt.Errorf("could not extract track ID from URL: %s", deezerURL)
	}

	if !d.IsAuthenticated() {
		return fmt.Errorf("not authenticated - please check auth.json")
	}

	var track provider.Track
	var streamInfo StreamInfo
	var fileName string
	var trackInfo ui.TrackInfo

	err := ui.RunLoading("Fetching track metadata...", func() error {
		// Get public track metadata
		trackResp, err := d.getTrackResponse(trackID)
		if err != nil {
			return fmt.Errorf("failed to get track metadata: %w", err)
		}
		track = d.metadataToTrack(trackResp)

		// Fetch album-level metadata for fields not on track
		albumBody, albumErr := d.publicAPICall("/album/" + strconv.Itoa(trackResp.Album.ID))
		if albumErr == nil {
			var albumResp AlbumResponse
			if json.Unmarshal(albumBody, &albumResp) == nil {
				track.Date = albumResp.ReleaseDate
				track.AlbumArtist = albumResp.Artist.Name
				track.TotalTracks = albumResp.NbTracks
				if len(albumResp.Genres.Data) > 0 {
					track.Genre = albumResp.Genres.Data[0].Name
				}
				if track.CoverURL == "" {
					track.CoverURL = CoverURL(albumResp.MD5Image, 1000)
				}
			}
		}

		// Get stream URL
		quality := deezerQuality(d.config.Quality)
		streamInfo, err = d.getStreamURL(trackID, quality, GWSongData{})
		if err != nil {
			return fmt.Errorf("failed to get stream URL: %w", err)
		}

		track.MimeType = streamInfo.MimeType
		track.StreamURL = streamInfo.URL
		track.Copyright = streamInfo.Copyright

		// Determine extension and filename
		ext := provider.MimeToExtension(streamInfo.MimeType)
		fileName = provider.FormatFilename(d.config.Format, track, ext, d.config.OutputDir)

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

	// Create download task and run with TUI (custom Deezer download loop)
	tasks := []deezerDownloadTask{{
		Index:     0,
		TrackInfo: trackInfo,
		StreamURL: streamInfo.URL,
		FilePath:  fileName,
		Metadata:  track,
		TrackID:   trackID,
	}}

	return d.runDeezerDownload(track.Title, track.Artist, false, []ui.TrackInfo{trackInfo}, tasks)
}

// DownloadAlbum downloads all tracks from an album by its Deezer URL.
func (d *Deezer) DownloadAlbum(deezerURL string) error {
	albumID := ExtractAlbumID(deezerURL)
	if albumID == "" {
		return fmt.Errorf("could not extract album ID from URL: %s", deezerURL)
	}

	if !d.IsAuthenticated() {
		return fmt.Errorf("not authenticated - please check auth.json")
	}

	var album provider.Album
	var trackInfos []ui.TrackInfo
	var tasks []deezerDownloadTask

	err := ui.RunLoading("Fetching album metadata...", func() error {
		var err error
		album, err = d.GetAlbum(albumID)
		if err != nil {
			return fmt.Errorf("failed to get album: %w", err)
		}

		quality := deezerQuality(d.config.Quality)
		trackInfos = make([]ui.TrackInfo, len(album.Tracks))

		for i, track := range album.Tracks {
			streamInfo, err := d.getStreamURL(track.ID, quality, GWSongData{})
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
			track.Copyright = streamInfo.Copyright
			filePath := provider.FormatFilename(d.config.Format, track, ext, d.config.OutputDir)

			tasks = append(tasks, deezerDownloadTask{
				Index:     i,
				TrackInfo: trackInfos[i],
				StreamURL: streamInfo.URL,
				FilePath:  filePath,
				Metadata:  track,
				TrackID:   track.ID,
			})
		}

		return nil
	})

	if err != nil {
		return err
	}

	if err := d.runDeezerDownload(album.Title, album.Artist, true, trackInfos, tasks); err != nil {
		return err
	}

	// Check for partial failures
	failedCount := 0
	for _, ti := range trackInfos {
		if ti.Status == ui.StatusError {
			failedCount++
		}
	}
	if failedCount > 0 {
		return &provider.PartialFailureError{
			URL:         deezerURL,
			FailedCount: failedCount,
			TotalCount:  len(album.Tracks),
		}
	}

	return nil
}

// DownloadArtist fetches all albums for an artist, presents a release-type
// filter and album selection UI, and downloads the chosen albums.
func (d *Deezer) DownloadArtist(deezerURL string) error {
	artistID := ExtractArtistID(deezerURL)
	if artistID == "" {
		return fmt.Errorf("could not extract artist ID from URL: %s", deezerURL)
	}

	if !d.IsAuthenticated() {
		return fmt.Errorf("not authenticated - please check auth.json")
	}

	// Step 1: Fetch artist metadata and all albums
	var artistResp ArtistResponse
	var allAlbums []AlbumResponse

	err := ui.RunLoading("Fetching artist info...", func() error {
		body, err := d.publicAPICall("/artist/" + artistID)
		if err != nil {
			return fmt.Errorf("failed to get artist metadata: %w", err)
		}
		if err := json.Unmarshal(body, &artistResp); err != nil {
			return fmt.Errorf("failed to parse artist response: %w", err)
		}

		allAlbums, err = d.getArtistAlbums(artistID)
		if err != nil {
			return fmt.Errorf("failed to get artist albums: %w", err)
		}
		return nil
	})

	if err != nil {
		return err
	}

	if len(allAlbums) == 0 {
		fmt.Println("No releases found for this artist.")
		return nil
	}

	// Count releases by type using the record_type field
	typeCounts := make(map[string]int)
	for _, album := range allAlbums {
		switch strings.ToLower(album.RecordType) {
		case "album":
			typeCounts["Albums"]++
		case "single", "ep":
			typeCounts["EP & Singles"]++
		case "compile":
			typeCounts["Compilations"]++
		}
	}

	totalReleases := 0
	for _, c := range typeCounts {
		totalReleases += c
	}
	if totalReleases == 0 {
		fmt.Println("No releases found for this artist.")
		return nil
	}

	// Step 2: Let user pick release types
	selectedFilters, cancelled, err := ui.RunFilter(artistResp.Name, typeCounts)
	if err != nil {
		return fmt.Errorf("release type selection failed: %w", err)
	}
	if cancelled || len(selectedFilters) == 0 {
		fmt.Println("No release types selected.")
		return nil
	}

	// Build a set of selected release types (map filter labels back to record_type values)
	selectedTypeSet := make(map[string]bool)
	for _, filter := range selectedFilters {
		switch filter {
		case "ALBUMS":
			selectedTypeSet["album"] = true
		case "EPSANDSINGLES":
			selectedTypeSet["single"] = true
			selectedTypeSet["ep"] = true
		case "COMPILATIONS":
			selectedTypeSet["compile"] = true
		}
	}

	// Filter albums by selected types
	var filteredAlbums []AlbumResponse
	for _, album := range allAlbums {
		if selectedTypeSet[strings.ToLower(album.RecordType)] {
			filteredAlbums = append(filteredAlbums, album)
		}
	}

	if len(filteredAlbums) == 0 {
		fmt.Println("No albums found for the selected release types.")
		return nil
	}

	// Build album choices for the selector UI
	choices := make([]ui.AlbumChoice, len(filteredAlbums))
	for i, album := range filteredAlbums {
		choices[i] = ui.AlbumChoice{
			ID:          strconv.Itoa(album.ID),
			Title:       album.Title,
			Artist:      album.Artist.Name,
			ReleaseType: formatReleaseType(album.RecordType),
			ReleaseDate: album.ReleaseDate,
			TrackCount:  album.NbTracks,
		}
	}

	// Step 3: Run the album selector
	selected, cancelled, err := ui.RunSelector(artistResp.Name, choices)
	if err != nil {
		return fmt.Errorf("album selection failed: %w", err)
	}

	if cancelled || len(selected) == 0 {
		fmt.Println("No albums selected.")
		return nil
	}

	// Download each selected album sequentially
	fmt.Printf("\nDownloading %d album(s) from %s\n\n", len(selected), artistResp.Name)

	albumURLs := make([]string, len(selected))
	for i, album := range selected {
		albumURLs[i] = fmt.Sprintf("https://www.deezer.com/album/%s", album.ID)
	}

	// Notify caller of selected albums (for session creation)
	if d.OnAlbumsSelected != nil {
		d.OnAlbumsSelected(albumURLs)
	}

	var failedURLs []string
	var failedDetails []string
	var otherErrors []string

	for i, album := range selected {
		fmt.Printf("Album %d/%d: %s\n", i+1, len(selected), album.Title)

		albumURL := albumURLs[i]
		if err := d.DownloadAlbum(albumURL); err != nil {
			if pfe, ok := err.(*provider.PartialFailureError); ok {
				failedURLs = append(failedURLs, pfe.URL)
				failedDetails = append(failedDetails, fmt.Sprintf("  %s: %s", album.Title, pfe.Error()))
			} else {
				otherErrors = append(otherErrors, fmt.Sprintf("  %s: %v", album.Title, err))
			}
		}

		if d.OnAlbumComplete != nil {
			d.OnAlbumComplete(albumURL)
		}

		fmt.Println()
	}

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

// --- Stream URL Resolution ---

// getStreamURL resolves a stream URL for a track, trying modern API first with legacy CDN fallback.
// If songData is zero-valued, it will be fetched via getSongData.
func (d *Deezer) getStreamURL(trackID, quality string, songData GWSongData) (StreamInfo, error) {
	// Fetch song data if not provided
	if songData.SNG_ID == "" {
		var err error
		songData, err = d.getSongData(trackID)
		if err != nil {
			return StreamInfo{}, fmt.Errorf("failed to get song data: %w", err)
		}
	}

	// Quality fallback: check file sizes
	actualQuality := quality
	if quality == "FLAC" && songData.FILESIZE_FLAC == "0" {
		actualQuality = "MP3_320"
	}
	if actualQuality == "MP3_320" && songData.FILESIZE_MP3_320 == "0" {
		actualQuality = "MP3_128"
	}

	// License capability pre-check: verify account can stream the requested quality.
	// This runs AFTER file-size fallback ("does this track have FLAC?") but BEFORE
	// the media API call ("give me the stream URL"), so users with free/HQ accounts
	// get a clear warning instead of a cryptic media API error.
	if actualQuality == "FLAC" && !d.auth.CanStreamLossless {
		if d.auth.CanStreamHQ {
			fmt.Fprintf(os.Stderr, "Warning: account cannot stream lossless, falling back to MP3_320\n")
			actualQuality = "MP3_320"
		} else {
			fmt.Fprintf(os.Stderr, "Warning: account cannot stream lossless or HQ, falling back to MP3_128\n")
			actualQuality = "MP3_128"
		}
	}
	if actualQuality == "MP3_320" && !d.auth.CanStreamHQ {
		fmt.Fprintf(os.Stderr, "Warning: account cannot stream HQ, falling back to MP3_128\n")
		actualQuality = "MP3_128"
	}

	mimeType := "audio/mpeg"
	if actualQuality == "FLAC" {
		mimeType = "audio/flac"
	}

	// Try modern media API first
	if songData.TRACK_TOKEN != "" && d.auth.LicenseToken != "" {
		mediaURL, err := d.resolveMediaURL(songData.TRACK_TOKEN, actualQuality)
		if err == nil && mediaURL != "" {
			return StreamInfo{
				URL:              mediaURL,
				MimeType:         mimeType,
				IsEncrypted:      true, // All Deezer streams are Blowfish encrypted
				SongData:         songData,
				RequestedQuality: quality,
				ActualQuality:    actualQuality,
				Copyright:        songData.COPYRIGHT,
			}, nil
		}
		// Check for geo-restriction — try FALLBACK track if available
		var geoErr *GeoRestrictedError
		if errors.As(err, &geoErr) && songData.FALLBACK.SNG_ID != "" && songData.FALLBACK.SNG_ID != songData.SNG_ID {
			if d.debug {
				fmt.Fprintf(os.Stderr, "Track %s geo-restricted, trying FALLBACK %s\n", songData.SNG_ID, songData.FALLBACK.SNG_ID)
			}
			fallbackData, fbErr := d.getSongData(songData.FALLBACK.SNG_ID)
			if fbErr == nil {
				return d.getStreamURL(songData.FALLBACK.SNG_ID, quality, fallbackData)
			}
		}
		// Fall through to legacy CDN on failure
	}

	// Fallback: legacy CDN URL
	if songData.MD5_ORIGIN == "" {
		return StreamInfo{}, fmt.Errorf("no MD5_ORIGIN available for track %s — cannot build CDN URL", trackID)
	}

	cdnURL, err := BuildLegacyCDNURL(songData.MD5_ORIGIN, songData.SNG_ID, songData.MEDIA_VERSION, actualQuality)
	if err != nil {
		return StreamInfo{}, fmt.Errorf("failed to build legacy CDN URL: %w", err)
	}

	return StreamInfo{
		URL:              cdnURL,
		MimeType:         mimeType,
		IsEncrypted:      true,
		SongData:         songData,
		RequestedQuality: quality,
		ActualQuality:    actualQuality,
		Copyright:        songData.COPYRIGHT,
	}, nil
}

// resolveMediaURL calls the modern media.deezer.com API to get a stream URL.
// The persistent d.httpClient carries ARL and session cookies via its cookie jar,
// which the media API requires for valid responses.
func (d *Deezer) resolveMediaURL(trackToken, quality string) (string, error) {
	reqBody := MediaRequest{
		LicenseToken: d.auth.LicenseToken,
		Media: []MediaTrack{{
			Type: "FULL",
			Formats: []MediaFormat{{
				Format: quality,
				Cipher: "BF_CBC_STRIPE",
			}},
		}},
		TrackTokens: []string{trackToken},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal media request: %w", err)
	}

	req, err := http.NewRequest("POST", MediaAPIBase, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create media request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", browserUA)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("media API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read media response: %w", err)
	}

	if d.debug {
		fmt.Printf("\n--- DEBUG: Deezer Media API (status %d) ---\n%s\n--- END DEBUG ---\n\n", resp.StatusCode, string(body))
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("media API returned status %d: %s", resp.StatusCode, string(body))
	}

	var mediaResp MediaResponse
	if err := json.Unmarshal(body, &mediaResp); err != nil {
		return "", fmt.Errorf("failed to parse media response: %w", err)
	}

	// Check for errors in the response data
	if len(mediaResp.Data) > 0 {
		// Check for geo-restriction errors (code 2002) before checking media
		if len(mediaResp.Data[0].Errors) > 0 {
			for _, e := range mediaResp.Data[0].Errors {
				if e.Code == 2002 {
					return "", &GeoRestrictedError{Code: e.Code, Message: e.Message}
				}
			}
		}
		// The API returns errors inside data[].errors[] when track tokens are
		// expired or the license doesn't cover the requested quality
		if len(mediaResp.Data[0].Media) == 0 {
			return "", fmt.Errorf("media API returned no media entries (track token may be expired or quality unavailable)")
		}
		if len(mediaResp.Data[0].Media[0].Sources) == 0 {
			return "", fmt.Errorf("media API returned no sources for format %s", quality)
		}
	} else {
		return "", fmt.Errorf("media API returned empty data array")
	}

	return mediaResp.Data[0].Media[0].Sources[0].URL, nil
}

// getTrackResponse fetches the raw public API track response.
func (d *Deezer) getTrackResponse(trackID string) (TrackResponse, error) {
	body, err := d.publicAPICall("/track/" + trackID)
	if err != nil {
		return TrackResponse{}, fmt.Errorf("failed to get track: %w", err)
	}

	var resp TrackResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return TrackResponse{}, fmt.Errorf("failed to parse track response: %w", err)
	}

	return resp, nil
}

// --- Deezer Download with Decryption ---

// deezerDownloadTask extends the standard download task with Deezer-specific fields.
type deezerDownloadTask struct {
	Index     int
	TrackInfo ui.TrackInfo
	StreamURL string
	FilePath  string
	Metadata  provider.Track
	TrackID   string
}

// runDeezerDownload runs the TUI download loop with Blowfish decryption.
// This replaces the standard dm.Run() because Deezer streams must be decrypted
// before tagging.
func (d *Deezer) runDeezerDownload(title, artist string, isAlbum bool, trackInfos []ui.TrackInfo, tasks []deezerDownloadTask) error {
	model := ui.NewDownloadModel(title, artist, d.Name(), isAlbum, trackInfos)
	program := tea.NewProgram(model)

	// Start downloads in a goroutine
	go d.downloadAllDeezer(program, model, tasks)

	// Run the TUI
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	if model.IsQuitting() {
		return fmt.Errorf("download cancelled by user")
	}

	return nil
}

// downloadAllDeezer handles the download loop for Deezer tracks:
// download encrypted → decrypt → tag → probe, sending TUI messages.
func (d *Deezer) downloadAllDeezer(program *tea.Program, model *ui.DownloadModel, tasks []deezerDownloadTask) {
	// Create MusicBrainz client if enrichment is enabled
	var mbClient *provider.MBClient
	if d.config.MusicBrainz {
		mbClient = provider.NewMBClient()
	}

	type mbResult struct {
		err error
	}

	// Start first MB lookup in parallel
	var mbCh chan mbResult
	if mbClient != nil && len(tasks) > 0 && tasks[0].Metadata.ISRC != "" {
		mbCh = make(chan mbResult, 1)
		go func(t *provider.Track) {
			mbCh <- mbResult{err: mbClient.Enrich(t)}
		}(&tasks[0].Metadata)
	}

	for i, task := range tasks {
		if model.IsQuitting() {
			break
		}

		// Skip if file already exists with non-zero size
		if info, err := os.Stat(task.FilePath); err == nil && info.Size() > 0 {
			program.Send(ui.TrackSkippedMsg{Index: task.Index})
			if mbCh != nil {
				<-mbCh
				mbCh = nil
			}
			continue
		}

		program.Send(ui.TrackStartMsg{Index: task.Index})

		// Start MB lookup for the NEXT task in parallel
		var nextMBCh chan mbResult
		if mbClient != nil && i+1 < len(tasks) && tasks[i+1].Metadata.ISRC != "" {
			nextMBCh = make(chan mbResult, 1)
			go func(t *provider.Track) {
				nextMBCh <- mbResult{err: mbClient.Enrich(t)}
			}(&tasks[i+1].Metadata)
		}

		// Download encrypted stream → decrypt → write final file
		err := d.downloadAndDecrypt(task, program, model)
		if err != nil {
			program.Send(ui.TrackErrorMsg{Index: task.Index, Error: err.Error()})
			if mbCh != nil {
				<-mbCh
			}
			mbCh = nextMBCh
			continue
		}

		// Wait for current track's MB enrichment
		if mbCh != nil {
			<-mbCh
		}

		// Tag the decrypted file
		if tagErr := provider.TagAudioFile(task.FilePath, tasks[i].Metadata); tagErr != nil {
			_ = tagErr // Tagging failure is non-fatal
		}

		// Probe audio info
		var audioInfo *provider.AudioInfo
		if info, probeErr := provider.ProbeAudioFile(task.FilePath); probeErr == nil {
			info.Provider = d.Name()
			audioInfo = info
		}

		program.Send(ui.TrackCompleteMsg{Index: task.Index, AudioInfo: audioInfo})

		mbCh = nextMBCh
	}

	program.Send(ui.AllCompleteMsg{})
}

// downloadAndDecrypt downloads an encrypted Deezer stream to a temp file,
// then decrypts it to the final output file.
func (d *Deezer) downloadAndDecrypt(task deezerDownloadTask, program *tea.Program, model *ui.DownloadModel) error {
	// Ensure directory exists
	if err := provider.EnsureDirectory(task.FilePath); err != nil {
		return err
	}

	encPath := task.FilePath + ".enc"

	// Download encrypted stream to temp file (retry once on failure)
	err := d.downloadEncrypted(task, encPath, program, model)
	if err != nil {
		os.Remove(encPath)
		if d.debug {
			fmt.Fprintf(os.Stderr, "Download failed, retrying: %v\n", err)
		}
		// Retry once
		err = d.downloadEncrypted(task, encPath, program, model)
		if err != nil {
			os.Remove(encPath)
			return fmt.Errorf("download failed after retry: %w", err)
		}
	}

	// Decrypt to final file
	encFile, err := os.Open(encPath)
	if err != nil {
		os.Remove(encPath)
		return fmt.Errorf("failed to open encrypted file: %w", err)
	}

	outFile, err := os.Create(task.FilePath)
	if err != nil {
		encFile.Close()
		os.Remove(encPath)
		return fmt.Errorf("failed to create output file: %w", err)
	}

	decryptErr := DecryptStream(encFile, outFile, task.TrackID)
	encFile.Close()
	outFile.Close()
	os.Remove(encPath) // Clean up temp file

	if decryptErr != nil {
		os.Remove(task.FilePath)
		return fmt.Errorf("decryption failed: %w", decryptErr)
	}

	return nil
}

// downloadEncrypted downloads the raw (encrypted) stream from a URL to a file,
// sending progress messages to the TUI.
func (d *Deezer) downloadEncrypted(task deezerDownloadTask, destPath string, program *tea.Program, model *ui.DownloadModel) error {
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	downloadOK := false
	defer func() {
		out.Close()
		if !downloadOK {
			os.Remove(destPath)
		}
	}()

	client := provider.NewHTTPClient()
	req, err := provider.NewDownloadRequest(task.StreamURL)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %s", resp.Status)
	}

	totalSize := resp.ContentLength

	// Small response detection: responses under 20KB are likely error JSON, not audio
	if totalSize > 0 && totalSize < 20*1024 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("response too small (%d bytes), likely error: %s", totalSize, string(body))
	}

	var written int64
	startTime := time.Now()
	lastUpdate := time.Now()
	buffer := make([]byte, provider.DownloadBufferSize)

	for {
		if model.IsQuitting() {
			return fmt.Errorf("cancelled")
		}

		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			_, writeErr := out.Write(buffer[:n])
			if writeErr != nil {
				return fmt.Errorf("failed to write: %w", writeErr)
			}
			atomic.AddInt64(&written, int64(n))

			now := time.Now()
			if now.Sub(lastUpdate) >= 100*time.Millisecond {
				lastUpdate = now
				elapsed := now.Sub(startTime).Seconds()
				if elapsed > 0 {
					speed := float64(written) / elapsed
					var progress float64
					if totalSize > 0 {
						progress = float64(written) / float64(totalSize) * 100
					}
					program.Send(ui.TrackProgressMsg{
						Index:      task.Index,
						Progress:   progress,
						Downloaded: written,
						Size:       totalSize,
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

// --- Internal Methods ---

// publicAPICall performs a GET request to the Deezer public API.
// If the response contains a rate limit error (codes 4 or 700), it sleeps 5s and retries once.
func (d *Deezer) publicAPICall(path string) ([]byte, error) {
	return d.publicAPICallInner(path, false)
}

// publicAPICallInner is the implementation of publicAPICall with a retry flag.
func (d *Deezer) publicAPICallInner(path string, retried bool) ([]byte, error) {
	fullURL := PublicAPIBase + path

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create public API request: %w", err)
	}
	req.Header.Set("User-Agent", browserUA)

	resp, err := d.httpClient.Do(req)
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

	// Check for rate limit errors in the response body
	if !retried {
		var apiErr PublicAPIError
		if json.Unmarshal(body, &apiErr) == nil && (apiErr.Error.Code == 4 || apiErr.Error.Code == 700) {
			if d.debug {
				fmt.Fprintf(os.Stderr, "DEBUG: Rate limited (code %d), retrying in 5s...\n", apiErr.Error.Code)
			}
			time.Sleep(5 * time.Second)
			return d.publicAPICallInner(path, true)
		}
	}

	return body, nil
}

// gwAPICall performs a POST request to the Deezer private gw-light API.
// If the server returns a CSRF token error ("VALID_TOKEN_REQUIRED" or "Invalid CSRF token"),
// the token is refreshed via refreshCSRFToken and the call is retried once.
func (d *Deezer) gwAPICall(method string, params interface{}) (json.RawMessage, error) {
	return d.gwAPICallInner(method, params, false)
}

// gwAPICallInner is the implementation of gwAPICall with a retry flag to prevent infinite loops.
func (d *Deezer) gwAPICallInner(method string, params interface{}, retried bool) (json.RawMessage, error) {
	// Build query string
	q := url.Values{}
	q.Set("method", method)
	q.Set("api_version", "1.0")
	q.Set("api_token", d.auth.APIToken)
	q.Set("input", "3")

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
	req.Header.Set("User-Agent", browserUA)

	resp, err := d.httpClient.Do(req)
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
		// Check for CSRF token expiry — auto-refresh and retry once
		if !retried && isCSRFTokenError(gwResp.Error) {
			if d.debug {
				fmt.Fprintf(os.Stderr, "DEBUG: CSRF token expired, refreshing...\n")
			}
			if refreshErr := d.refreshCSRFToken(); refreshErr != nil {
				return nil, fmt.Errorf("CSRF token expired and refresh failed: %w", refreshErr)
			}
			return d.gwAPICallInner(method, params, true)
		}

		// Build error message from the map
		var errParts []string
		for k, v := range gwResp.Error {
			errParts = append(errParts, fmt.Sprintf("%s: %s", k, v))
		}
		return nil, fmt.Errorf("GW API error: %s", strings.Join(errParts, "; "))
	}

	return gwResp.Results, nil
}

// isCSRFTokenError checks if a GW API error map indicates a CSRF token expiry.
// Deezer returns {"VALID_TOKEN_REQUIRED": "..."} or error values containing
// "Invalid CSRF token" or "invalid api token".
func isCSRFTokenError(errMap map[string]string) bool {
	if _, ok := errMap["VALID_TOKEN_REQUIRED"]; ok {
		return true
	}
	for _, v := range errMap {
		lower := strings.ToLower(v)
		if strings.Contains(lower, "invalid csrf token") || strings.Contains(lower, "invalid api token") {
			return true
		}
	}
	return false
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
