package provider

import (
	"strings"
)

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
}

// Album represents a generic album from any music service.
type Album struct {
	ID     string
	Title  string
	Artist string
	Tracks []Track
}

// Config holds common application configuration.
type Config struct {
	Format string `json:"format"`
	Debug  bool   `json:"debug"`
}

// DefaultConfig returns a default configuration.
func DefaultConfig() Config {
	return Config{
		Format: "%artist% - %title%.%extension%",
		Debug:  false,
	}
}

// Provider is the interface that all music service providers must implement.
type Provider interface {
	// Name returns the service name (e.g., "tidal", "spotify", "deezer").
	Name() string

	// MatchesURL returns true if the URL belongs to this service.
	MatchesURL(url string) bool

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
func FormatFilename(format string, track Track, extension string) string {
	filename := format

	artist := track.Artist
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
	filename = strings.ReplaceAll(filename, "%artist%", SanitizeFilename(artist))
	filename = strings.ReplaceAll(filename, "%title%", SanitizeFilename(title))
	filename = strings.ReplaceAll(filename, "%album%", SanitizeFilename(album))
	filename = strings.ReplaceAll(filename, "%quality%", track.Quality)
	filename = strings.ReplaceAll(filename, "%extension%", extension)

	return filename
}
