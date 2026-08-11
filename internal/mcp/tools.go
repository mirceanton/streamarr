package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mirceanton/streamarr/internal/db"
	"github.com/mirceanton/streamarr/internal/models"
	"github.com/mirceanton/streamarr/internal/processor"
	"github.com/mirceanton/streamarr/internal/scanner"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var libraryTypes = []string{"movies", "shows", "music"}

// ListAttentionMediaInput is the input for the list_attention_media tool.
type ListAttentionMediaInput struct {
	LibraryType string `json:"library_type,omitempty" jsonschema:"Restrict results to one library type: movies, shows, or music. Omit to check every library type."`
}

// AttentionMediaFile summarizes a media file that needs attention.
type AttentionMediaFile struct {
	ID               int64    `json:"id" jsonschema:"Media file ID. Pass this to get_media_attention_reasons or trigger_track_job."`
	Title            string   `json:"title"`
	Path             string   `json:"path"`
	LibraryType      string   `json:"library_type"`
	Season           *int     `json:"season,omitempty"`
	Episode          *int     `json:"episode,omitempty"`
	AttentionReasons []string `json:"attention_reasons"`
}

// ListAttentionMediaOutput is the output of the list_attention_media tool.
type ListAttentionMediaOutput struct {
	Count int                  `json:"count"`
	Files []AttentionMediaFile `json:"files"`
}

func listAttentionMedia(_ context.Context, _ *mcpsdk.CallToolRequest, in ListAttentionMediaInput) (*mcpsdk.CallToolResult, ListAttentionMediaOutput, error) {
	types := libraryTypes
	if in.LibraryType != "" {
		if !slices.Contains(libraryTypes, in.LibraryType) {
			return nil, ListAttentionMediaOutput{}, fmt.Errorf("invalid library_type %q: must be one of %s", in.LibraryType, strings.Join(libraryTypes, ", "))
		}
		types = []string{in.LibraryType}
	}

	out := ListAttentionMediaOutput{Files: []AttentionMediaFile{}}
	for _, t := range types {
		files, err := db.GetMediaFilesByLibraryType(t, true)
		if err != nil {
			return nil, ListAttentionMediaOutput{}, fmt.Errorf("list %s needing attention: %w", t, err)
		}
		for _, f := range files {
			out.Files = append(out.Files, AttentionMediaFile{
				ID:               f.ID,
				Title:            f.Title,
				Path:             f.Path,
				LibraryType:      f.LibraryType,
				Season:           f.Season,
				Episode:          f.Episode,
				AttentionReasons: splitReasons(f.AttentionReasons),
			})
		}
	}
	out.Count = len(out.Files)
	return nil, out, nil
}

// GetAttentionReasonsInput is the input for the get_media_attention_reasons tool.
type GetAttentionReasonsInput struct {
	MediaFileID int64 `json:"media_file_id" jsonschema:"ID of the media file to inspect, as returned by list_attention_media."`
}

// ExternalSubtitleInfo describes a subtitle sidecar file found on disk alongside a media file.
type ExternalSubtitleInfo struct {
	Path     string `json:"path" jsonschema:"Full path to the sidecar subtitle file. Pass this as source_path to a delete_external_subtitle operation in trigger_track_job."`
	Filename string `json:"filename"`
	Language string `json:"language"`
	Format   string `json:"format" jsonschema:"Subtitle format, e.g. srt, ass, ssa, vtt, sub."`
	Forced   bool   `json:"forced"`
	SDH      bool   `json:"sdh"`
}

// GetAttentionReasonsOutput is the output of the get_media_attention_reasons tool.
type GetAttentionReasonsOutput struct {
	Title                 string                 `json:"title"`
	Path                  string                 `json:"path"`
	NeedsAttention        bool                   `json:"needs_attention"`
	AttentionReasons      []string               `json:"attention_reasons"`
	ExternalSubtitleFiles []ExternalSubtitleInfo `json:"external_subtitle_files" jsonschema:"Subtitle sidecar files on disk next to this media file (not embedded in the container). Not covered by remove_subtitle/extract_subtitle — use delete_external_subtitle instead."`
}

