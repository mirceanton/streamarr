package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/mirceanton/streamarr/internal/db"
	"github.com/mirceanton/streamarr/internal/handlers"
	"github.com/mirceanton/streamarr/internal/processor"
	"github.com/mirceanton/streamarr/internal/scheduler"
)

func main() {
	port := os.Getenv("STREAMARR_PORT")
	if port == "" {
		port = "8080"
	}

	dbPath := os.Getenv("STREAMARR_CONFIG_PATH")
	if dbPath == "" {
		dbPath = "/config/streamarr.db"
	}

	// Initialize database
	if err := db.Init(dbPath); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Initialize templates
	if err := handlers.InitTemplates(); err != nil {
		log.Fatalf("Failed to initialize templates: %v", err)
	}

	// Start job processor
	processor.Start()

	// Start scan scheduler
	scheduler.Start()

	// Set up router
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Pages
	r.Get("/", handlers.DashboardHandler)
	r.Get("/movies", handlers.MoviesHandler)
	r.Get("/shows", handlers.ShowsHandler)
	r.Get("/shows/{seriesTitle}", handlers.SeriesEpisodesHandler)
	r.Get("/media/{id}", handlers.MediaDetailHandler)
	r.Get("/jobs", handlers.JobsHandler)
	r.Get("/settings", handlers.SettingsHandler)

	// API endpoints
	r.Post("/api/scan/all", handlers.ScanAllHandler)
	r.Get("/api/scan/status", handlers.ScanStatusHandler)
	r.Post("/api/scan/{id}", handlers.ScanLibraryHandler)
	r.Post("/api/jobs", handlers.CreateJobHandler)
	r.Get("/api/jobs/{id}/status", handlers.JobStatusHandler)
	r.Get("/api/jobs/{id}/status-json", handlers.JobStatusJSONHandler)
	r.Post("/api/settings/libraries", handlers.AddLibraryHandler)
	r.Delete("/api/settings/libraries/{id}", handlers.DeleteLibraryHandler)
	r.Post("/api/settings/languages", handlers.UpdateLanguagesHandler)
	r.Post("/api/settings/parallel-jobs", handlers.UpdateParallelJobsHandler)
	r.Post("/api/settings/subtitle-format", handlers.UpdateSubtitleFormatHandler)
	r.Post("/api/settings/libraries/{id}/schedule", handlers.UpdateLibraryScanScheduleHandler)
	r.Delete("/api/media/{id}", handlers.DeleteMediaFileHandler)
	r.Post("/api/media/{id}/rescan", handlers.RescanFileHandler)
	r.Post("/api/series/rescan", handlers.RescanSeriesHandler)
	r.Post("/api/series/delete", handlers.DeleteSeriesHandler)
	r.Post("/api/overrides/movie/{id}", handlers.SetMovieLanguageOverrideHandler)
	r.Delete("/api/overrides/movie/{id}", handlers.DeleteMovieLanguageOverrideHandler)
	r.Post("/api/overrides/movie/{id}/add-lang", handlers.AddMovieLanguageOverrideHandler)
	r.Post("/api/overrides/movie/{id}/subtitle-format", handlers.SetMovieSubtitleFormatOverrideHandler)
	r.Delete("/api/overrides/movie/{id}/subtitle-format", handlers.DeleteMovieSubtitleFormatOverrideHandler)
	r.Post("/api/overrides/series", handlers.SetSeriesLanguageOverrideHandler)
	r.Delete("/api/overrides/series", handlers.DeleteSeriesLanguageOverrideHandler)
	r.Post("/api/overrides/series/delete", handlers.DeleteSeriesLanguageOverridePostHandler)
	r.Post("/api/overrides/series/add-lang", handlers.AddSeriesLanguageOverrideHandler)
	r.Post("/api/overrides/series/subtitle-format", handlers.SetSeriesSubtitleFormatOverrideHandler)
	r.Post("/api/overrides/series/subtitle-format/delete", handlers.DeleteSeriesSubtitleFormatOverridePostHandler)

	// Bulk operations
	r.Get("/api/series/tracks", handlers.GetSeriesTracksHandler)
	r.Post("/api/bulk-jobs/series", handlers.BulkJobsSeriesHandler)

	// Metrics
	r.Handle("/metrics", handlers.MetricsHandler())

	log.Printf("StreamArr starting on port %s", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%s", port), r); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
