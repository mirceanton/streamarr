package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mirceanton/streamarr/internal/db"
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

	data := map[string]interface{}{
		"Page":          "media",
		"File":          mf,
		"HasPendingJob": hasPendingJob,
	}
	render(w, "media_detail.html", data)
}