func getAttentionReasons(_ context.Context, _ *mcpsdk.CallToolRequest, in GetAttentionReasonsInput) (*mcpsdk.CallToolResult, GetAttentionReasonsOutput, error) {
	mf, err := db.GetMediaFile(in.MediaFileID)
	if err != nil {
		return nil, GetAttentionReasonsOutput{}, fmt.Errorf("media file %d not found: %w", in.MediaFileID, err)
	}
	extSubs := make([]ExternalSubtitleInfo, len(mf.ExternalSubtitleFiles))
	for i, es := range mf.ExternalSubtitleFiles {
		extSubs[i] = ExternalSubtitleInfo{
			Path:     es.Path,
			Filename: es.Filename,
			Language: es.Language,
			Format:   es.Format,
			Forced:   es.Forced,
			SDH:      es.SDH,
		}
	}
	return nil, GetAttentionReasonsOutput{
		Title:                 mf.Title,
		Path:                  mf.Path,
		NeedsAttention:        mf.NeedsAttention,
		AttentionReasons:      splitReasons(mf.AttentionReasons),
		ExternalSubtitleFiles: extSubs,
	}, nil
}

var allowedTrackOps = map[string]bool{
	"remove_audio":             true,
	"remove_subtitle":          true,
	"extract_subtitle":         true,
	"delete_external_subtitle": true,
}

// TrackOperation describes a single track action within a trigger_track_job call.
type TrackOperation struct {
	Type        string `json:"type" jsonschema:"One of: remove_audio, remove_subtitle, extract_subtitle, delete_external_subtitle."`
	StreamIndex int    `json:"stream_index,omitempty" jsonschema:"ffmpeg stream index of the audio or subtitle track to act on. Required for remove_audio, remove_subtitle, extract_subtitle."`
	SourcePath  string `json:"source_path,omitempty" jsonschema:"Path of the external subtitle sidecar file to delete, as returned by get_media_attention_reasons. Required for delete_external_subtitle."`
}

// TriggerTrackJobInput is the input for the trigger_track_job tool.
type TriggerTrackJobInput struct {
	MediaFileID int64            `json:"media_file_id" jsonschema:"ID of the media file to modify, as returned by list_attention_media."`
	Operations  []TrackOperation `json:"operations" jsonschema:"One or more track operations to run as a single job."`
}

// TriggerTrackJobOutput is the output of the trigger_track_job tool.
type TriggerTrackJobOutput struct {
	JobID  int64  `json:"job_id"`
	Status string `json:"status"`
}

