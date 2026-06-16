package mcp

import (
	"context"
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

// GetAttentionReasonsOutput is the output of the get_media_attention_reasons tool.
type GetAttentionReasonsOutput struct {
	Title            string   `json:"title"`
	Path             string   `json:"path"`
	NeedsAttention   bool     `json:"needs_attention"`
	AttentionReasons []string `json:"attention_reasons"`
}

func getAttentionReasons(_ context.Context, _ *mcpsdk.CallToolRequest, in GetAttentionReasonsInput) (*mcpsdk.CallToolResult, GetAttentionReasonsOutput, error) {
	mf, err := db.GetMediaFile(in.MediaFileID)
	if err != nil {
		return nil, GetAttentionReasonsOutput{}, fmt.Errorf("media file %d not found: %w", in.MediaFileID, err)
	}
	return nil, GetAttentionReasonsOutput{
		Title:            mf.Title,
		Path:             mf.Path,
		NeedsAttention:   mf.NeedsAttention,
		AttentionReasons: splitReasons(mf.AttentionReasons),
	}, nil
}

var allowedTrackOps = map[string]bool{
	"remove_audio":     true,
	"remove_subtitle":  true,
	"extract_subtitle": true,
}

// TrackOperation describes a single track action within a trigger_track_job call.
type TrackOperation struct {
	Type        string `json:"type" jsonschema:"One of: remove_audio, remove_subtitle, extract_subtitle."`
	StreamIndex int    `json:"stream_index" jsonschema:"ffmpeg stream index of the audio or subtitle track to act on."`
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
			return nil, TriggerTrackJobOutput{}, fmt.Errorf("invalid operation type %q: must be one of remove_audio, remove_subtitle, extract_subtitle", op.Type)
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

func extractSubtitlePath(mf *models.MediaFile, st *models.SubtitleTrack) string {
	ext := scanner.SubtitleExtension(st.Codec)
	lang := st.Language
	if lang == "" {
		lang = "und"
	}
	baseName := strings.TrimSuffix(mf.Filename, filepath.Ext(mf.Filename))
	return filepath.Join(filepath.Dir(mf.Path), fmt.Sprintf("%s.%s.%s", baseName, lang, ext))
}

func splitReasons(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, "\n")
}
