package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// --- Configuration & Required Data based on Tidal API Interaction ---

const (
	// NOTE: Replace this with your actual, live Tidal authentication token.
	TIDAL_AUTH_TOKEN = "TOKEN_HERE"

	// !!! IMPORTANT: CHANGE THIS TO MATCH YOUR ACCOUNT'S REGISTRATION COUNTRY !!!
	COUNTRY_CODE = "US" // Required for playback info

	TIDAL_API_BASE_URL = "https://api.tidal.com/v1/"

	// Set to true to print the full JSON response body for every Tidal API call.
	DEBUG = true

	CONFIG_FILENAME = "config.json"
)

// --- JSON Structs for API Response Parsing ---

// TidalArtist is a nested structure for the track's primary artist.
type TidalArtist struct {
	Name string `json:"name"`
	// Additional fields like ID, Picture etc., are omitted for brevity
}

// TrackMetadata is the structure for the initial track info request (/tracks/{trackId})
type TrackMetadata struct {
	AudioQuality string      `json:"audioQuality"`
	Title        string      `json:"title"` // Added for filename formatting
	Artist       TidalArtist `json:"artist"` // Added for filename formatting
}

// PlaybackInfo is the outer JSON structure from /playbackinfopostpaywall
type PlaybackInfo struct {
	TrackID      int    `json:"trackId"`
	AudioQuality string `json:"audioQuality"`
	Manifest     string `json:"manifest"` // Base64 encoded JSON
}

// StreamManifest is the inner JSON structure from the base64 decoded manifest string
type StreamManifest struct {
	MimeType       string   `json:"mimeType"`
	EncryptionType string   `json:"encryptionType"`
	URLs           []string `json:"urls"`
	// Additional fields like KeyID would be here for encrypted streams
}

// --- Configuration Struct ---
type Config struct {
	Format string `json:"format"` // E.g., "%artist%/%title%.%extension%"
}

var config Config // Global variable to hold the loaded config

// loadConfig reads the config.json file into the global Config struct.
func loadConfig() error {
	fmt.Printf("Attempting to load config from %s...\n", CONFIG_FILENAME)
	data, err := os.ReadFile(CONFIG_FILENAME)
	if err != nil {
		fmt.Printf("WARNING: Could not read %s. Using default format 'tidal_track_%%id_%%quality.%%extension'. Error: %v\n", CONFIG_FILENAME, err)
		config.Format = "tidal_track_%id_%quality.%extension%" // Default fallback format
		return nil
	}

	if err := json.Unmarshal(data, &config); err != nil {
		fmt.Printf("ERROR: Failed to parse %s. Using default format. Error: %v\n", CONFIG_FILENAME, err)
		config.Format = "tidal_track_%id_%quality.%extension%" // Default fallback format
		return err
	}

	fmt.Printf("Config loaded successfully. Filename format: **%s**\n", config.Format)
	return nil
}

// sanitizeFilename removes characters that are illegal or problematic in filenames.
func sanitizeFilename(s string) string {
	// Replace problematic characters with an underscore
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "\\", "-")
	s = strings.ReplaceAll(s, ":", " - ")
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "?", "")
	s = strings.ReplaceAll(s, "\"", "'")
	s = strings.ReplaceAll(s, "<", "")
	s = strings.ReplaceAll(s, ">", "")
	s = strings.ReplaceAll(s, "|", "-")

	// Remove leading/trailing spaces
	s = strings.TrimSpace(s)
	return s
}

// applyFormat applies the loaded format string to generate the final filename.
func applyFormat(format string, trackID, artistName, title, quality, extension string) string {
	// Replace placeholders with actual values
	filename := format
	filename = strings.ReplaceAll(filename, "%id%", trackID)
	filename = strings.ReplaceAll(filename, "%artist%", sanitizeFilename(artistName))
	filename = strings.ReplaceAll(filename, "%title%", sanitizeFilename(title))
	filename = strings.ReplaceAll(filename, "%quality%", quality)
	filename = strings.ReplaceAll(filename, "%extension%", extension)

	// Ensure directory structure is created (if format includes paths)
	dir := ""
	if lastSeparator := strings.LastIndexAny(filename, "/\\"); lastSeparator != -1 {
		dir = filename[:lastSeparator]
		// Create the directory if it doesn't exist
		if dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				fmt.Printf("WARNING: Failed to create directory structure '%s'. Saving to current directory. Error: %v\n", dir, err)
				filename = strings.TrimLeft(filename[lastSeparator:], "/\\")
			}
		}
	}

	return filename
}