func triggerTrackJob(_ context.Context, _ *mcpsdk.CallToolRequest, in TriggerTrackJobInput) (*mcpsdk.CallToolResult, TriggerTrackJobOutput, error) {
	if len(in.Operations) == 0 {
		return nil, TriggerTrackJobOutput{}, fmt.Errorf("at least one operation is required")
	}

	mf, err := db.GetMediaFile(in.MediaFileID)
	if err != nil {
		return nil, TriggerTrackJobOutput{}, fmt.Errorf("media file %d not found: %w", in.MediaFileID, err)
	}

	hasPending, err := db.HasPendingJob(in.MediaFileID)
	if err != nil {
		return nil, TriggerTrackJobOutput{}, err
	}
	if hasPending {
		return nil, TriggerTrackJobOutput{}, fmt.Errorf("media file %d already has a pending or running job", in.MediaFileID)
	}

	audioRemoveCount := 0
	ops := make([]models.Operation, len(in.Operations))
	for i, op := range in.Operations {
		if !allowedTrackOps[op.Type] {
			return nil, TriggerTrackJobOutput{}, fmt.Errorf("invalid operation type %q: must be one of remove_audio, remove_subtitle, extract_subtitle, delete_external_subtitle", op.Type)
		}

		ops[i] = models.Operation{Type: op.Type, StreamIndex: op.StreamIndex}

		switch op.Type {
		case "remove_audio":
			audioRemoveCount++
			if !hasAudioStream(mf.AudioTracks, op.StreamIndex) {
				return nil, TriggerTrackJobOutput{}, fmt.Errorf("no audio track with stream index %d on media file %d", op.StreamIndex, in.MediaFileID)
			}
		case "remove_subtitle", "extract_subtitle":
			st := findSubtitleTrack(mf.SubtitleTracks, op.StreamIndex)
			if st == nil {
				return nil, TriggerTrackJobOutput{}, fmt.Errorf("no subtitle track with stream index %d on media file %d", op.StreamIndex, in.MediaFileID)
			}
			if op.Type == "extract_subtitle" {
				ops[i].OutputPath = extractSubtitlePath(mf, st)
			}
		case "delete_external_subtitle":
			if op.SourcePath == "" {
				return nil, TriggerTrackJobOutput{}, fmt.Errorf("source_path is required for delete_external_subtitle")
			}
			if !hasExternalSubtitle(mf.ExternalSubtitleFiles, op.SourcePath) {
				return nil, TriggerTrackJobOutput{}, fmt.Errorf("no external subtitle file at path %q on media file %d", op.SourcePath, in.MediaFileID)
			}
			ops[i].SourcePath = op.SourcePath
		}
	}

	if len(mf.AudioTracks) > 0 && audioRemoveCount >= len(mf.AudioTracks) {
		return nil, TriggerTrackJobOutput{}, fmt.Errorf("cannot remove all audio tracks from media file %d", in.MediaFileID)
	}

	jobID, err := db.CreateJob(in.MediaFileID, ops)
	if err != nil {
		return nil, TriggerTrackJobOutput{}, err
	}
	processor.Enqueue(jobID)

	return nil, TriggerTrackJobOutput{JobID: jobID, Status: "pending"}, nil
}

func hasAudioStream(tracks []models.AudioTrack, idx int) bool {
	for _, t := range tracks {
		if t.StreamIndex == idx {
			return true
		}
	}
	return false
}

func findSubtitleTrack(tracks []models.SubtitleTrack, idx int) *models.SubtitleTrack {
	for i := range tracks {
		if tracks[i].StreamIndex == idx {
			return &tracks[i]
		}
	}
	return nil
}

func hasExternalSubtitle(files []models.ExternalSubtitleFile, path string) bool {
	for _, f := range files {
		if f.Path == path {
			return true
		}
	}
	return false
}

func extractSubtitlePath(mf *models.MediaFile, st *models.SubtitleTrack) string {
	ext := scanner.SubtitleExtension(st.Codec)
	lang := st.Language
	if lang == "" {
		lang = "und"
	}
	baseName := strings.TrimSuffix(mf.Filename, filepath.Ext(mf.Filename))
	return filepath.Join(filepath.Dir(mf.Path), fmt.Sprintf("%s.%s.%s", baseName, lang, ext))
}

// RunFFprobeInput is the input for the run_ffprobe tool.
type RunFFprobeInput struct {
	MediaFileID int64 `json:"media_file_id" jsonschema:"ID of the media file to probe, as returned by list_attention_media."`
}

// RunFFprobeOutput is the output of the run_ffprobe tool.
type RunFFprobeOutput struct {
	Path   string         `json:"path"`
	Result map[string]any `json:"result" jsonschema:"Raw ffprobe JSON output (streams and format) for the file."`
}

func runFFprobe(_ context.Context, _ *mcpsdk.CallToolRequest, in RunFFprobeInput) (*mcpsdk.CallToolResult, RunFFprobeOutput, error) {
	mf, err := db.GetMediaFile(in.MediaFileID)
	if err != nil {
		return nil, RunFFprobeOutput{}, fmt.Errorf("media file %d not found: %w", in.MediaFileID, err)
	}

	raw, err := scanner.ProbeRaw(mf.Path)
	if err != nil {
		return nil, RunFFprobeOutput{}, err
	}

	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, RunFFprobeOutput{}, fmt.Errorf("parse ffprobe output for %s: %w", mf.Path, err)
	}

	return nil, RunFFprobeOutput{Path: mf.Path, Result: result}, nil
}

