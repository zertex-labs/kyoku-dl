package tidal

// --- Tidal API Response Types ---

// Artist represents a Tidal artist in API responses.
type Artist struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Picture string `json:"picture,omitempty"`
}

// Album represents a Tidal album in API responses.
type Album struct {
	ID             int    `json:"id"`
	Title          string `json:"title"`
	Cover          string `json:"cover,omitempty"`
	NumberOfTracks int    `json:"numberOfTracks"`
}

// TrackMetadata is the structure for track info from /tracks/{trackId}
type TrackMetadata struct {
	ID           int      `json:"id"`
	Title        string   `json:"title"`
	Duration     int      `json:"duration"` // seconds
	TrackNumber  int      `json:"trackNumber"`
	VolumeNumber int      `json:"volumeNumber"`
	AudioQuality string   `json:"audioQuality"`
	Artist       Artist   `json:"artist"`
	Artists      []Artist `json:"artists,omitempty"`
	Album        Album    `json:"album"`
	Copyright    string   `json:"copyright,omitempty"`
	ISRC         string   `json:"isrc,omitempty"`
}

// AlbumMetadata is the structure for album info from /albums/{albumId}
type AlbumMetadata struct {
	ID              int      `json:"id"`
	Title           string   `json:"title"`
	Duration        int      `json:"duration"`
	NumberOfTracks  int      `json:"numberOfTracks"`
	NumberOfVolumes int      `json:"numberOfVolumes"`
	ReleaseDate     string   `json:"releaseDate"`
	Copyright       string   `json:"copyright,omitempty"`
	AudioQuality    string   `json:"audioQuality"`
	Artist          Artist   `json:"artist"`
	Artists         []Artist `json:"artists,omitempty"`
	Cover           string   `json:"cover,omitempty"`
	UPC             string   `json:"upc,omitempty"`
}

// AlbumTracksResponse is the structure for /albums/{albumId}/tracks
type AlbumTracksResponse struct {
	Limit              int             `json:"limit"`
	Offset             int             `json:"offset"`
	TotalNumberOfItems int             `json:"totalNumberOfItems"`
	Items              []TrackMetadata `json:"items"`
}

// PlaybackInfo is the structure from /playbackinfopostpaywall
type PlaybackInfo struct {
	TrackID           int    `json:"trackId"`
	AssetPresentation string `json:"assetPresentation"`
	AudioMode         string `json:"audioMode"`
	AudioQuality      string `json:"audioQuality"`
	ManifestMimeType  string `json:"manifestMimeType"`
	Manifest          string `json:"manifest"` // Base64 encoded JSON
}

// StreamManifest is the decoded manifest structure.
type StreamManifest struct {
	MimeType       string   `json:"mimeType"`
	Codecs         string   `json:"codecs,omitempty"`
	EncryptionType string   `json:"encryptionType"`
	URLs           []string `json:"urls"`
	KeyID          string   `json:"keyId,omitempty"`
}

// --- Auth Types ---

// Auth holds Tidal authentication credentials.
type Auth struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	UserID       string `json:"user_id"`
	CountryCode  string `json:"country_code"`
}
