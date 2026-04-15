package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mirceanton/streamarr/internal/db"
	"github.com/mirceanton/streamarr/internal/scanner"
)

func SettingsHandler(w http.ResponseWriter, r *http.Request) {
	roots, err := db.GetLibraryRoots()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	langs, _ := db.GetPreferredLanguages()
	scanStatus := scanner.IsScanRunning()

	data := map[string]interface{}{
		"Page":               "settings",
		"LibraryRoots":       roots,
		"PreferredLanguages": strings.Join(langs, ", "),
		"ScanStatus":         scanStatus,
	}
	render(w, "settings.html", data)
}

func AddLibraryHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	path := strings.TrimSpace(r.FormValue("path"))
	libType := r.FormValue("type")

	if name == "" || path == "" {
		http.Error(w, "Name and path are required", http.StatusBadRequest)
		return
	}

	if libType != "movies" && libType != "shows" {
		http.Error(w, "Type must be 'movies' or 'shows'", http.StatusBadRequest)
		return
	}

	_, err := db.CreateLibraryRoot(name, path, libType)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to add library: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/settings")
	w.WriteHeader(http.StatusOK)
}

func DeleteLibraryHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := db.DeleteLibraryRoot(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/settings")
	w.WriteHeader(http.StatusOK)
}

func ScanLibraryHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	root, err := db.GetLibraryRoot(id)
	if err != nil {
		http.Error(w, "Library not found", http.StatusNotFound)
		return
	}

	status := scanner.IsScanRunning()
	if status.Running {
		http.Error(w, "A scan is already in progress", http.StatusConflict)
		return
	}

	go func() {
		if err := scanner.ScanLibrary(root); err != nil {
			log.Printf("scan library %d: %v", id, err)
		}
	}()

	w.Header().Set("HX-Redirect", "/settings")
	w.WriteHeader(http.StatusOK)
}

func ScanAllHandler(w http.ResponseWriter, r *http.Request) {
	status := scanner.IsScanRunning()
	if status.Running {
		http.Error(w, "A scan is already in progress", http.StatusConflict)
		return
	}

	roots, err := db.GetLibraryRoots()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	go func() {
		for _, root := range roots {
			r := root
			if err := scanner.ScanLibrary(&r); err != nil {
				log.Printf("scan library %d: %v", r.ID, err)
			}
		}
	}()

	w.Header().Set("HX-Redirect", "/settings")
	w.WriteHeader(http.StatusOK)
}

func ScanStatusHandler(w http.ResponseWriter, r *http.Request) {
	status := scanner.IsScanRunning()
	renderFragment(w, "scan_status", status)
}

func UpdateLanguagesHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	langsStr := strings.TrimSpace(r.FormValue("languages"))
	var langs []string
	for _, l := range strings.Split(langsStr, ",") {
		l = strings.TrimSpace(strings.ToLower(l))
		if l != "" {
			langs = append(langs, l)
		}
	}

	if len(langs) == 0 {
		langs = []string{"eng"}
	}

	langsJSON, _ := json.Marshal(langs)
	if err := db.SetSetting("preferred_languages", string(langsJSON)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/settings")
	w.WriteHeader(http.StatusOK)
}