func splitReasons(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, "\n")
}

var (
	validSubtitleFormats = map[string]bool{"srt": true, "ass": true, "vtt": true, "pgs": true, "dvd": true}
	validAudioFormats    = map[string]bool{"flac": true, "mp3": true, "aac": true, "opus": true}
)

// resolveTextOverrideScope resolves the (item_type, item_key) pair used by the
// language_overrides and subtitle_format_overrides tables for a given media file
// and requested scope. Movies are always scoped to the file itself. Shows default
// to the episode itself ("episode" scope) but can also be scoped to the whole
// series ("series" scope), matching the series-wide overrides already available
// in the web UI.
func resolveTextOverrideScope(mf *models.MediaFile, scope string) (itemType, itemKey string, err error) {
	switch mf.LibraryType {
	case "movies":
		return "movie", mf.Path, nil
	case "shows":
		if scope == "" {
			scope = "episode"
		}
		switch scope {
		case "episode":
			return "episode", mf.Path, nil
		case "series":
			key := mf.Title
			if key == "" {
				key = "Unknown Series"
			}
			return "series", key, nil
		default:
			return "", "", fmt.Errorf("invalid scope %q: must be episode or series", scope)
		}
	default:
		return "", "", fmt.Errorf("media file %d is a %s file; language and subtitle format preferences only apply to movies and shows", mf.ID, mf.LibraryType)
	}
}

// rescanAndReport rescans a media file (recomputing needs_attention with any
// overrides just applied) and returns its refreshed attention state.
func rescanAndReport(mf *models.MediaFile) (bool, []string, error) {
	if err := scanner.RescanFile(mf); err != nil {
		return false, nil, fmt.Errorf("rescan media file %d: %w", mf.ID, err)
	}
	updated, err := db.GetMediaFile(mf.ID)
	if err != nil {
		return false, nil, fmt.Errorf("reload media file %d: %w", mf.ID, err)
	}
	return updated.NeedsAttention, splitReasons(updated.AttentionReasons), nil
}

// PreferenceUpdateOutput is the shared output shape for tools that set or clear a
// per-item preference and then re-evaluate whether the file still needs attention.
type PreferenceUpdateOutput struct {
	MediaFileID      int64    `json:"media_file_id"`
	Scope            string   `json:"scope" jsonschema:"The scope the preference was applied to: movie, episode, or series."`
	AppliedTo        string   `json:"applied_to" jsonschema:"The key identifying what the preference applies to: a file path (movie/episode) or a title (series/album)."`
	NeedsAttention   bool     `json:"needs_attention" jsonschema:"Whether the file still needs attention after this change, recomputed immediately."`
	AttentionReasons []string `json:"attention_reasons"`
}

// SetLanguagePreferenceInput is the input for the set_language_preference tool.
type SetLanguagePreferenceInput struct {
	MediaFileID int64    `json:"media_file_id" jsonschema:"ID of the media file, as returned by list_attention_media."`
	Languages   []string `json:"languages" jsonschema:"Preferred audio/subtitle language codes (e.g. eng, fra). Files matching one of these languages will no longer be flagged for language reasons."`
	Scope       string   `json:"scope,omitempty" jsonschema:"Shows only: episode (default) to set this just for this episode, or series to set it for the whole series. Ignored for movies, which are always scoped to the file itself."`
}

