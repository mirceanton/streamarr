package scanner

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mirceanton/streamarr/internal/db"
	"github.com/mirceanton/streamarr/internal/models"
)

var (
	scanMu     sync.Mutex
	ScanStatus = &models.ScanStatus{}

	mediaExts = map[string]bool{
		".mkv": true,
		".mp4": true,
		".avi": true,
		".m4v": true,
		".mov": true,
	}

	// Patterns for parsing title and year from directory/filenames
	titleYearRe = regexp.MustCompile(`^(.+?)\s*[\(\[]?(\d{4})[\)\]]?`)
	// TV show patterns
	seasonEpisodeRe = regexp.MustCompile(`(?i)[Ss](\d+)[Ee](\d+)`)
)

// ScanLibrary scans a library root and populates the database.
func ScanLibrary(root *models.LibraryRoot) error {
	scanMu.Lock()
	if ScanStatus.Running {
		scanMu.Unlock()
		return fmt.Errorf("scan already in progress")
	}
	ScanStatus.Running = true
	ScanStatus.LibraryID = root.ID
	ScanStatus.Done = 0
	ScanStatus.Total = 0
	ScanStatus.Current = ""
	scanMu.Unlock()

	defer func() {
		scanMu.Lock()
		ScanStatus.Running = false
		scanMu.Unlock()
	}()

	// Collect all media files first
	var files []string
	err := filepath.Walk(root.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if mediaExts[ext] {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk directory: %w", err)
	}

	scanMu.Lock()
	ScanStatus.Total = len(files)
	scanMu.Unlock()

	preferredLangs, _ := db.GetPreferredLanguages()

	for _, path := range files {
		scanMu.Lock()
		ScanStatus.Current = filepath.Base(path)
		scanMu.Unlock()

		if err := scanFile(root, path, preferredLangs); err != nil {
			log.Printf("scan file %s: %v", path, err)
		}

		scanMu.Lock()
		ScanStatus.Done++
		scanMu.Unlock()
	}

	db.UpdateLibraryScanTime(root.ID)
	return nil
}

func scanFile(root *models.LibraryRoot, path string, preferredLangs []string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	filename := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(filename))
	container := strings.TrimPrefix(ext, ".")

	title, year := parseTitle(path, root.Type)
	var season, episode *int
	if root.Type == "shows" {
		s, e := parseSeasonEpisode(filename)
		if s > 0 {
			season = &s
			episode = &e
		}
	}

	// Resolve effective preferred languages: per-item override takes precedence over global
	effectiveLangs := preferredLangs
	switch root.Type {
	case "movies":
		if override, _ := db.GetLanguageOverride(root.ID, path, "movie"); len(override) > 0 {
			effectiveLangs = override
		}
	case "shows":
		seriesKey := title
		if seriesKey == "" {
			seriesKey = "Unknown Series"
		}
		if override, _ := db.GetLanguageOverride(root.ID, seriesKey, "series"); len(override) > 0 {
			effectiveLangs = override
		}
	}

	// Probe streams
	audioTracks, subtitleTracks, err := Probe(path)
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}

	needsAttention := checkNeedsAttention(audioTracks, subtitleTracks, effectiveLangs)

	mf := &models.MediaFile{
		LibraryRootID:  root.ID,
		Path:           path,
		Filename:       filename,
		Title:          title,
		Year:           year,
		Season:         season,
		Episode:        episode,
		SizeBytes:      info.Size(),
		Container:      container,
		ScannedAt:      time.Now(),
		NeedsAttention: needsAttention,
	}

	fileID, err := db.UpsertMediaFile(mf)
	if err != nil {
		return fmt.Errorf("upsert media file: %w", err)
	}

	// Replace tracks
	if err := db.DeleteTracksForFile(fileID); err != nil {
		return fmt.Errorf("delete old tracks: %w", err)
	}

	for _, t := range audioTracks {
		t.MediaFileID = fileID
		if err := db.InsertAudioTrack(&t); err != nil {
			return fmt.Errorf("insert audio track: %w", err)
		}
	}

	for _, t := range subtitleTracks {
		t.MediaFileID = fileID
		if err := db.InsertSubtitleTrack(&t); err != nil {
			return fmt.Errorf("insert subtitle track: %w", err)
		}
	}

	mf.ID = fileID
	if err := ScanExternalSubtitles(mf); err != nil {
		log.Printf("scan external subtitles for %s: %v", path, err)
	}

	return nil
}

