package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mirceanton/streamarr/internal/db"
	"github.com/mirceanton/streamarr/internal/models"
	"github.com/mirceanton/streamarr/internal/processor"
	"github.com/mirceanton/streamarr/internal/scanner"
)

func JobsHandler(w http.ResponseWriter, r *http.Request) {
	jobs, err := db.GetJobs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Page": "jobs",
		"Jobs": jobs,
	}
	render(w, "jobs.html", data)
}

func CreateJobHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	mediaFileIDStr := r.FormValue("media_file_id")
	mediaFileID, err := strconv.ParseInt(mediaFileIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid media file ID", http.StatusBadRequest)
		return
	}

	// Check for pending jobs
	hasPending, err := db.HasPendingJob(mediaFileID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if hasPending {
		http.Error(w, "File already has a pending or running job", http.StatusConflict)
		return
	}

	opsJSON := r.FormValue("operations")
	var ops []models.Operation
	if err := json.Unmarshal([]byte(opsJSON), &ops); err != nil {
		http.Error(w, "Invalid operations JSON", http.StatusBadRequest)
		return
	}

	if len(ops) == 0 {
		http.Error(w, "No operations specified", http.StatusBadRequest)
		return
	}

	// Get media file for validation and path info
	mf, err := db.GetMediaFile(mediaFileID)
	if err != nil {
		http.Error(w, "Media file not found", http.StatusNotFound)
		return
	}

	// Validate: don't remove all audio tracks
	audioRemoveCount := 0
	for _, op := range ops {
		if op.Type == "remove_audio" {
			audioRemoveCount++
		}
	}
	if audioRemoveCount >= len(mf.AudioTracks) {
		http.Error(w, "Cannot remove all audio tracks", http.StatusBadRequest)
		return
	}

	// Fill in output paths for extract operations
	for i, op := range ops {
		if op.Type == "extract_subtitle" && op.OutputPath == "" {
			// Find the subtitle track to determine language and codec
			for _, st := range mf.SubtitleTracks {
				if st.StreamIndex == op.StreamIndex {
					ext := scanner.SubtitleExtension(st.Codec)
					lang := st.Language
					if lang == "" {
						lang = "und"
					}
					baseName := strings.TrimSuffix(mf.Filename, filepath.Ext(mf.Filename))
					ops[i].OutputPath = filepath.Join(filepath.Dir(mf.Path), fmt.Sprintf("%s.%s.%s", baseName, lang, ext))
					break
				}
			}
		}
	}

	jobID, err := db.CreateJob(mediaFileID, ops)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	processor.Enqueue(jobID)

	// Redirect back to media detail, passing job ID so the page can track it
	redirectURL := fmt.Sprintf("/media/%d?new_job=%d", mediaFileID, jobID)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", redirectURL)
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
	}
}

func JobStatusJSONHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	job, err := db.GetJob(id)
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":   job.Status,
		"filename": job.MediaFilename,
	})
}

func JobStatusHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	job, err := db.GetJob(id)
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	renderFragment(w, "job_status", job)
}