func setLanguagePreference(_ context.Context, _ *mcpsdk.CallToolRequest, in SetLanguagePreferenceInput) (*mcpsdk.CallToolResult, PreferenceUpdateOutput, error) {
	if len(in.Languages) == 0 {
		return nil, PreferenceUpdateOutput{}, fmt.Errorf("at least one language is required")
	}

	mf, err := db.GetMediaFile(in.MediaFileID)
	if err != nil {
		return nil, PreferenceUpdateOutput{}, fmt.Errorf("media file %d not found: %w", in.MediaFileID, err)
	}

	itemType, itemKey, err := resolveTextOverrideScope(mf, in.Scope)
	if err != nil {
		return nil, PreferenceUpdateOutput{}, err
	}

	langs := make([]string, len(in.Languages))
	for i, l := range in.Languages {
		langs[i] = strings.ToLower(strings.TrimSpace(l))
	}

	if err := db.SetLanguageOverride(mf.LibraryRootID, itemKey, itemType, langs); err != nil {
		return nil, PreferenceUpdateOutput{}, err
	}

	needsAttention, reasons, err := rescanAndReport(mf)
	if err != nil {
		return nil, PreferenceUpdateOutput{}, err
	}
	return nil, PreferenceUpdateOutput{
		MediaFileID: in.MediaFileID, Scope: itemType, AppliedTo: itemKey,
		NeedsAttention: needsAttention, AttentionReasons: reasons,
	}, nil
}

// ClearLanguagePreferenceInput is the input for the clear_language_preference tool.
type ClearLanguagePreferenceInput struct {
	MediaFileID int64  `json:"media_file_id" jsonschema:"ID of the media file, as returned by list_attention_media."`
	Scope       string `json:"scope,omitempty" jsonschema:"Shows only: episode (default) or series, matching whichever scope the preference was set at. Ignored for movies."`
}

func clearLanguagePreference(_ context.Context, _ *mcpsdk.CallToolRequest, in ClearLanguagePreferenceInput) (*mcpsdk.CallToolResult, PreferenceUpdateOutput, error) {
	mf, err := db.GetMediaFile(in.MediaFileID)
	if err != nil {
		return nil, PreferenceUpdateOutput{}, fmt.Errorf("media file %d not found: %w", in.MediaFileID, err)
	}

	itemType, itemKey, err := resolveTextOverrideScope(mf, in.Scope)
	if err != nil {
		return nil, PreferenceUpdateOutput{}, err
	}

	if err := db.DeleteLanguageOverride(mf.LibraryRootID, itemKey, itemType); err != nil {
		return nil, PreferenceUpdateOutput{}, err
	}

	needsAttention, reasons, err := rescanAndReport(mf)
	if err != nil {
		return nil, PreferenceUpdateOutput{}, err
	}
	return nil, PreferenceUpdateOutput{
		MediaFileID: in.MediaFileID, Scope: itemType, AppliedTo: itemKey,
		NeedsAttention: needsAttention, AttentionReasons: reasons,
	}, nil
}

// SetSubtitleFormatPreferenceInput is the input for the set_subtitle_format_preference tool.
type SetSubtitleFormatPreferenceInput struct {
	MediaFileID int64  `json:"media_file_id" jsonschema:"ID of the media file, as returned by list_attention_media."`
	Format      string `json:"format" jsonschema:"Preferred subtitle format: srt, ass, vtt, pgs, or dvd."`
	Scope       string `json:"scope,omitempty" jsonschema:"Shows only: episode (default) to set this just for this episode, or series to set it for the whole series. Ignored for movies, which are always scoped to the file itself."`
}

func setSubtitleFormatPreference(_ context.Context, _ *mcpsdk.CallToolRequest, in SetSubtitleFormatPreferenceInput) (*mcpsdk.CallToolResult, PreferenceUpdateOutput, error) {
	format := strings.ToLower(strings.TrimSpace(in.Format))
	if !validSubtitleFormats[format] {
		return nil, PreferenceUpdateOutput{}, fmt.Errorf("invalid format %q: must be one of srt, ass, vtt, pgs, dvd", in.Format)
	}

	mf, err := db.GetMediaFile(in.MediaFileID)
	if err != nil {
		return nil, PreferenceUpdateOutput{}, fmt.Errorf("media file %d not found: %w", in.MediaFileID, err)
	}

	itemType, itemKey, err := resolveTextOverrideScope(mf, in.Scope)
	if err != nil {
		return nil, PreferenceUpdateOutput{}, err
	}

	if err := db.SetSubtitleFormatOverride(mf.LibraryRootID, itemKey, itemType, format); err != nil {
		return nil, PreferenceUpdateOutput{}, err
	}

	needsAttention, reasons, err := rescanAndReport(mf)
	if err != nil {
		return nil, PreferenceUpdateOutput{}, err
	}
	return nil, PreferenceUpdateOutput{
		MediaFileID: in.MediaFileID, Scope: itemType, AppliedTo: itemKey,
		NeedsAttention: needsAttention, AttentionReasons: reasons,
	}, nil
}

