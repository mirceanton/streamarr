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

	data := map[string]interface{}{
		"Page":            "media",
		"File":            mf,
		"HasPendingJob":   hasPendingJob,
		"GlobalLanguages": strings.Join(globalLangs, ", "),
	}
	render(w, "media_detail.html", data)
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
