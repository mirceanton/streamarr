package scanner

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/mirceanton/streamarr/internal/models"
)

// flexInt unmarshals from either a JSON number or a JSON string containing a number.
// ffprobe returns bits_per_raw_sample and bits_per_sample as strings in some versions.
type flexInt int

func (f *flexInt) UnmarshalJSON(data []byte) error {
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*f = flexInt(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		*f = 0
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*f = flexInt(n)
	return nil
}

// ffprobeOutput represents the JSON output from ffprobe.
type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
	Format  ffprobeFormat   `json:"format"`
}

type ffprobeStream struct {
	Index            int     `json:"index"`
	CodecName        string  `json:"codec_name"`
	CodecType        string  `json:"codec_type"` // video, audio, subtitle
	Channels         int     `json:"channels"`
	BitRate          string  `json:"bit_rate"`
	SampleRate       string  `json:"sample_rate"`
	BitsPerRawSample flexInt `json:"bits_per_raw_sample"`
	BitsPerSample    flexInt `json:"bits_per_sample"`
	Tags             struct {
		Language    string `json:"language"`
		Title       string `json:"title"`
		Artist      string `json:"artist"`
		Album       string `json:"album"`
		AlbumArtist string `json:"album_artist"`
		TrackNum    string `json:"track"`
		Date        string `json:"date"`
	} `json:"tags"`
	Disposition struct {
		Default         int `json:"default"`
		Forced          int `json:"forced"`
		HearingImpaired int `json:"hearing_impaired"`
	} `json:"disposition"`
}

type ffprobeFormat struct {
	BitRate string `json:"bit_rate"`
	Tags    struct {
		Artist      string `json:"artist"`
		Album       string `json:"album"`
		AlbumArtist string `json:"album_artist"`
		Title       string `json:"title"`
		TrackNum    string `json:"track"`
		Date        string `json:"date"`
	} `json:"tags"`
}

// Probe runs ffprobe on a file and returns parsed audio and subtitle tracks.
func Probe(filepath string) ([]models.AudioTrack, []models.SubtitleTrack, error) {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-show_format",
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

// MusicProbeResult holds metadata extracted from a music file.
type MusicProbeResult struct {
	Codec      string
	Bitrate    int64 // bits per second
	SampleRate int   // Hz
	BitDepth   int   // bits per sample (0 if not reported)
	Artist     string
	Album      string
	Title      string
	TrackNum   int
	Year       int
}

// ProbeMusic runs ffprobe on a music file and returns its audio metadata.
func ProbeMusic(filePath string) (*MusicProbeResult, error) {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-show_format",
		filePath,
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed for %s: %w", filePath, err)
	}

	var result ffprobeOutput
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("parse ffprobe output: %w", err)
	}

	res := &MusicProbeResult{}

	// Find the primary audio stream
	for _, s := range result.Streams {
		if s.CodecType != "audio" {
			continue
		}
		res.Codec = strings.ToLower(s.CodecName)

		// Bit depth: prefer bits_per_raw_sample, fall back to bits_per_sample
		if s.BitsPerRawSample > 0 {
			res.BitDepth = int(s.BitsPerRawSample)
		} else if s.BitsPerSample > 0 {
			res.BitDepth = int(s.BitsPerSample)
		}

		// Sample rate
		if s.SampleRate != "" {
			if sr, err := strconv.Atoi(s.SampleRate); err == nil {
				res.SampleRate = sr
			}
		}

		// Stream-level bitrate
		if s.BitRate != "" {
			if br, err := strconv.ParseInt(s.BitRate, 10, 64); err == nil {
				res.Bitrate = br
			}
		}

		// ID3 tags from stream level
		res.Artist = firstNonEmpty(s.Tags.AlbumArtist, s.Tags.Artist)
		res.Album = s.Tags.Album
		res.Title = s.Tags.Title
		if s.Tags.TrackNum != "" {
			// Track number may be "5/12" format
			parts := strings.SplitN(s.Tags.TrackNum, "/", 2)
			if n, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil {
				res.TrackNum = n
			}
		}
		if s.Tags.Date != "" {
			if len(s.Tags.Date) >= 4 {
				if y, err := strconv.Atoi(s.Tags.Date[:4]); err == nil {
					res.Year = y
				}
			}
		}
		break
	}

	// Fall back to format-level bitrate if stream didn't have one
	if res.Bitrate == 0 && result.Format.BitRate != "" {
		if br, err := strconv.ParseInt(result.Format.BitRate, 10, 64); err == nil {
			res.Bitrate = br
		}
	}

	// Fall back to format-level tags
	if res.Artist == "" {
		res.Artist = firstNonEmpty(result.Format.Tags.AlbumArtist, result.Format.Tags.Artist)
	}
	if res.Album == "" {
		res.Album = result.Format.Tags.Album
	}
	if res.Title == "" {
		res.Title = result.Format.Tags.Title
	}
	if res.TrackNum == 0 && result.Format.Tags.TrackNum != "" {
		parts := strings.SplitN(result.Format.Tags.TrackNum, "/", 2)
		if n, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil {
			res.TrackNum = n
		}
	}
	if res.Year == 0 && result.Format.Tags.Date != "" {
		if len(result.Format.Tags.Date) >= 4 {
			if y, err := strconv.Atoi(result.Format.Tags.Date[:4]); err == nil {
				res.Year = y
			}
		}
	}

	return res, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// IsLosslessCodec reports whether the given ffmpeg codec name is a lossless audio format.
func IsLosslessCodec(codec string) bool {
	switch strings.ToLower(codec) {
	case "flac", "alac", "wavpack", "ape", "tak", "wv",
		"pcm_s16le", "pcm_s16be", "pcm_s24le", "pcm_s24be",
		"pcm_s32le", "pcm_s32be", "pcm_f32le", "pcm_f64le",
		"pcm_u8", "aiff":
		return true
	}
	return false
}

// IsLossyCodec reports whether the given ffmpeg codec name is a lossy audio format.
func IsLossyCodec(codec string) bool {
	switch strings.ToLower(codec) {
	case "mp3", "aac", "vorbis", "opus", "wma", "wmav2", "wmav1",
		"ac3", "eac3", "dts", "dca", "mp2", "mp1", "amr_nb", "amr_wb",
		"speex", "gsm", "g723_1", "g729":
		return true
	}
	return false
}

// AudioFileExtension returns the output container extension for a target codec.
func AudioFileExtension(codec string) string {
	switch strings.ToLower(codec) {
	case "flac":
		return "flac"
	case "mp3":
		return "mp3"
	case "aac":
		return "m4a"
	case "opus":
		return "opus"
	case "alac":
		return "m4a"
	case "vorbis":
		return "ogg"
	default:
		return strings.ToLower(codec)
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