func parseTitle(path, libType string) (string, int) {
	// For movies: use parent directory name
	// For shows: use grandparent (series) directory name
	dir := filepath.Dir(path)
	name := filepath.Base(dir)

	if libType == "shows" {
		// Go up one more level to get series name (skip season folder)
		parent := filepath.Dir(dir)
		seriesName := filepath.Base(parent)
		// Check if current dir looks like a season folder
		if isSeasonFolder(name) {
			name = seriesName
		}
	}

	matches := titleYearRe.FindStringSubmatch(name)
	if len(matches) >= 3 {
		title := strings.TrimSpace(matches[1])
		// Clean up common separators
		title = strings.ReplaceAll(title, ".", " ")
		title = strings.ReplaceAll(title, "_", " ")
		year, _ := strconv.Atoi(matches[2])
		return title, year
	}

	// No year found, just clean up the name
	title := strings.ReplaceAll(name, ".", " ")
	title = strings.ReplaceAll(title, "_", " ")
	return strings.TrimSpace(title), 0
}

func isSeasonFolder(name string) bool {
	lower := strings.ToLower(name)
	matched, _ := regexp.MatchString(`^(season\s*\d+|s\d+|specials)$`, lower)
	return matched
}

func parseSeasonEpisode(filename string) (int, int) {
	matches := seasonEpisodeRe.FindStringSubmatch(filename)
	if len(matches) >= 3 {
		season, _ := strconv.Atoi(matches[1])
		episode, _ := strconv.Atoi(matches[2])
		return season, episode
	}
	return 0, 0
}

func checkNeedsAttention(audio []models.AudioTrack, subs []models.SubtitleTrack, preferredLangs []string) bool {
	preferred := make(map[string]bool)
	for _, l := range preferredLangs {
		preferred[strings.ToLower(l)] = true
	}
	// Also always allow "und" (undefined) for audio
	preferred["und"] = true
	preferred[""] = true

	for _, a := range audio {
		lang := strings.ToLower(a.Language)
		if !preferred[lang] {
			return true
		}
	}

	// For subtitles, "und" is not automatically allowed
	subPreferred := make(map[string]bool)
	for _, l := range preferredLangs {
		subPreferred[strings.ToLower(l)] = true
	}
	subPreferred[""] = true

	for _, s := range subs {
		lang := strings.ToLower(s.Language)
		if !subPreferred[lang] && lang != "und" {
			return true
		}
	}

	return false
}

// subtitleFlagWords are parts of an external subtitle filename that indicate flags, not a language code.
var subtitleFlagWords = map[string]bool{
	"forced":  true,
	"sdh":     true,
	"hi":      true,
	"cc":      true,
	"default": true,
}

// ScanExternalSubtitles detects subtitle sidecar files in the same directory as mf and stores them in the DB.
func ScanExternalSubtitles(mf *models.MediaFile) error {
	dir := filepath.Dir(mf.Path)
	basename := strings.TrimSuffix(mf.Filename, filepath.Ext(mf.Filename))

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read directory: %w", err)
	}

	if err := db.DeleteExternalSubtitleFilesForFile(mf.ID); err != nil {
		return fmt.Errorf("delete old external subtitle files: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if !IsExternalSubtitleExt(ext) {
			continue
		}
		// The file's name without its extension must equal the basename or start with basename+"."
		inner := strings.TrimSuffix(name, filepath.Ext(name))
		if inner != basename && !strings.HasPrefix(inner, basename+".") {
			continue
		}
		suffix := strings.TrimPrefix(inner, basename)
		suffix = strings.TrimPrefix(suffix, ".")

		lang, forced, sdh := parseExternalSubSuffix(suffix)
		format := strings.TrimPrefix(ext, ".")

		esf := &models.ExternalSubtitleFile{
			MediaFileID: mf.ID,
			Path:        filepath.Join(dir, name),
			Filename:    name,
			Language:    lang,
			Format:      format,
			Forced:      forced,
			SDH:         sdh,
		}
		if err := db.InsertExternalSubtitleFile(esf); err != nil {
			log.Printf("insert external subtitle %s: %v", name, err)
		}
	}
	return nil
}

// parseExternalSubSuffix parses the part of an external subtitle filename between the media basename
// and the file extension (e.g. "eng.forced" → lang="eng", forced=true).
func parseExternalSubSuffix(suffix string) (lang string, forced, sdh bool) {
	if suffix == "" {
		return
	}
	for _, p := range strings.Split(strings.ToLower(suffix), ".") {
		if p == "" {
			continue
		}
		switch p {
		case "forced":
			forced = true
		case "sdh", "hi", "cc":
			sdh = true
		default:
			if (len(p) == 2 || len(p) == 3) && !subtitleFlagWords[p] {
				lang = p
			}
		}
	}
	return
}

// IsScanRunning returns current scan status.
func IsScanRunning() *models.ScanStatus {
	scanMu.Lock()
	defer scanMu.Unlock()
	status := *ScanStatus
	return &status
}
