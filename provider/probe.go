package provider

import (
	"encoding/json"
	"fmt"
	"os/exec"
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
