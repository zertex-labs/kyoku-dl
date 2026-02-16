package provider

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// TagAudioFile embeds metadata tags and optional cover art into an audio file
// using ffmpeg. The file is re-muxed with -codec copy (no re-encoding).
// Returns an error if ffmpeg is not available or the tagging fails.
func TagAudioFile(filePath string, track Track) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}

	// Build ffmpeg arguments
	// -i <input> -metadata key=value ... -codec copy <output>
	// ffmpeg can't write in-place, so we write to a temp file and rename.
	dir := filepath.Dir(filePath)
	tmpFile := filepath.Join(dir, ".tag_tmp_"+filepath.Base(filePath))

	args := []string{
		"-i", filePath,
		"-y", // overwrite temp file if it exists
	}

	// Download and attach cover art if available
	var coverTmp string
	if track.CoverURL != "" {
		var err error
		coverTmp, err = downloadCoverArt(track.CoverURL, dir)
		if err == nil {
			args = append(args, "-i", coverTmp)
			args = append(args,
				"-map", "0:a", // audio from first input
				"-map", "1:0", // image from second input
				"-metadata:s:v", "title=Album cover",
				"-metadata:s:v", "comment=Cover (front)",
			)

			// Set disposition based on format
			ext := filepath.Ext(filePath)
			if ext == ".flac" {
				args = append(args, "-disposition:v", "attached_pic")
			} else {
				args = append(args, "-disposition:v", "attached_pic")
			}
		}
		// If cover download fails, continue without cover art
	}

	// Add metadata tags
	if track.Title != "" {
		args = append(args, "-metadata", "title="+track.Title)
	}
	if track.Artist != "" {
		args = append(args, "-metadata", "artist="+track.Artist)
	}
	if track.Album != "" {
		args = append(args, "-metadata", "album="+track.Album)
	}
	if track.AlbumArtist != "" {
		args = append(args, "-metadata", "album_artist="+track.AlbumArtist)
	}
	if track.TrackNumber > 0 {
		trackNum := strconv.Itoa(track.TrackNumber)
		if track.TotalTracks > 0 {
			trackNum += "/" + strconv.Itoa(track.TotalTracks)
		}
		args = append(args, "-metadata", "track="+trackNum)
	}
	if track.DiscNumber > 0 {
		discNum := strconv.Itoa(track.DiscNumber)
		if track.TotalDiscs > 0 {
			discNum += "/" + strconv.Itoa(track.TotalDiscs)
		}
		args = append(args, "-metadata", "disc="+discNum)
	}
	if track.Date != "" {
		args = append(args, "-metadata", "date="+track.Date)
	}
	if track.Copyright != "" {
		args = append(args, "-metadata", "copyright="+track.Copyright)
	}
	if track.ISRC != "" {
		args = append(args, "-metadata", "ISRC="+track.ISRC)
	}

	// MusicBrainz enrichment tags
	if track.Genre != "" {
		args = append(args, "-metadata", "genre="+track.Genre)
	}
	if track.MusicBrainzRecordingID != "" {
		args = append(args, "-metadata", "MUSICBRAINZ_TRACKID="+track.MusicBrainzRecordingID)
	}
	if track.MusicBrainzReleaseID != "" {
		args = append(args, "-metadata", "MUSICBRAINZ_ALBUMID="+track.MusicBrainzReleaseID)
	}
	if track.MusicBrainzArtistID != "" {
		args = append(args, "-metadata", "MUSICBRAINZ_ARTISTID="+track.MusicBrainzArtistID)
	}

	args = append(args, "-codec", "copy", tmpFile)

	// Run ffmpeg
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = io.Discard // suppress ffmpeg stderr noise
	cmd.Stdout = io.Discard

	if err := cmd.Run(); err != nil {
		// Clean up temp files on failure
		os.Remove(tmpFile)
		if coverTmp != "" {
			os.Remove(coverTmp)
		}
		return fmt.Errorf("ffmpeg tagging failed: %w", err)
	}

	// Clean up cover art temp file
	if coverTmp != "" {
		os.Remove(coverTmp)
	}

	// Replace original with tagged version
	if err := os.Rename(tmpFile, filePath); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to replace file with tagged version: %w", err)
	}

	return nil
}

// downloadCoverArt downloads a cover art image to a temporary file in the given directory.
// Returns the path to the temporary file.
func downloadCoverArt(coverURL, dir string) (string, error) {
	resp, err := http.Get(coverURL)
	if err != nil {
		return "", fmt.Errorf("failed to download cover art: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("cover art download failed with status %d", resp.StatusCode)
	}

	tmpFile := filepath.Join(dir, ".cover_tmp.jpg")
	out, err := os.Create(tmpFile)
	if err != nil {
		return "", fmt.Errorf("failed to create cover temp file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		os.Remove(tmpFile)
		return "", fmt.Errorf("failed to write cover art: %w", err)
	}

	return tmpFile, nil
}