// --- Core Functions (Unchanged or Minorly Adjusted) ---

// extractTrackID parses the Tidal URL to get the track ID.
func extractTrackID(rawURL string) string {
	re := regexp.MustCompile(`track/(\d+)$`)
	match := re.FindStringSubmatch(rawURL)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

// performAPICall executes a generic GET request to the Tidal API.
func performAPICall(path string, query url.Values, token string) ([]byte, int, error) {
	fullAPIURL := TIDAL_API_BASE_URL + path + "?" + query.Encode()

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", fullAPIURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "Go-Tidal-Downloader/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("error executing request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("error reading response body: %w", err)
	}

	// Print the full API response body if DEBUG is true
	if DEBUG {
		fmt.Printf("\n--- DEBUG: Full API Response Body for Path: %s ---\n", path)
		fmt.Println(string(body))
		fmt.Println("--- END DEBUG ---\n")
	}

	return body, resp.StatusCode, nil
}

// downloadFile performs the HTTP GET request for the actual audio data and saves it.
func downloadFile(streamURL, fileName string) error {
	fmt.Printf("Starting download to: %s\n", fileName)

	// Step 1: Request the stream
	resp, err := http.Get(streamURL)
	if err != nil {
		return fmt.Errorf("failed to fetch stream URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download request failed with status: %s", resp.Status)
	}

	// Step 2: Create the file (Handles path creation via applyFormat)
	out, err := os.Create(fileName)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", fileName, err)
	}
	defer out.Close()

	// Step 3: Write the body to file
	n, err := io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write stream to file: %w", err)
	}

	fmt.Printf("Download complete! Wrote %d bytes.\n", n)
	return nil
}

// writeMetadataFile combines the two raw API responses and saves them to a JSON file.
// streamURL should be passed as the successfully extracted URL, or a descriptive error string if not available.
func writeMetadataFile(metadataBody, playbackBody []byte, streamURL string, trackID string) error {
	outputFilename := fmt.Sprintf("metadata_%s.json", trackID)

	// Create a map to hold both responses
	combinedData := map[string]interface{}{}

	// Unmarshal the raw bytes into generic interfaces (map[string]interface{})
	var meta map[string]interface{}
	var playback map[string]interface{}

	// Attempt to unmarshal, but treat as empty object on error to keep the flow going
	if err := json.Unmarshal(metadataBody, &meta); err != nil {
		meta = map[string]interface{}{"parsingError": fmt.Sprintf("Failed to parse track metadata: %v", err)}
	}
	if err := json.Unmarshal(playbackBody, &playback); err != nil {
		playback = map[string]interface{}{"parsingError": fmt.Sprintf("Failed to parse playback info: %v", err)}
	}

	combinedData["trackMetadata"] = meta
	combinedData["playbackInfo"] = playback

	// Explicitly add the resolved stream URL for inspection
	combinedData["resolvedStreamURL"] = streamURL

	// Marshal the combined data into pretty-printed JSON
	finalJSON, err := json.MarshalIndent(combinedData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal combined JSON: %w", err)
	}

	// Write the final JSON to the file
	if err := os.WriteFile(outputFilename, finalJSON, 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", outputFilename, err)
	}

	fmt.Printf("\nSUCCESS: Saved full API responses and resolved URL to: %s\n", outputFilename)
	return nil
}

// getHighestAudioQuality fetches the track metadata to determine the highest available quality.
// It now returns the raw body and the parsed metadata.
func getHighestAudioQuality(trackIDStr, token string) (TrackMetadata, []byte, error) {
	fmt.Println("\n[STEP 1a: Fetching Track Metadata to find Highest Quality and Filename Info]")

	apiPath := fmt.Sprintf("tracks/%s", trackIDStr)

	// FIX: Added countryCode parameter as required by the API
	query := url.Values{}
	query.Set("countryCode", COUNTRY_CODE)

	body, status, err := performAPICall(apiPath, query, token)
	if err != nil {
		return TrackMetadata{}, nil, fmt.Errorf("error during metadata request: %w", err)
	}

	if status != http.StatusOK {
		return TrackMetadata{}, body, fmt.Errorf("metadata request failed with status: %d. Response: %s", status, string(body))
	}

	var metadata TrackMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		return TrackMetadata{}, body, fmt.Errorf("failed to parse TrackMetadata JSON: %w", err)
	}

	if metadata.AudioQuality == "" {
		return metadata, body, fmt.Errorf("audioQuality field was empty in track metadata response")
	}

	fmt.Printf("Highest advertised audio quality: %s\n", metadata.AudioQuality)
	fmt.Printf("Track Title: '%s', Artist: '%s'\n", metadata.Title, metadata.Artist.Name)
	return metadata, body, nil
}

