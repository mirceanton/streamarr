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

// SubtitleTrackInfo describes a distinct subtitle track by language and format.
type SubtitleTrackInfo struct {
	Language string `json:"language"`
	Format   string `json:"format"` // codec for embedded tracks; file extension for external sidecar files
	IsImage  bool   `json:"is_image"`
}

// TrackFilter selects a subtitle track by language and format.
type TrackFilter struct {
	Language string `json:"language"`
	Format   string `json:"format"`
}

// SeriesTracksResponse is returned by GetSeriesTracksHandler.
type SeriesTracksResponse struct {
	EpisodeCount           int                 `json:"episode_count"`
	AudioLanguages         []string            `json:"audio_languages"`
	SubtitleTracks         []SubtitleTrackInfo `json:"subtitle_tracks"`
	ExternalSubtitleTracks []SubtitleTrackInfo `json:"external_subtitle_tracks"`
}

// GetSeriesTracksHandler returns aggregate track language+format info for all episodes in a series.
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

	type trackKey struct{ lang, format string }

	audioLangSet := make(map[string]bool)
	subTrackSet := make(map[trackKey]bool)
	extSubTrackSet := make(map[trackKey]bool)

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
			subTrackSet[trackKey{lang, st.Codec}] = true
		}
		for _, esf := range ep.ExternalSubtitleFiles {
			lang := esf.Language
			if lang == "" {
				lang = "und"
			}
			extSubTrackSet[trackKey{lang, esf.Format}] = true
		}
	}

	audioLangs := sortedKeys(audioLangSet)

	var subTracks []SubtitleTrackInfo
	for k := range subTrackSet {
		subTracks = append(subTracks, SubtitleTrackInfo{
			Language: k.lang,
			Format:   k.format,
			IsImage:  scanner.IsImageBasedSubtitle(k.format),
		})
	}
	sortSubtitleTracks(subTracks)

	var extSubTracks []SubtitleTrackInfo
	for k := range extSubTrackSet {
		extSubTracks = append(extSubTracks, SubtitleTrackInfo{
			Language: k.lang,
			Format:   k.format,
		})
	}
	sortSubtitleTracks(extSubTracks)

	resp := SeriesTracksResponse{
		EpisodeCount:           len(episodes),
		AudioLanguages:         audioLangs,
		SubtitleTracks:         subTracks,
		ExternalSubtitleTracks: extSubTracks,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// BulkJobRequest is the JSON body for POST /api/bulk-jobs/series.
type BulkJobRequest struct {
	SeriesTitle                  string        `json:"series_title"`
	LibraryRootID                int64         `json:"library_root_id"`
	KeepAudioLanguages           []string      `json:"keep_audio_languages"`
	KeepSubtitleTracks           []TrackFilter `json:"keep_subtitle_tracks"`
	ExtractSubtitleTracks        []TrackFilter `json:"extract_subtitle_tracks"`
	EmbedExternalSubtitleTracks  []TrackFilter `json:"embed_external_subtitle_tracks"`
	DeleteExternalSubtitleTracks []TrackFilter `json:"delete_external_subtitle_tracks"`
	// SetAudioLanguage, when non-empty, sets the language tag on the single undefined audio track.
	// Only applies to episodes that have exactly one audio track with an undefined language.
	SetAudioLanguage string `json:"set_audio_language"`
}

// BulkJobsSeriesHandler creates one job per episode in a series based on language+format-level operations.
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
	keepSub := toTrackSet(req.KeepSubtitleTracks)
	extractSub := toTrackSet(req.ExtractSubtitleTracks)
	embedExtSub := toTrackSet(req.EmbedExternalSubtitleTracks)
	deleteExtSub := toTrackSet(req.DeleteExternalSubtitleTracks)

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

		ops, skipReason := buildEpisodeOps(ep, keepAudio, keepSub, extractSub, embedExtSub, deleteExtSub, req.SetAudioLanguage)
		if len(ops) == 0 {
			results = append(results, result{Filename: ep.Filename, Skipped: true, Reason: skipReason})
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

// buildEpisodeOps generates the operations for a single episode given language+format keep/extract sets.
// Returns the list of operations and, when the list is empty, a human-readable reason explaining why.
func buildEpisodeOps(ep models.MediaFile, keepAudio, keepSub, extractSub, embedExtSub, deleteExtSub map[string]bool, setAudioLang string) ([]models.Operation, string) {
	var ops []models.Operation
	var skipReasons []string

	// Set audio language: only for episodes with exactly 1 audio track that has an undefined language.
	if setAudioLang != "" {
		if len(ep.AudioTracks) == 1 {
			at := ep.AudioTracks[0]
			lang := at.Language
			if lang == "" {
				lang = "und"
			}
			if lang == "und" {
				ops = append(ops, models.Operation{
					Type:        "set_language",
					StreamIndex: at.StreamIndex,
					Language:    setAudioLang,
				})
			} else {
				skipReasons = append(skipReasons, "audio language not set: the single audio track already has language '"+at.Language+"'")
			}
		} else if len(ep.AudioTracks) == 0 {
			skipReasons = append(skipReasons, "audio language not set: no audio tracks found")
		} else {
			skipReasons = append(skipReasons, fmt.Sprintf("audio language not set: episode has %d audio tracks (only episodes with a single undefined track are supported)", len(ep.AudioTracks)))
		}
	}

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
		remainingAudio := len(ep.AudioTracks) - removeAudioCount
		if remainingAudio < 1 && removeAudioCount > 0 {
			// All audio tracks would be removed — skip to preserve at least one track
			skipReasons = append(skipReasons, "audio removal skipped: removing non-preferred tracks would leave no audio stream")
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

	// Embedded subtitles: extract (if requested), then remove (if not in keepSub).
	// Matching is by "lang:codec" key, supporting distinct handling per format.
	if len(extractSub) > 0 {
		for _, st := range ep.SubtitleTracks {
			lang := st.Language
			if lang == "" {
				lang = "und"
			}
			trackKey := lang + ":" + strings.ToLower(st.Codec)
			if extractSub[trackKey] {
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
			trackKey := lang + ":" + strings.ToLower(st.Codec)
			if !keepSub[trackKey] {
				ops = append(ops, models.Operation{
					Type:        "remove_subtitle",
					StreamIndex: st.StreamIndex,
				})
			}
		}
	}

	// External subtitles: embed into media file or delete sidecar.
	// Matching is by "lang:format" key.
	for _, esf := range ep.ExternalSubtitleFiles {
		lang := esf.Language
		if lang == "" {
			lang = "und"
		}
		extKey := lang + ":" + strings.ToLower(esf.Format)
		if embedExtSub[extKey] {
			ops = append(ops, models.Operation{
				Type:       "embed_subtitle",
				SourcePath: esf.Path,
				Language:   esf.Language,
				Forced:     esf.Forced,
				SDH:        esf.SDH,
			})
		} else if deleteExtSub[extKey] {
			ops = append(ops, models.Operation{
				Type:       "delete_external_subtitle",
				SourcePath: esf.Path,
			})
		}
	}

	if len(ops) == 0 {
		if len(skipReasons) > 0 {
			return ops, strings.Join(skipReasons, "; ")
		}
		return ops, "no operations needed: episode already matches requested configuration"
	}
	return ops, ""
}

// toTrackSet converts a TrackFilter slice to a lookup map keyed by "lang:format".
func toTrackSet(tracks []TrackFilter) map[string]bool {
	s := make(map[string]bool, len(tracks))
	for _, t := range tracks {
		lang := strings.ToLower(strings.TrimSpace(t.Language))
		format := strings.ToLower(strings.TrimSpace(t.Format))
		s[lang+":"+format] = true
	}
	return s
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
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

// sortSubtitleTracks sorts a SubtitleTrackInfo slice by language then format.
func sortSubtitleTracks(tracks []SubtitleTrackInfo) {
	for i := 0; i < len(tracks); i++ {
		for j := i + 1; j < len(tracks); j++ {
			ki := tracks[i].Language + ":" + tracks[i].Format
			kj := tracks[j].Language + ":" + tracks[j].Format
			if ki > kj {
				tracks[i], tracks[j] = tracks[j], tracks[i]
			}
		}
	}
}
