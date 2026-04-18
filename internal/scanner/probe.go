package scanner

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/mirceanton/streamarr/internal/models"
)

// ffprobeOutput represents the JSON output from ffprobe.
type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
}

type ffprobeStream struct {
	Index     int    `json:"index"`
	CodecName string `json:"codec_name"`
	CodecType string `json:"codec_type"` // video, audio, subtitle
	Channels  int    `json:"channels"`
	Tags      struct {
		Language string `json:"language"`
		Title    string `json:"title"`
	} `json:"tags"`
	Disposition struct {
		Default         int `json:"default"`
		Forced          int `json:"forced"`
		HearingImpaired int `json:"hearing_impaired"`
	} `json:"disposition"`
}

// Probe runs ffprobe on a file and returns parsed audio and subtitle tracks.
func Probe(filepath string) ([]models.AudioTrack, []models.SubtitleTrack, error) {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		filepath,
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("ffprobe failed for %s: %w", filepath, err)
	}

	var result ffprobeOutput
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, nil, fmt.Errorf("parse ffprobe output: %w", err)
	}

	var audioTracks []models.AudioTrack
	var subtitleTracks []models.SubtitleTrack

	for _, s := range result.Streams {
		switch s.CodecType {
		case "audio":
			audioTracks = append(audioTracks, models.AudioTrack{
				StreamIndex:  s.Index,
				Codec:        s.CodecName,
				Language:     strings.ToLower(s.Tags.Language),
				Title:        s.Tags.Title,
				Channels:     s.Channels,
				DefaultTrack: s.Disposition.Default == 1,
				Forced:       s.Disposition.Forced == 1,
			})
		case "subtitle":
			subtitleTracks = append(subtitleTracks, models.SubtitleTrack{
				StreamIndex:  s.Index,
				Codec:        s.CodecName,
				Language:     strings.ToLower(s.Tags.Language),
				Title:        s.Tags.Title,
				DefaultTrack: s.Disposition.Default == 1,
				Forced:       s.Disposition.Forced == 1,
				SDH:          s.Disposition.HearingImpaired == 1,
			})
		}
	}

	return audioTracks, subtitleTracks, nil
}

// CodecDisplayName returns a human-friendly name for a codec.
func CodecDisplayName(codec string) string {
	names := map[string]string{
		"aac":               "AAC",
		"ac3":               "AC3",
		"eac3":              "EAC3",
		"dts":               "DTS",
		"dca":               "DTS",
		"truehd":            "TrueHD",
		"flac":              "FLAC",
		"mp3":               "MP3",
		"mp2":               "MP2",
		"vorbis":            "Vorbis",
		"opus":              "Opus",
		"pcm_s16le":         "PCM",
		"pcm_s24le":         "PCM 24-bit",
		"subrip":            "SubRip (SRT)",
		"srt":               "SubRip (SRT)",
		"ass":               "ASS",
		"ssa":               "SSA",
		"webvtt":            "WebVTT",
		"mov_text":          "MOV Text",
		"hdmv_pgs_subtitle": "PGS",
		"dvd_subtitle":      "VOBSUB",
		"dvdsub":            "VOBSUB",
	}
	if name, ok := names[strings.ToLower(codec)]; ok {
		return name
	}
	return strings.ToUpper(codec)
}

// IsImageBasedSubtitle returns true for subtitle codecs that are image-based.
func IsImageBasedSubtitle(codec string) bool {
	switch strings.ToLower(codec) {
	case "hdmv_pgs_subtitle", "dvd_subtitle", "dvdsub", "pgssub":
		return true
	}
	return false
}

// SubtitleExtension returns the appropriate file extension for a subtitle codec.
func SubtitleExtension(codec string) string {
	switch strings.ToLower(codec) {
	case "subrip", "srt":
		return "srt"
	case "ass":
		return "ass"
	case "ssa":
		return "ssa"
	case "webvtt":
		return "vtt"
	case "mov_text":
		return "srt"
	case "hdmv_pgs_subtitle", "pgssub":
		return "sup"
	case "dvd_subtitle", "dvdsub":
		return "sub"
	default:
		return "srt"
	}
}

// externalSubtitleExts contains file extensions recognised as external subtitle sidecar files.
var externalSubtitleExts = map[string]bool{
	".srt": true,
	".ass": true,
	".ssa": true,
	".vtt": true,
	".sub": true,
	".sup": true,
}

// IsExternalSubtitleExt reports whether ext (e.g. ".srt") is a supported external subtitle extension.
func IsExternalSubtitleExt(ext string) bool {
	return externalSubtitleExts[strings.ToLower(ext)]
}

// ChannelLayout returns a human-readable channel layout description.
func ChannelLayout(channels int) string {
	switch channels {
	case 1:
		return "Mono"
	case 2:
		return "Stereo"
	case 6:
		return "5.1"
	case 8:
		return "7.1"
	default:
		return fmt.Sprintf("%dch", channels)
	}
}
