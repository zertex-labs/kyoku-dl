package provider

import (
	"fmt"
	"path/filepath"
	"strings"
)

// PartialFailureError indicates that a download completed but some tracks failed.
// The URL field contains the original album/track URL for retry purposes.
type PartialFailureError struct {
	URL         string
	FailedCount int
	TotalCount  int
}

func (e *PartialFailureError) Error() string {
	return fmt.Sprintf("%d/%d tracks failed for %s", e.FailedCount, e.TotalCount, e.URL)
}

// FailedAlbumsError indicates that one or more albums had track-level failures.
// FailedURLs contains the album URLs that should be retried.
type FailedAlbumsError struct {
	FailedURLs []string
	Details    []string // human-readable error per album
}

func (e *FailedAlbumsError) Error() string {
	return fmt.Sprintf("%d album(s) had failed tracks:\n%s", len(e.FailedURLs), strings.Join(e.Details, "\n"))
}

// Track represents a generic track from any music service.
type Track struct {
	ID        string
	Title     string
	Artist    string
	Album     string
	Quality   string
	Duration  int // seconds
	StreamURL string
	MimeType  string

	// Metadata for tagging
	AlbumArtist string
	TrackNumber int
	TotalTracks int
	DiscNumber  int
	TotalDiscs  int
	Date        string // release date (YYYY or YYYY-MM-DD)
	Copyright   string
	ISRC        string
	CoverURL    string // URL to album cover art

	// MusicBrainz enrichment (populated via --musicbrainz flag)
	Genre                  string
	MusicBrainzRecordingID string
	MusicBrainzReleaseID   string
	MusicBrainzArtistID    string
}

// Album represents a generic album from any music service.
type Album struct {
	ID          string
	Title       string
	Artist      string
	Tracks      []Track
	Date        string // release date
	TotalTracks int
	TotalDiscs  int
	CoverURL    string
}

// Config holds common application configuration.
type Config struct {
	Format      string `json:"format"`
	Debug       bool   `json:"debug"`
	OutputDir   string `json:"output_dir,omitempty"`
	Quality     string `json:"quality,omitempty"`
	MusicBrainz bool   `json:"musicbrainz,omitempty"`
}

// DefaultConfig returns a default configuration.
func DefaultConfig() Config {
	return Config{
		Format:  "%artist%/%album%/%track% - %title%.%extension%",
		Debug:   false,
		Quality: "LOSSLESS",
	}
}

// Provider is the interface that all music service providers must implement.
type Provider interface {
	// Name returns the service name (e.g., "tidal", "spotify", "deezer").
	Name() string

	// MatchesURL returns true if the URL belongs to this service.
	MatchesURL(url string) bool

	// URLType returns the type of content the URL points to ("track", "album", "artist", or "").
	URLType(url string) string

	// IsAuthenticated checks if the provider has valid credentials.
	IsAuthenticated() bool

	// GetTrack fetches track metadata by ID.
	GetTrack(id string) (Track, error)

	// GetAlbum fetches album metadata and tracks by ID.
	GetAlbum(id string) (Album, error)

	// GetStreamURL returns the direct stream URL for a track.
	GetStreamURL(trackID string) (string, error)

	// DownloadTrack downloads a track by its service URL.
	DownloadTrack(url string) error

	// DownloadAlbum downloads all tracks from an album by its service URL.
	DownloadAlbum(url string) error

	// DownloadArtist fetches all albums for an artist, presents a selection UI,
	// and downloads the selected albums.
	DownloadArtist(url string) error

	// SetDebug enables or disables debug output.
	SetDebug(enabled bool)
}

// --- Shared Utilities ---

// SanitizeFilename removes characters that are problematic in filenames.
func SanitizeFilename(s string) string {
	replacements := map[string]string{
		"/":  "-",
		"\\": "-",
		":":  " - ",
		"*":  "",
		"?":  "",
		"\"": "'",
		"<":  "",
		">":  "",
		"|":  "-",
	}

	for old, new := range replacements {
		s = strings.ReplaceAll(s, old, new)
	}

	return strings.TrimSpace(s)
}