// ClearSubtitleFormatPreferenceInput is the input for the clear_subtitle_format_preference tool.
type ClearSubtitleFormatPreferenceInput struct {
	MediaFileID int64  `json:"media_file_id" jsonschema:"ID of the media file, as returned by list_attention_media."`
	Scope       string `json:"scope,omitempty" jsonschema:"Shows only: episode (default) or series, matching whichever scope the preference was set at. Ignored for movies."`
}

func clearSubtitleFormatPreference(_ context.Context, _ *mcpsdk.CallToolRequest, in ClearSubtitleFormatPreferenceInput) (*mcpsdk.CallToolResult, PreferenceUpdateOutput, error) {
	mf, err := db.GetMediaFile(in.MediaFileID)
	if err != nil {
		return nil, PreferenceUpdateOutput{}, fmt.Errorf("media file %d not found: %w", in.MediaFileID, err)
	}

	itemType, itemKey, err := resolveTextOverrideScope(mf, in.Scope)
	if err != nil {
		return nil, PreferenceUpdateOutput{}, err
	}

	if err := db.DeleteSubtitleFormatOverride(mf.LibraryRootID, itemKey, itemType); err != nil {
		return nil, PreferenceUpdateOutput{}, err
	}

	needsAttention, reasons, err := rescanAndReport(mf)
	if err != nil {
		return nil, PreferenceUpdateOutput{}, err
	}
	return nil, PreferenceUpdateOutput{
		MediaFileID: in.MediaFileID, Scope: itemType, AppliedTo: itemKey,
		NeedsAttention: needsAttention, AttentionReasons: reasons,
	}, nil
}

// SetAudioPreferenceInput is the input for the set_audio_preference tool.
type SetAudioPreferenceInput struct {
	MediaFileID    int64  `json:"media_file_id" jsonschema:"ID of the music track, as returned by list_attention_media."`
	Format         string `json:"format,omitempty" jsonschema:"Preferred audio format for this track's album: flac, mp3, aac, or opus. Omit to leave the format preference unchanged."`
	MinBitrateKbps *int   `json:"min_bitrate_kbps,omitempty" jsonschema:"Minimum acceptable bitrate in kbps for this track's album. 0 means no minimum is required. Omit to leave the bitrate preference unchanged."`
}

// AudioPreferenceUpdateOutput is the output of the set_audio_preference and clear_audio_preference tools.
type AudioPreferenceUpdateOutput struct {
	MediaFileID      int64    `json:"media_file_id"`
	AlbumKey         string   `json:"album_key" jsonschema:"The artist/album this preference was applied to. Audio preferences are scoped per album, not per track."`
	NeedsAttention   bool     `json:"needs_attention"`
	AttentionReasons []string `json:"attention_reasons"`
}

