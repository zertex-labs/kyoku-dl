package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	mbAPIBase   = "https://musicbrainz.org/ws/2"
	mbUserAgent = "kyoku-dl/1.0.0 (https://github.com/zertex-labs/kyoku-dl)"
	mbRateLimit = 1100 * time.Millisecond // slightly over 1s to stay safe
)

// MBClient is a rate-limited MusicBrainz API client for enriching track metadata.
type MBClient struct {
	httpClient *http.Client
	mu         sync.Mutex
	lastReq    time.Time
}

// NewMBClient creates a new MusicBrainz client with built-in rate limiting.
func NewMBClient() *MBClient {
	return &MBClient{
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// Enrich looks up a track by ISRC on MusicBrainz and populates Genre and MBID fields.
// This is a two-step process:
//  1. ISRC lookup → list of recording MBIDs + artist credits
//  2. Recording lookup by MBID → genres, releases
//
// It only enriches fields that are currently empty on the track; it never overwrites
// existing data from the primary provider.
func (c *MBClient) Enrich(track *Track) error {
	if track.ISRC == "" {
		return fmt.Errorf("no ISRC available for lookup")
	}

	// Step 1: ISRC → recordings (returns artist-credits but NOT genres/releases with full detail)
	isrcRecordings, err := c.lookupISRC(track.ISRC)
	if err != nil {
		return err
	}

	if len(isrcRecordings) == 0 {
		return fmt.Errorf("no recordings found for ISRC %s", track.ISRC)
	}

	// Pick the best recording from the ISRC results
	best := c.bestRecording(isrcRecordings, track)

	// Set recording MBID (available from step 1)
	if track.MusicBrainzRecordingID == "" && best.ID != "" {
		track.MusicBrainzRecordingID = best.ID
	}

	// Set artist MBID (available from step 1)
	if track.MusicBrainzArtistID == "" && len(best.ArtistCredit) > 0 {
		track.MusicBrainzArtistID = best.ArtistCredit[0].Artist.ID
	}

	// Step 2: Recording MBID → full details (genres + releases)
	// Only needed if we still want genre or release MBID
	if (track.Genre == "" || track.MusicBrainzReleaseID == "") && best.ID != "" {
		fullRec, err := c.lookupRecording(best.ID)
		if err == nil {
			// Set genre
			if track.Genre == "" && len(fullRec.Genres) > 0 {
				track.Genre = c.topGenre(fullRec.Genres)
			}

			// Set release MBID — fuzzy match album title
			if track.MusicBrainzReleaseID == "" && len(fullRec.Releases) > 0 {
				if rel := c.bestRelease(fullRec.Releases, track.Album); rel != nil {
					track.MusicBrainzReleaseID = rel.ID
				}
			}
		}
		// If step 2 fails, we still have the data from step 1 — that's fine
	}

	return nil
}

// --- MusicBrainz API types ---

type mbISRCResponse struct {
	Recordings []mbRecording `json:"recordings"`
}

type mbRecording struct {
	ID           string           `json:"id"`
	Title        string           `json:"title"`
	ArtistCredit []mbArtistCredit `json:"artist-credit"`
	Releases     []mbRelease      `json:"releases"`
	Genres       []mbGenre        `json:"genres"`
}

type mbArtistCredit struct {
	Artist mbArtist `json:"artist"`
}

type mbArtist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type mbRelease struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type mbGenre struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// --- Internal methods ---

// rateLimit ensures at least mbRateLimit has passed since the last request.
func (c *MBClient) rateLimit() {
	c.mu.Lock()
	defer c.mu.Unlock()

	since := time.Since(c.lastReq)
	if since < mbRateLimit {
		time.Sleep(mbRateLimit - since)
	}
	c.lastReq = time.Now()
}

// doRequest performs a rate-limited GET request to the MusicBrainz API.
func (c *MBClient) doRequest(url string) ([]byte, error) {
	c.rateLimit()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create MB request: %w", err)
	}
	req.Header.Set("User-Agent", mbUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("MB API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("MB API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read MB response: %w", err)
	}

	return body, nil
}

// lookupISRC queries the MusicBrainz ISRC endpoint.
// The ISRC endpoint only supports artist-credits as an inc parameter (not genres/releases).
func (c *MBClient) lookupISRC(isrc string) ([]mbRecording, error) {
	url := fmt.Sprintf("%s/isrc/%s?inc=artist-credits&fmt=json", mbAPIBase, isrc)

	body, err := c.doRequest(url)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return nil, nil // 404 — ISRC not in MusicBrainz
	}

	var result mbISRCResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse ISRC response: %w", err)
	}

	return result.Recordings, nil
}

// lookupRecording fetches a recording by MBID with genres and releases included.
func (c *MBClient) lookupRecording(mbid string) (*mbRecording, error) {
	url := fmt.Sprintf("%s/recording/%s?inc=genres+releases+artist-credits&fmt=json", mbAPIBase, mbid)

	body, err := c.doRequest(url)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return nil, fmt.Errorf("recording %s not found", mbid)
	}

	var rec mbRecording
	if err := json.Unmarshal(body, &rec); err != nil {
		return nil, fmt.Errorf("failed to parse recording response: %w", err)
	}

	return &rec, nil
}

// bestRecording picks the recording whose title best matches the track.
// If none match well, returns the first one.
func (c *MBClient) bestRecording(recordings []mbRecording, track *Track) mbRecording {
	// Prefer exact title match (case-insensitive)
	for _, rec := range recordings {
		if strings.EqualFold(rec.Title, track.Title) {
			return rec
		}
	}
	// Fall back to first recording
	return recordings[0]
}

// bestRelease picks the release whose title best matches the album name.
// Returns nil if no reasonable match is found.
func (c *MBClient) bestRelease(releases []mbRelease, albumTitle string) *mbRelease {
	if albumTitle == "" {
		return nil
	}

	lower := strings.ToLower(albumTitle)

	// Exact match (case-insensitive)
	for i, rel := range releases {
		if strings.EqualFold(rel.Title, albumTitle) {
			return &releases[i]
		}
	}

	// Substring match — the release title contains the album title or vice versa
	for i, rel := range releases {
		relLower := strings.ToLower(rel.Title)
		if strings.Contains(relLower, lower) || strings.Contains(lower, relLower) {
			return &releases[i]
		}
	}

	// No confident match — skip release MBID rather than pick the wrong one
	return nil
}

// topGenre returns the genre name with the highest vote count.
func (c *MBClient) topGenre(genres []mbGenre) string {
	best := genres[0]
	for _, g := range genres[1:] {
		if g.Count > best.Count {
			best = g
		}
	}
	return best.Name
}
