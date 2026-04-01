package deezer

import "encoding/json"

// --- Public API Response Types (api.deezer.com) ---

// ArtistRef is a compact artist reference embedded in track and album responses.
type ArtistRef struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	PictureSmall  string `json:"picture_small,omitempty"`
	PictureMedium string `json:"picture_medium,omitempty"`
	PictureBig    string `json:"picture_big,omitempty"`
	PictureXL     string `json:"picture_xl,omitempty"`
}

// AlbumRef is a compact album reference embedded in track responses.
type AlbumRef struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	CoverSmall  string `json:"cover_small,omitempty"`
	CoverMedium string `json:"cover_medium,omitempty"`
	CoverBig    string `json:"cover_big,omitempty"`
	CoverXL     string `json:"cover_xl,omitempty"`
	MD5Image    string `json:"md5_image,omitempty"`
}

// Contributor represents a credited contributor on a track.
type Contributor struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// Genre represents a music genre.
type Genre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// GenreList wraps a list of genres.
type GenreList struct {
	Data []Genre `json:"data"`
}

// TrackList wraps a list of tracks in an album response.
type TrackList struct {
	Data []TrackResponse `json:"data"`
}

// TrackResponse is the public API response for a single track.
type TrackResponse struct {
	ID            int           `json:"id"`
	Title         string        `json:"title"`
	TitleShort    string        `json:"title_short"`
	ISRC          string        `json:"isrc"`
	Duration      int           `json:"duration"`
	TrackPosition int           `json:"track_position"`
	DiskNumber    int           `json:"disk_number"`
	ReleaseDate   string        `json:"release_date"`
	Preview       string        `json:"preview"`
	MD5Image      string        `json:"md5_image"`
	Artist        ArtistRef     `json:"artist"`
	Album         AlbumRef      `json:"album"`
	Contributors  []Contributor `json:"contributors"`
	Type          string        `json:"type"`
}

// AlbumResponse is the public API response for an album.
type AlbumResponse struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	UPC         string    `json:"upc"`
	CoverSmall  string    `json:"cover_small"`
	CoverMedium string    `json:"cover_medium"`
	CoverBig    string    `json:"cover_big"`
	CoverXL     string    `json:"cover_xl"`
	MD5Image    string    `json:"md5_image"`
	GenreID     int       `json:"genre_id"`
	Genres      GenreList `json:"genres"`
	Label       string    `json:"label"`
	NbTracks    int       `json:"nb_tracks"`
	Duration    int       `json:"duration"`
	ReleaseDate string    `json:"release_date"`
	RecordType  string    `json:"record_type"`
	Available   bool      `json:"available"`
	Artist      ArtistRef `json:"artist"`
	Tracks      TrackList `json:"tracks"`
	Type        string    `json:"type"`
}

// ArtistResponse is the public API response for an artist.
type ArtistResponse struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Link          string `json:"link"`
	PictureSmall  string `json:"picture_small"`
	PictureMedium string `json:"picture_medium"`
	PictureBig    string `json:"picture_big"`
	PictureXL     string `json:"picture_xl"`
	NbAlbum       int    `json:"nb_album"`
	NbFan         int    `json:"nb_fan"`
	Type          string `json:"type"`
}

// ArtistAlbumsResponse is the paginated response for an artist's albums.
type ArtistAlbumsResponse struct {
	Data  []AlbumResponse `json:"data"`
	Total int             `json:"total"`
	Next  string          `json:"next"`
}

// --- Private API Response Types (gw-light.php) ---

// GWResponse is the generic wrapper for all gw-light.php API responses.
type GWResponse struct {
	Error   []interface{}   `json:"error"`
	Results json.RawMessage `json:"results"`
}