func setAudioPreference(_ context.Context, _ *mcpsdk.CallToolRequest, in SetAudioPreferenceInput) (*mcpsdk.CallToolResult, AudioPreferenceUpdateOutput, error) {
	format := strings.ToLower(strings.TrimSpace(in.Format))
	if format != "" && !validAudioFormats[format] {
		return nil, AudioPreferenceUpdateOutput{}, fmt.Errorf("invalid format %q: must be one of flac, mp3, aac, opus", in.Format)
	}
	if format == "" && in.MinBitrateKbps == nil {
		return nil, AudioPreferenceUpdateOutput{}, fmt.Errorf("at least one of format or min_bitrate_kbps is required")
	}
	if in.MinBitrateKbps != nil && *in.MinBitrateKbps < 0 {
		return nil, AudioPreferenceUpdateOutput{}, fmt.Errorf("min_bitrate_kbps must be >= 0")
	}

	mf, err := db.GetMediaFile(in.MediaFileID)
	if err != nil {
		return nil, AudioPreferenceUpdateOutput{}, fmt.Errorf("media file %d not found: %w", in.MediaFileID, err)
	}
	if mf.LibraryType != "music" {
		return nil, AudioPreferenceUpdateOutput{}, fmt.Errorf("media file %d is a %s file; audio format/bitrate preferences only apply to music", in.MediaFileID, mf.LibraryType)
	}

	albumKey := mf.Artist + "/" + mf.Album
	if format != "" {
		if err := db.SetAudioFormatOverride(mf.LibraryRootID, albumKey, "album", format); err != nil {
			return nil, AudioPreferenceUpdateOutput{}, err
		}
	}
	if in.MinBitrateKbps != nil {
		if err := db.SetMinBitrateOverride(mf.LibraryRootID, albumKey, "album", *in.MinBitrateKbps); err != nil {
			return nil, AudioPreferenceUpdateOutput{}, err
		}
	}

	needsAttention, reasons, err := rescanAndReport(mf)
	if err != nil {
		return nil, AudioPreferenceUpdateOutput{}, err
	}
	return nil, AudioPreferenceUpdateOutput{
		MediaFileID: in.MediaFileID, AlbumKey: albumKey,
		NeedsAttention: needsAttention, AttentionReasons: reasons,
	}, nil
}

// ClearAudioPreferenceInput is the input for the clear_audio_preference tool.
type ClearAudioPreferenceInput struct {
	MediaFileID  int64 `json:"media_file_id" jsonschema:"ID of the music track, as returned by list_attention_media."`
	ClearFormat  bool  `json:"clear_format,omitempty" jsonschema:"Clear the album's preferred audio format override, reverting to the global setting."`
	ClearBitrate bool  `json:"clear_bitrate,omitempty" jsonschema:"Clear the album's minimum bitrate override, reverting to the global setting."`
}

func clearAudioPreference(_ context.Context, _ *mcpsdk.CallToolRequest, in ClearAudioPreferenceInput) (*mcpsdk.CallToolResult, AudioPreferenceUpdateOutput, error) {
	if !in.ClearFormat && !in.ClearBitrate {
		return nil, AudioPreferenceUpdateOutput{}, fmt.Errorf("at least one of clear_format or clear_bitrate must be true")
	}

	mf, err := db.GetMediaFile(in.MediaFileID)
	if err != nil {
		return nil, AudioPreferenceUpdateOutput{}, fmt.Errorf("media file %d not found: %w", in.MediaFileID, err)
	}
	if mf.LibraryType != "music" {
		return nil, AudioPreferenceUpdateOutput{}, fmt.Errorf("media file %d is a %s file; audio format/bitrate preferences only apply to music", in.MediaFileID, mf.LibraryType)
	}

	albumKey := mf.Artist + "/" + mf.Album
	if in.ClearFormat {
		if err := db.SetAudioFormatOverride(mf.LibraryRootID, albumKey, "album", ""); err != nil {
			return nil, AudioPreferenceUpdateOutput{}, err
		}
	}
	if in.ClearBitrate {
		if err := db.DeleteMinBitrateOverride(mf.LibraryRootID, albumKey, "album"); err != nil {
			return nil, AudioPreferenceUpdateOutput{}, err
		}
	}

	needsAttention, reasons, err := rescanAndReport(mf)
	if err != nil {
		return nil, AudioPreferenceUpdateOutput{}, err
	}
	return nil, AudioPreferenceUpdateOutput{
		MediaFileID: in.MediaFileID, AlbumKey: albumKey,
		NeedsAttention: needsAttention, AttentionReasons: reasons,
	}, nil
}
