package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mirceanton/streamarr/internal/db"
	"github.com/mirceanton/streamarr/internal/models"
	"github.com/mirceanton/streamarr/internal/processor"
	"github.com/mirceanton/streamarr/internal/scanner"
)

// SeriesTracksResponse is returned by GetSeriesTracksHandler.
type SeriesTracksResponse struct {
	EpisodeCount              int      `json:"episode_count"`
	AudioLanguages            []string `json:"audio_languages"`
	SubtitleLanguages         []string `json:"subtitle_languages"`
	ExternalSubtitleLanguages []string `json:"external_subtitle_languages"`
	HasImageBasedSubtitle     bool     `json:"has_image_based_subtitle"`
}

// GetSeriesTracksHandler returns aggregate track language info for all episodes in a series.
// GET /api/series/tracks?title=...&library_root_id=...
func GetSeriesTracksHandler(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Query().Get("title")
	libRootIDStr := r.URL.Query().Get("library_root_id")

	if title == "" || libRootIDStr == "" {
		http.Error(w, "title and library_root_id are required", http.StatusBadRequest)
		return
	}

	libRootID, err := strconv.ParseInt(libRootIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid library_root_id", http.StatusBadRequest)
		return
	}

	episodes, err := db.GetSeriesEpisodesFull(title, libRootID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	audioLangSet := make(map[string]bool)
	subLangSet := make(map[string]bool)
	extSubLangSet := make(map[string]bool)
	hasImageSub := false

	for _, ep := range episodes {
		for _, at := range ep.AudioTracks {
			lang := at.Language
			if lang == "" {
				lang = "und"
			}
			audioLangSet[lang] = true
		}
		for _, st := range ep.SubtitleTracks {
			lang := st.Language
			if lang == "" {
				lang = "und"
			}
			subLangSet[lang] = true
			if scanner.IsImageBasedSubtitle(st.Codec) {
				hasImageSub = true
			}
		}
		for _, esf := range ep.ExternalSubtitleFiles {
			lang := esf.Language
			if lang == "" {
				lang = "und"
			}
			extSubLangSet[lang] = true
		}
	}

	audioLangs := sortedKeys(audioLangSet)
	subLangs := sortedKeys(subLangSet)
	extSubLangs := sortedKeys(extSubLangSet)

	resp := SeriesTracksResponse{
		EpisodeCount:              len(episodes),
		AudioLanguages:            audioLangs,
		SubtitleLanguages:         subLangs,
		ExternalSubtitleLanguages: extSubLangs,
		HasImageBasedSubtitle:     hasImageSub,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// BulkJobRequest is the JSON body for POST /api/bulk-jobs/series.
type BulkJobRequest struct {
	SeriesTitle                     string   `json:"series_title"`
	LibraryRootID                   int64    `json:"library_root_id"`
	KeepAudioLanguages              []string `json:"keep_audio_languages"`
	KeepSubtitleLanguages           []string `json:"keep_subtitle_languages"`
	ExtractSubtitleLanguages        []string `json:"extract_subtitle_languages"`
	EmbedExternalSubtitleLanguages  []string `json:"embed_external_subtitle_languages"`
	DeleteExternalSubtitleLanguages []string `json:"delete_external_subtitle_languages"`
}

// BulkJobsSeriesHandler creates one job per episode in a series based on language-level operations.
// POST /api/bulk-jobs/series
func BulkJobsSeriesHandler(w http.ResponseWriter, r *http.Request) {
	var req BulkJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.SeriesTitle == "" || req.LibraryRootID == 0 {
		http.Error(w, "series_title and library_root_id are required", http.StatusBadRequest)
		return
	}

	episodes, err := db.GetSeriesEpisodesFull(req.SeriesTitle, req.LibraryRootID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(episodes) == 0 {
		http.Error(w, "no episodes found for this series", http.StatusNotFound)
		return
	}

	keepAudio := toLangSet(req.KeepAudioLanguages)
	keepSub := toLangSet(req.KeepSubtitleLanguages)
	extractSub := toLangSet(req.ExtractSubtitleLanguages)
	embedExtSub := toLangSet(req.EmbedExternalSubtitleLanguages)
	deleteExtSub := toLangSet(req.DeleteExternalSubtitleLanguages)

	type result struct {
		Filename string `json:"filename"`
		JobID    int64  `json:"job_id"`
		Skipped  bool   `json:"skipped"`
		Reason   string `json:"reason,omitempty"`
	}
	var results []result

	for _, ep := range episodes {
		// Skip episodes that already have a pending/running job
		hasPending, err := db.HasPendingJob(ep.ID)
		if err != nil {
			results = append(results, result{Filename: ep.Filename, Skipped: true, Reason: "db error checking pending job"})
			continue
		}
		if hasPending {
			results = append(results, result{Filename: ep.Filename, Skipped: true, Reason: "already has pending or running job"})
			continue
		}

		ops := buildEpisodeOps(ep, keepAudio, keepSub, extractSub, embedExtSub, deleteExtSub)
		if len(ops) == 0 {
			results = append(results, result{Filename: ep.Filename, Skipped: true, Reason: "no operations needed"})
			continue
		}

		jobID, err := db.CreateJob(ep.ID, ops)
		if err != nil {
			results = append(results, result{Filename: ep.Filename, Skipped: true, Reason: "failed to create job: " + err.Error()})
			continue
		}
		processor.Enqueue(jobID)
		results = append(results, result{Filename: ep.Filename, JobID: jobID})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"results": results,
	})
}

// buildEpisodeOps generates the operations for a single episode given language keep/extract sets.
func buildEpisodeOps(ep models.MediaFile, keepAudio, keepSub, extractSub, embedExtSub, deleteExtSub map[string]bool) []models.Operation {
	var ops []models.Operation

	// Audio: remove tracks whose language is NOT in keepAudio (if keepAudio is non-empty)
	if len(keepAudio) > 0 {
		// Determine which audio tracks would remain after removals
		removeAudioCount := 0
		for _, at := range ep.AudioTracks {
			lang := at.Language
			if lang == "" {
				lang = "und"
			}
			if !keepAudio[lang] {
				removeAudioCount++
			}
		}
		// Don't remove all audio tracks
		remainingAudio := len(ep.AudioTracks) - removeAudioCount
		if remainingAudio < 1 {
			// Keep at least one track — skip audio removals entirely for this episode
			removeAudioCount = 0
		}
		if removeAudioCount > 0 {
			for _, at := range ep.AudioTracks {
				lang := at.Language
				if lang == "" {
					lang = "und"
				}
				if !keepAudio[lang] {
					ops = append(ops, models.Operation{
						Type:        "remove_audio",
						StreamIndex: at.StreamIndex,
					})
				}
			}
		}
	}

	// Subtitles: first extract (if requested), then remove (if not in keepSub)
	if len(extractSub) > 0 {
		for _, st := range ep.SubtitleTracks {
			lang := st.Language
			if lang == "" {
				lang = "und"
			}
			// Only extract text-based subtitles
			if extractSub[lang] && !scanner.IsImageBasedSubtitle(st.Codec) {
				baseName := strings.TrimSuffix(ep.Filename, filepath.Ext(ep.Filename))
				ext := scanner.SubtitleExtension(st.Codec)
				outputLang := lang
				if outputLang == "" {
					outputLang = "und"
				}
				outputPath := filepath.Join(filepath.Dir(ep.Path), fmt.Sprintf("%s.%s.%s", baseName, outputLang, ext))
				ops = append(ops, models.Operation{
					Type:        "extract_subtitle",
					StreamIndex: st.StreamIndex,
					OutputPath:  outputPath,
				})
			}
		}
	}

	if len(keepSub) > 0 {
		for _, st := range ep.SubtitleTracks {
			lang := st.Language
			if lang == "" {
				lang = "und"
			}
			if !keepSub[lang] {
				ops = append(ops, models.Operation{
					Type:        "remove_subtitle",
					StreamIndex: st.StreamIndex,
				})
			}
		}
	}

	// External subtitles: embed into media file or delete sidecar
	for _, esf := range ep.ExternalSubtitleFiles {
		lang := esf.Language
		if lang == "" {
			lang = "und"
		}
		if embedExtSub[lang] {
			ops = append(ops, models.Operation{
				Type:       "embed_subtitle",
				SourcePath: esf.Path,
				Language:   esf.Language,
				Forced:     esf.Forced,
				SDH:        esf.SDH,
			})
		} else if deleteExtSub[lang] {
			ops = append(ops, models.Operation{
				Type:       "delete_external_subtitle",
				SourcePath: esf.Path,
			})
		}
	}

	return ops
}

// toLangSet converts a language slice to a lookup map.
func toLangSet(langs []string) map[string]bool {
	s := make(map[string]bool, len(langs))
	for _, l := range langs {
		s[strings.ToLower(strings.TrimSpace(l))] = true
	}
	return s
}

// sortedKeys returns sorted keys of a string bool map.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple sort
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