// GetDirectory extracts the directory portion of a path.
func GetDirectory(path string) string {
	if idx := strings.LastIndexAny(path, "/\\"); idx != -1 {
		return path[:idx]
	}
	return ""
}

// MimeToExtension converts a MIME type to a file extension.
func MimeToExtension(mimeType string) string {
	switch {
	case strings.Contains(mimeType, "flac"):
		return "flac"
	case strings.Contains(mimeType, "mp4"), strings.Contains(mimeType, "aac"):
		return "m4a"
	case strings.Contains(mimeType, "mpeg"):
		return "mp3"
	case strings.Contains(mimeType, "ogg"):
		return "ogg"
	case strings.Contains(mimeType, "wav"):
		return "wav"
	default:
		return "m4a"
	}
}

// FormatFilename applies a format template to generate a filename.
// If outputDir is provided in the config, it is prepended to the path.
//
// Supported placeholders:
//
//	%artist%, %title%, %album%, %id%, %quality%, %extension%
//	%albumartist% - album artist (falls back to track artist)
//	%track%  - zero-padded track number (e.g. "01")
//	%disc%   - disc number
//	%year%   - release year (e.g. "2024")
//	%date%   - full release date (e.g. "2024-01-15")
func FormatFilename(format string, track Track, extension string, outputDir string) string {
	filename := format

	// Use album artist for folder structure, falling back to track artist.
	// This keeps compilations/DJ mixes in one folder instead of scattering
	// tracks across per-artist directories.
	artist := track.AlbumArtist
	if artist == "" {
		artist = track.Artist
	}
	if artist == "" {
		artist = "Unknown Artist"
	}

	title := track.Title
	if title == "" {
		title = "Unknown Title"
	}

	album := track.Album
	if album == "" {
		album = "Unknown Album"
	}

	filename = strings.ReplaceAll(filename, "%id%", track.ID)
	filename = strings.ReplaceAll(filename, "%albumartist%", SanitizeFilename(artist))
	filename = strings.ReplaceAll(filename, "%artist%", SanitizeFilename(artist))
	filename = strings.ReplaceAll(filename, "%title%", SanitizeFilename(title))
	filename = strings.ReplaceAll(filename, "%album%", SanitizeFilename(album))
	filename = strings.ReplaceAll(filename, "%quality%", track.Quality)
	filename = strings.ReplaceAll(filename, "%extension%", extension)

	// Track number: zero-padded to 2 digits (or more if TotalTracks >= 100)
	if track.TrackNumber > 0 {
		pad := 2
		if track.TotalTracks >= 100 {
			pad = 3
		}
		filename = strings.ReplaceAll(filename, "%track%", fmt.Sprintf("%0*d", pad, track.TrackNumber))
	} else {
		filename = strings.ReplaceAll(filename, "%track%", "00")
	}

	// Disc number
	if track.DiscNumber > 0 {
		filename = strings.ReplaceAll(filename, "%disc%", fmt.Sprintf("%d", track.DiscNumber))
	} else {
		filename = strings.ReplaceAll(filename, "%disc%", "1")
	}

	// Date and year
	if track.Date != "" {
		filename = strings.ReplaceAll(filename, "%date%", track.Date)
		// Extract year (first 4 characters if available)
		year := track.Date
		if len(track.Date) >= 4 {
			year = track.Date[:4]
		}
		filename = strings.ReplaceAll(filename, "%year%", year)
	} else {
		filename = strings.ReplaceAll(filename, "%date%", "Unknown")
		filename = strings.ReplaceAll(filename, "%year%", "Unknown")
	}

	if outputDir != "" {
		filename = filepath.Join(outputDir, filename)
	}

	return filename
}
