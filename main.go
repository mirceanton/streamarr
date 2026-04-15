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

	// Set up router
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Pages
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/movies", http.StatusFound)
	})
	r.Get("/movies", handlers.MoviesHandler)
	r.Get("/shows", handlers.ShowsHandler)
	r.Get("/media/{id}", handlers.MediaDetailHandler)
	r.Get("/jobs", handlers.JobsHandler)
	r.Get("/settings", handlers.SettingsHandler)

	// API endpoints
	r.Post("/api/scan/all", handlers.ScanAllHandler)
	r.Get("/api/scan/status", handlers.ScanStatusHandler)
	r.Post("/api/scan/{id}", handlers.ScanLibraryHandler)
	r.Post("/api/jobs", handlers.CreateJobHandler)
	r.Get("/api/jobs/{id}/status", handlers.JobStatusHandler)
	r.Post("/api/settings/libraries", handlers.AddLibraryHandler)
	r.Delete("/api/settings/libraries/{id}", handlers.DeleteLibraryHandler)
	r.Post("/api/settings/languages", handlers.UpdateLanguagesHandler)

	log.Printf("StreamArr starting on port %s", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%s", port), r); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
