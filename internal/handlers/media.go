package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mirceanton/streamarr/internal/db"
	"github.com/mirceanton/streamarr/internal/scanner"
)

func RescanSeriesHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}
	title := r.FormValue("title")
	libRootIDStr := r.FormValue("library_root_id")
	if title == "" || libRootIDStr == "" {
		http.Error(w, "title and library_root_id are required", http.StatusBadRequest)
		return
	}
	libRootID, err := strconv.ParseInt(libRootIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid library_root_id", http.StatusBadRequest)
		return
	}
	go func() {
		if err := scanner.RescanSeries(title, libRootID); err != nil {
			log.Printf("rescan series %q: %v", title, err)
		}
	}()
	w.WriteHeader(http.StatusOK)
}

func MediaDetailHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	mf, err := db.GetMediaFile(id)
	if err != nil {
		http.Error(w, "Media file not found", http.StatusNotFound)
		return
	}

	hasPendingJob, _ := db.HasPendingJob(id)
	globalLangs, _ := db.GetPreferredLanguages()
	globalSubtitleFormat, _ := db.GetPreferredSubtitleFormat()

	data := map[string]interface{}{
		"Page":                 "media",
		"File":                 mf,
		"HasPendingJob":        hasPendingJob,
		"GlobalLanguages":      strings.Join(globalLangs, ", "),
		"GlobalSubtitleFormat": globalSubtitleFormat,
	}
	render(w, "media_detail.html", data)
}

func DeleteMediaFileHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	mf, err := db.GetMediaFile(id)
	if err != nil {
		http.Error(w, "Media file not found", http.StatusNotFound)
		return
	}

	if err := db.DeleteMediaFileByID(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if mf.LibraryType == "movies" {
		w.Header().Set("HX-Redirect", "/movies")
	} else {
		w.Header().Set("HX-Redirect", "/shows")
	}
	w.WriteHeader(http.StatusOK)
}

func RescanFileHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	mf, err := db.GetMediaFile(id)
	if err != nil {
		http.Error(w, "Media file not found", http.StatusNotFound)
		return
	}

	go func() {
		if err := scanner.RescanFile(mf); err != nil {
			log.Printf("rescan file %d: %v", id, err)
		}
	}()

	w.Header().Set("HX-Redirect", fmt.Sprintf("/media/%d", id))
	w.WriteHeader(http.StatusOK)
}
