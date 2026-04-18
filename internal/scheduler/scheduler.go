package scheduler

import (
	"log"
	"sync"

	"github.com/mirceanton/streamarr/internal/db"
	"github.com/mirceanton/streamarr/internal/scanner"
	"github.com/robfig/cron/v3"
)

var (
	c       *cron.Cron
	mu      sync.Mutex
	entries = make(map[int64]cron.EntryID)
)

// Start initialises the cron scheduler and loads persisted schedules from the database.
func Start() {
	c = cron.New()
	roots, err := db.GetLibraryRoots()
	if err != nil {
		log.Printf("scheduler: failed to load libraries: %v", err)
	} else {
		for _, root := range roots {
			r := root
			if r.ScanSchedule != nil && *r.ScanSchedule != "" {
				if err := addEntry(r.ID, *r.ScanSchedule); err != nil {
					log.Printf("scheduler: failed to schedule library %d (%s): %v", r.ID, r.Name, err)
				}
			}
		}
	}
	c.Start()
	log.Printf("scheduler: started with %d scheduled libraries", len(entries))
}

// UpdateSchedule replaces the cron schedule for a library. Pass an empty string to disable.
func UpdateSchedule(libraryID int64, schedule string) error {
	mu.Lock()
	defer mu.Unlock()

	if entryID, ok := entries[libraryID]; ok {
		c.Remove(entryID)
		delete(entries, libraryID)
	}

	if schedule == "" {
		return nil
	}

	return addEntry(libraryID, schedule)
}

// ValidateSchedule checks whether schedule is a valid 5-field cron expression.
func ValidateSchedule(schedule string) error {
	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := p.Parse(schedule)
	return err
}

func addEntry(libraryID int64, schedule string) error {
	entryID, err := c.AddFunc(schedule, func() {
		root, err := db.GetLibraryRoot(libraryID)
		if err != nil {
			log.Printf("scheduler: library %d not found: %v", libraryID, err)
			return
		}
		log.Printf("scheduler: starting scheduled full scan for library %s", root.Name)
		if err := scanner.ScanLibrary(root); err != nil {
			log.Printf("scheduler: scan library %d: %v", libraryID, err)
		}
	})
	if err != nil {
		return err
	}
	entries[libraryID] = entryID
	return nil
}