// fetchTidalStreamInfo constructs and executes the API request, then downloads the track.
func fetchTidalStreamInfo(rawURL string) bool {
	fmt.Println("--- Tidal Stream Link API Request Execution ---")
	fmt.Printf("Target URL: %s\n", rawURL)

	trackIDStr := extractTrackID(rawURL)
	if trackIDStr == "" {
		fmt.Println("\nERROR: Could not parse a valid Track ID from the URL.")
		return false
	}

	if TIDAL_AUTH_TOKEN == "YOUR_TIDAL_AUTH_TOKEN_HERE" {
		fmt.Println("\nFATAL ERROR: Authentication token is missing.")
		fmt.Println("Please replace 'YOUR_TIDAL_AUTH_TOKEN_HERE' in the source code with a valid Tidal authentication token to proceed.")
		return false
	}

	fmt.Printf("Successfully extracted Track ID: %s\n", trackIDStr)

	// --- STEP 1a: GET HIGHEST QUALITY AND METADATA FOR FILENAME ---
	metadata, rawMetadataBody, err := getHighestAudioQuality(trackIDStr, TIDAL_AUTH_TOKEN)
	highestQuality := "LOSSLESS" // Default in case of failure
	if err != nil {
		fmt.Printf("WARNING: Could not determine highest quality and filename metadata (%v). Falling back to **LOSSLESS** and generic filename.\n", err)
		// If API failed, rawMetadataBody contains the error response, or is nil. Initialize if nil.
		if rawMetadataBody == nil {
			rawMetadataBody = []byte(fmt.Sprintf(`{"error": "%v"}`, err))
		}
	} else {
		highestQuality = metadata.AudioQuality
	}

	// --- STEP 1b: REQUEST STREAM PLAYBACK INFO ---
	fmt.Println("\n[STEP 1b: Requesting Post-Paywall Playback Info]")

	apiPath := fmt.Sprintf("tracks/%s/playbackinfopostpaywall", trackIDStr)

	streamQuery := url.Values{}
	streamQuery.Set("audioquality", highestQuality)
	streamQuery.Set("playbackmode", "STREAM")
	streamQuery.Set("assetpresentation", "FULL")
	streamQuery.Set("countrycode", COUNTRY_CODE)

	streamBody, streamStatus, streamErr := performAPICall(apiPath, streamQuery, TIDAL_AUTH_TOKEN)

	// Initialize final stream URL to an error state
	var finalStreamURL = "N/A - API Request Failed or Status Error"
	var mimeType = "audio/mp4" // Default assumption for extension (m4a)

	// Check for total request failure (e.g., network error)
	if streamErr != nil {
		fmt.Printf("ERROR during stream request: %v\n", streamErr)
		// Store error state for metadata file
		streamBody = []byte(fmt.Sprintf(`{"error": "%v"}`, streamErr))
	} else if streamStatus != http.StatusOK {
		// Check for API status error (e.g., 404, 403, 400)
		fmt.Printf("\nERROR: Stream Request Failed with status code %d (Requested Quality: %s)\n", streamStatus, highestQuality)
		if !DEBUG {
			fmt.Println("Response Body (Error Message):")
			fmt.Println(string(streamBody))
		}
		fmt.Println("\nSuggestion: The requested quality might not be available. You might need to manually try 'LOSSLESS', 'HIGH', or 'LOW'.")
	} else {
		// --- STEP 2: PARSE MANIFEST URL ---
		fmt.Println("\n[STEP 2: Parsing Stream Manifest]")
		var playbackInfo PlaybackInfo

		if err := json.Unmarshal(streamBody, &playbackInfo); err != nil {
			fmt.Printf("ERROR: Failed to parse PlaybackInfo JSON: %v\n", err)
			finalStreamURL = "N/A - Failed to parse PlaybackInfo JSON"
		} else {
			// The manifest is Base64-encoded, so we must decode it first.
			decodedManifestBytes, err := base64.StdEncoding.DecodeString(playbackInfo.Manifest)
			if err != nil {
				fmt.Printf("ERROR: Failed to Base64 decode manifest: %v\n", err)
				finalStreamURL = "N/A - Failed to Base64 decode manifest"
			} else {
				var streamManifest StreamManifest
				if err := json.Unmarshal(decodedManifestBytes, &streamManifest); err != nil {
					fmt.Printf("ERROR: Failed to parse StreamManifest JSON: %v\n", err)
					finalStreamURL = "N/A - Failed to parse StreamManifest JSON"
				} else if streamManifest.EncryptionType != "NONE" {
					fmt.Println("\nFATAL: Stream is ENCRYPTED. Decryption required.")
					fmt.Println("Encryption Type:", streamManifest.EncryptionType)
					finalStreamURL = "N/A - Encrypted Stream"
				} else if len(streamManifest.URLs) == 0 {
					fmt.Println("\nERROR: No stream URLs found in the manifest.")
					finalStreamURL = "N/A - No Stream URLs Found"
				} else {
					// Success path
					finalStreamURL = streamManifest.URLs[0]
					mimeType = streamManifest.MimeType // Use the actual mime type
					fmt.Printf("Successfully extracted stream URL (Actual Received Quality: %s):\n%s\n", playbackInfo.AudioQuality, finalStreamURL)
				}
			}
		}
	}


	// --- METADATA FILE WRITE ---
	// Write the raw responses and the final URL status to metadata_<trackId>.json.
	if err := writeMetadataFile(rawMetadataBody, streamBody, finalStreamURL, trackIDStr); err != nil {
		fmt.Printf("WARNING: Failed to write metadata file: %v\n", err)
	}
	// --- END METADATA FILE WRITE ---

	// Only proceed to download if we have a valid stream URL (not an error message)
	if strings.HasPrefix(finalStreamURL, "N/A") {
		return false
	}

	// Determine file extension based on the MIME type
	parts := strings.Split(mimeType, "/")
	ext := "m4a" // Default to m4a for safety
	if len(parts) == 2 && strings.HasPrefix(parts[0], "audio") {
		ext = parts[1]
		if ext == "mp4" {
			ext = "m4a" // Use m4a for audio/mp4 streams
		} else if ext == "flac" {
			ext = "flac"
		} else if ext == "aac" {
			ext = "m4a" // Treat audio/aac as m4a
		}
	}

	// Since we successfully parsed playbackInfo in the success path above, we use its quality.
	// Re-parse streamBody to get playbackInfo for the *actual* received quality.
	var playbackInfo PlaybackInfo
	if err := json.Unmarshal(streamBody, &playbackInfo); err != nil {
		fmt.Printf("WARNING: Could not re-parse PlaybackInfo for final quality. Using LOSSLESS. %v\n", err)
		playbackInfo.AudioQuality = "LOSSLESS"
	}

	// --- STEP 3: CONSTRUCT FILENAME & DOWNLOAD THE STREAM ---
	artistName := metadata.Artist.Name
	trackTitle := metadata.Title
	if artistName == "" { artistName = "UnknownArtist" }
	if trackTitle == "" { trackTitle = "UnknownTitle" }

	// Use the loaded config format
	fileName := applyFormat(config.Format, trackIDStr, artistName, trackTitle, playbackInfo.AudioQuality, ext)

	fmt.Printf("\n[STEP 3: Downloading Audio Stream to '%s']\n", fileName)
	if err := downloadFile(finalStreamURL, fileName); err != nil {
		fmt.Printf("DOWNLOAD FAILED: %v\n", err)
		return false
	}

	fmt.Printf("\n--- Success! ---")
	fmt.Printf("\nTrack %s downloaded and saved as %s\n", trackIDStr, fileName)
	return true
}

func main() {
	// Load configuration first
	if err := loadConfig(); err != nil {
		// loadConfig already prints errors, we can continue with the default format
	}

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s <tidal_url>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nA command-line tool to fetch and download unencrypted Tidal stream data.\n")
		fmt.Fprintf(os.Stderr, "NOTE: You must replace the placeholder token and ensure the COUNTRY_CODE is correct.\n")
		fmt.Fprintf(os.Stderr, "Filename is generated using the format from '%s': **%s**\n", CONFIG_FILENAME, config.Format)
		fmt.Fprintf(os.Stderr, "Example:\n")
		fmt.Fprintf(os.Stderr, "  %s https://tidal.com/browse/track/123456789\n", os.Args[0])
	}

	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}

	urlArgument := flag.Arg(0)

	if !fetchTidalStreamInfo(urlArgument) {
		os.Exit(1)
	}
}
