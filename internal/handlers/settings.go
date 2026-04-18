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
	"github.com/mirceanton/streamarr/internal/scheduler"
)

func SettingsHandler(w http.ResponseWriter, r *http.Request) {
	roots, err := db.GetLibraryRoots()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	langs, _ := db.GetPreferredLanguages()
	scanStatus := scanner.IsScanRunning()
	parallelJobs := db.GetParallelJobs()
	preferredSubtitleFormat, _ := db.GetPreferredSubtitleFormat()

	data := map[string]interface{}{
		"Page":                    "settings",
		"LibraryRoots":            roots,
		"PreferredLanguages":      strings.Join(langs, ", "),
		"ScanStatus":              scanStatus,
		"ParallelJobs":            parallelJobs,
		"PreferredSubtitleFormat": preferredSubtitleFormat,
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

	r.ParseForm()
	scanType := r.FormValue("scan_type")

	go func() {
		var err error
		if scanType == "quick" {
			err = scanner.ScanLibraryQuick(root)
		} else {
			err = scanner.ScanLibrary(root)
		}
		if err != nil {
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

	r.ParseForm()
	scanType := r.FormValue("scan_type")

	go func() {
		for _, root := range roots {
			r := root
			var err error
			if scanType == "quick" {
				err = scanner.ScanLibraryQuick(&r)
			} else {
				err = scanner.ScanLibrary(&r)
			}
			if err != nil {
				log.Printf("scan library %d: %v", r.ID, err)
			}
		}
	}()

	w.Header().Set("HX-Redirect", "/settings")
	w.WriteHeader(http.StatusOK)
}

func UpdateLibraryScanScheduleHandler(w http.ResponseWriter, r *http.Request) {
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

	schedule := strings.TrimSpace(r.FormValue("schedule"))

	if schedule != "" {
		if err := scheduler.ValidateSchedule(schedule); err != nil {
			http.Error(w, fmt.Sprintf("Invalid cron expression: %v", err), http.StatusBadRequest)
			return
		}
	}

	if err := db.UpdateLibraryScanSchedule(id, schedule); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := scheduler.UpdateSchedule(id, schedule); err != nil {
		log.Printf("update scheduler for library %d: %v", id, err)
	}

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

func UpdateParallelJobsHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	nStr := strings.TrimSpace(r.FormValue("parallel_jobs"))
	n, err := strconv.Atoi(nStr)
	if err != nil || n < 1 {
		http.Error(w, "parallel_jobs must be a positive integer", http.StatusBadRequest)
		return
	}

	if err := db.SetSetting("parallel_jobs", strconv.Itoa(n)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/settings")
	w.WriteHeader(http.StatusOK)
}

var validSubtitleFormats = map[string]bool{
	"":    true,
	"srt": true,
	"ass": true,
	"vtt": true,
	"pgs": true,
	"dvd": true,
}

func UpdateSubtitleFormatHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	format := strings.TrimSpace(strings.ToLower(r.FormValue("subtitle_format")))
	if !validSubtitleFormats[format] {
		http.Error(w, "Invalid subtitle format", http.StatusBadRequest)
		return
	}

	if err := db.SetSetting("preferred_subtitle_format", format); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/settings")
	w.WriteHeader(http.StatusOK)
}
