package provider

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// AudioInfo contains the important metadata from an audio file.
type AudioInfo struct {
	Provider      string  `json:"provider"`
	Codec         string  `json:"codec"`
	SampleRate    string  `json:"sample_rate"`
	Channels      int     `json:"channels"`
	ChannelLayout string  `json:"channel_layout"`
	BitDepth      string  `json:"bit_depth"`
	Duration      float64 `json:"duration"`
	Bitrate       int64   `json:"bitrate"`
	FileSize      int64   `json:"file_size"`
	Format        string  `json:"format"`
}

// ffprobeOutput represents the JSON structure from ffprobe.
type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
	Format  ffprobeFormat   `json:"format"`
}

type ffprobeStream struct {
	CodecName        string `json:"codec_name"`
	CodecLongName    string `json:"codec_long_name"`
	SampleRate       string `json:"sample_rate"`
	Channels         int    `json:"channels"`
	ChannelLayout    string `json:"channel_layout"`
	BitsPerRawSample string `json:"bits_per_raw_sample"`
}

type ffprobeFormat struct {
	Filename       string `json:"filename"`
	FormatName     string `json:"format_name"`
	FormatLongName string `json:"format_long_name"`
	Duration       string `json:"duration"`
	Size           string `json:"size"`
	BitRate        string `json:"bit_rate"`
}

// ProbeAudioFile uses ffprobe to extract metadata from an audio file.
func ProbeAudioFile(filePath string) (*AudioInfo, error) {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		filePath,
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w (is ffprobe installed?)", err)
	}

	var probe ffprobeOutput
	if err := json.Unmarshal(output, &probe); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	info := &AudioInfo{
		Format: probe.Format.FormatName,
	}

	// Parse format info
	if probe.Format.Duration != "" {
		fmt.Sscanf(probe.Format.Duration, "%f", &info.Duration)
	}
	if probe.Format.Size != "" {
		fmt.Sscanf(probe.Format.Size, "%d", &info.FileSize)
	}
	if probe.Format.BitRate != "" {
		fmt.Sscanf(probe.Format.BitRate, "%d", &info.Bitrate)
	}

	// Get audio stream info (first audio stream)
	for _, stream := range probe.Streams {
		if stream.CodecName != "" {
			info.Codec = stream.CodecName
			info.SampleRate = stream.SampleRate
			info.Channels = stream.Channels
			info.ChannelLayout = stream.ChannelLayout
			info.BitDepth = stream.BitsPerRawSample
			break
		}
	}

	return info, nil
}

// PrintAudioInfo prints audio metadata in a formatted way.
func PrintAudioInfo(info *AudioInfo) {
	fmt.Println("\n┌─ Audio Info ────────────────────────────────")
	if info.Provider != "" {
		fmt.Printf("│ Provider:      %s\n", strings.Title(info.Provider))
	}
	fmt.Printf("│ Codec:         %s\n", strings.ToUpper(info.Codec))
	fmt.Printf("│ Sample Rate:   %s Hz\n", info.SampleRate)
	fmt.Printf("│ Channels:      %d (%s)\n", info.Channels, info.ChannelLayout)
	if info.BitDepth != "" && info.BitDepth != "0" {
		fmt.Printf("│ Bit Depth:     %s-bit\n", info.BitDepth)
	}
	fmt.Printf("│ Duration:      %s\n", formatDuration(info.Duration))
	fmt.Printf("│ Bitrate:       %s\n", formatBitrate(info.Bitrate))
	fmt.Printf("│ File Size:     %s\n", formatFileSize(info.FileSize))
	fmt.Println("└──────────────────────────────────────────────")
}

// formatDuration converts seconds to mm:ss format.
func formatDuration(seconds float64) string {
	mins := int(seconds) / 60
	secs := int(seconds) % 60
	return fmt.Sprintf("%d:%02d", mins, secs)
}

// formatBitrate formats bitrate in kbps.
func formatBitrate(bitrate int64) string {
	if bitrate == 0 {
		return "N/A"
	}
	kbps := bitrate / 1000
	return fmt.Sprintf("%d kbps", kbps)
}

// formatFileSize formats file size in human readable format.
func formatFileSize(size int64) string {
	if size == 0 {
		return "N/A"
	}

	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case size >= GB:
		return fmt.Sprintf("%.2f GB", float64(size)/GB)
	case size >= MB:
		return fmt.Sprintf("%.2f MB", float64(size)/MB)
	case size >= KB:
		return fmt.Sprintf("%.2f KB", float64(size)/KB)
	default:
		return fmt.Sprintf("%d bytes", size)
	}
}
