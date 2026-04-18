package handlers

import (
	"net/http"

	"github.com/mirceanton/streamarr/internal/db"
)

func MoviesHandler(w http.ResponseWriter, r *http.Request) {
	needsAttention := r.URL.Query().Get("attention") == "1"

	files, err := db.GetMediaFilesByLibraryType("movies", needsAttention)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	globalSubtitleFormat, _ := db.GetPreferredSubtitleFormat()

	data := map[string]interface{}{
		"Page":                 "movies",
		"Files":                files,
		"NeedsAttention":       needsAttention,
		"GlobalSubtitleFormat": globalSubtitleFormat,
	}
	render(w, "movies.html", data)
}