// GWSongData holds the private API song data from song.getData.
type GWSongData struct {
	SNG_ID             string `json:"SNG_ID"`
	SNG_TITLE          string `json:"SNG_TITLE"`
	ALB_ID             string `json:"ALB_ID"`
	ALB_TITLE          string `json:"ALB_TITLE"`
	ALB_PICTURE        string `json:"ALB_PICTURE"`
	ART_ID             string `json:"ART_ID"`
	ART_NAME           string `json:"ART_NAME"`
	DURATION           string `json:"DURATION"`
	MD5_ORIGIN         string `json:"MD5_ORIGIN"`
	MEDIA_VERSION      string `json:"MEDIA_VERSION"`
	TRACK_TOKEN        string `json:"TRACK_TOKEN"`
	TRACK_TOKEN_EXPIRE int64  `json:"TRACK_TOKEN_EXPIRE"`
	ISRC               string `json:"ISRC"`
	DISK_NUMBER        string `json:"DISK_NUMBER"`
	TRACK_NUMBER       string `json:"TRACK_NUMBER"`
	GAIN               string `json:"GAIN"`
	FILESIZE_MP3_128   string `json:"FILESIZE_MP3_128"`
	FILESIZE_MP3_320   string `json:"FILESIZE_MP3_320"`
	FILESIZE_FLAC      string `json:"FILESIZE_FLAC"`
	COPYRIGHT          string `json:"COPYRIGHT"`
	FALLBACK           struct {
		SNG_ID        string `json:"SNG_ID"`
		MD5_ORIGIN    string `json:"MD5_ORIGIN"`
		MEDIA_VERSION string `json:"MEDIA_VERSION"`
		TRACK_TOKEN   string `json:"TRACK_TOKEN"`
	} `json:"FALLBACK"`
}

// GWUserData holds the private API user data from deezer.getUserData.
type GWUserData struct {
	USER struct {
		USER_ID int64 `json:"USER_ID"`
		OPTIONS struct {
			LicenseToken string `json:"license_token"`
			WebLossless  bool   `json:"web_lossless"`
			WebHQ        bool   `json:"web_hq"`
		} `json:"OPTIONS"`
	} `json:"USER"`
	CheckForm string `json:"checkForm"`
	SessionID string `json:"SESSION_ID"`
}

// --- Media API Types (media.deezer.com/v1/get_url) ---

// MediaRequest is the payload sent to media.deezer.com/v1/get_url.
type MediaRequest struct {
	LicenseToken string       `json:"license_token"`
	Media        []MediaTrack `json:"media"`
	TrackTokens  []string     `json:"track_tokens"`
}

// MediaTrack describes the requested media format.
type MediaTrack struct {
	Type    string        `json:"type"`
	Formats []MediaFormat `json:"formats"`
}

// MediaFormat specifies cipher and format for a media request.
type MediaFormat struct {
	Cipher string `json:"cipher"`
	Format string `json:"format"`
}

// MediaResponse is the response from media.deezer.com/v1/get_url.
type MediaResponse struct {
	Data []MediaData `json:"data"`
}

// MediaData holds the media URL data for a single track.
type MediaData struct {
	Media  []MediaSource `json:"media"`
	Errors []MediaError  `json:"errors"`
}

// MediaError represents an error returned inside media API data entries.
// Error code 2002 indicates geo-restriction.
type MediaError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MediaSource holds sources for a given media item.
type MediaSource struct {
	Format  string     `json:"format"`
	Cipher  string     `json:"cipher"`
	Sources []MediaURL `json:"sources"`
}

// MediaURL is a single stream source URL.
type MediaURL struct {
	URL      string `json:"url"`
	Provider string `json:"provider"`
}

// --- Auth Types ---

// Auth holds Deezer authentication credentials and session data.
type Auth struct {
	ARL               string `json:"arl"`
	SessionID         string `json:"session_id"`
	LicenseToken      string `json:"license_token"`
	APIToken          string `json:"api_token"` // checkForm value
	UserID            string `json:"user_id"`
	CanStreamLossless bool   `json:"can_stream_lossless"`
	CanStreamHQ       bool   `json:"can_stream_hq"`
}
