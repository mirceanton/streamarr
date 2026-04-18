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

func AddMovieLanguageOverrideHandler(w http.ResponseWriter, r *http.Request) {
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

	lang := strings.TrimSpace(strings.ToLower(r.FormValue("language")))
	if lang == "" {
		http.Error(w, "language is required", http.StatusBadRequest)
		return
	}

	mf, err := db.GetMediaFile(id)
	if err != nil {
		http.Error(w, "Media file not found", http.StatusNotFound)
		return
	}

	current := mf.LanguageOverride
	if len(current) == 0 {
		current, _ = db.GetPreferredLanguages()
	}

	for _, l := range current {
		if l == lang {
			w.Header().Set("HX-Redirect", "/media/"+idStr)
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	current = append(current, lang)
	if err := db.SetLanguageOverride(mf.LibraryRootID, mf.Path, "movie", current); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/media/"+idStr)
	w.WriteHeader(http.StatusOK)
}

func AddSeriesLanguageOverrideHandler(w http.ResponseWriter, r *http.Request) {
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

	lang := strings.TrimSpace(strings.ToLower(r.FormValue("language")))
	if lang == "" {
		http.Error(w, "language is required", http.StatusBadRequest)
		return
	}

	current, _ := db.GetLanguageOverride(libraryRootID, title, "series")
	if len(current) == 0 {
		current, _ = db.GetPreferredLanguages()
	}

	for _, l := range current {
		if l == lang {
			redirectAfterSeriesOverride(w, r)
			return
		}
	}

	current = append(current, lang)
	if err := db.SetLanguageOverride(libraryRootID, title, "series", current); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	redirectAfterSeriesOverride(w, r)
}

func redirectAfterSeriesOverride(w http.ResponseWriter, r *http.Request) {
	if mediaFileID := r.FormValue("media_file_id"); mediaFileID != "" {
		w.Header().Set("HX-Redirect", "/media/"+mediaFileID)
	} else {
		w.Header().Set("HX-Redirect", "/shows")
	}
	w.WriteHeader(http.StatusOK)
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
