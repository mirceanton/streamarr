package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mirceanton/streamarr/internal/db"
)

func SetMovieLanguageOverrideHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	mf, err := db.GetMediaFile(id)
	if err != nil {
		http.Error(w, "Media file not found", http.StatusNotFound)
		return
	}

	langs := parseLangs(r.FormValue("languages"))
	if err := db.SetLanguageOverride(mf.LibraryRootID, mf.Path, "movie", langs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/media/"+idStr)
	w.WriteHeader(http.StatusOK)
}

func DeleteMovieLanguageOverrideHandler(w http.ResponseWriter, r *http.Request) {
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

	if err := db.DeleteLanguageOverride(mf.LibraryRootID, mf.Path, "movie"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/media/"+idStr)
	w.WriteHeader(http.StatusOK)
}

func SetSeriesLanguageOverrideHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	libraryRootIDStr := r.FormValue("library_root_id")
	libraryRootID, err := strconv.ParseInt(libraryRootIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid library_root_id", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}

	langs := parseLangs(r.FormValue("languages"))
	if err := db.SetLanguageOverride(libraryRootID, title, "series", langs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/shows")
	w.WriteHeader(http.StatusOK)
}

func DeleteSeriesLanguageOverrideHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	libraryRootIDStr := r.FormValue("library_root_id")
	libraryRootID, err := strconv.ParseInt(libraryRootIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid library_root_id", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}

	if err := db.DeleteLanguageOverride(libraryRootID, title, "series"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/shows")
	w.WriteHeader(http.StatusOK)
}

// DeleteSeriesLanguageOverridePostHandler handles POST /api/overrides/series/delete
// (used from HTMX hx-post since hx-delete with body params is less ergonomic).
func DeleteSeriesLanguageOverridePostHandler(w http.ResponseWriter, r *http.Request) {
	DeleteSeriesLanguageOverrideHandler(w, r)
}

func parseLangs(input string) []string {
	var langs []string
	for _, l := range strings.Split(input, ",") {
		l = strings.TrimSpace(strings.ToLower(l))
		if l != "" {
			langs = append(langs, l)
		}
	}
	if len(langs) == 0 {
		langs = []string{"eng"}
	}
	return langs
}
