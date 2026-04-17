package handlers

import (
	"net/http"
	"sort"

	"github.com/mirceanton/streamarr/internal/db"
	"github.com/mirceanton/streamarr/internal/models"
)

func ShowsHandler(w http.ResponseWriter, r *http.Request) {
	needsAttention := r.URL.Query().Get("attention") == "1"

	files, err := db.GetMediaFilesByLibraryType("shows", needsAttention)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Group by series title
	seriesMap := make(map[string]*models.Series)
	for _, f := range files {
		key := f.Title
		if key == "" {
			key = "Unknown Series"
		}
		s, ok := seriesMap[key]
		if !ok {
			s = &models.Series{
				Title: key,
			}
			seriesMap[key] = s
		}
		s.Episodes = append(s.Episodes, f)
		if f.NeedsAttention {
			s.NeedsAttention = true
		}
	}

	var series []models.Series
	for _, s := range seriesMap {
		sort.Slice(s.Episodes, func(i, j int) bool {
			si, sj := 0, 0
			ei, ej := 0, 0
			if s.Episodes[i].Season != nil {
				si = *s.Episodes[i].Season
			}
			if s.Episodes[j].Season != nil {
				sj = *s.Episodes[j].Season
			}
			if s.Episodes[i].Episode != nil {
				ei = *s.Episodes[i].Episode
			}
			if s.Episodes[j].Episode != nil {
				ej = *s.Episodes[j].Episode
			}
			if si != sj {
				return si < sj
			}
			return ei < ej
		})
		series = append(series, *s)
	}

	sort.Slice(series, func(i, j int) bool {
		return series[i].Title < series[j].Title
	})

	data := map[string]interface{}{
		"Page":           "shows",
		"Series":         series,
		"NeedsAttention": needsAttention,
	}
	render(w, "shows.html", data)
}
