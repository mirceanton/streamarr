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

// ScanLibrary performs a full scan of a library root, rescanning all media files.
func ScanLibrary(root *models.LibraryRoot) error {
	return runScan(root, false)
}

// ScanLibraryQuick scans only new or modified files in a library root.
// A file is considered new if it is not already in the database.
// A file is considered modified if its modification time is newer than its last scan time.
func ScanLibraryQuick(root *models.LibraryRoot) error {
	return runScan(root, true)
}

func runScan(root *models.LibraryRoot, quickMode bool) error {
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

	// In quick mode, load existing file scan times to skip unchanged files
	var scanTimes map[string]time.Time
	if quickMode {
		var err error
		scanTimes, err = db.GetMediaFileScanTimes(root.ID)
		if err != nil {
			log.Printf("get scan times for library %d: %v", root.ID, err)
			scanTimes = map[string]time.Time{}
		}
	}

	// Collect files to scan
	var files []string
	err := filepath.Walk(root.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !mediaExts[ext] {
			return nil
		}
		if quickMode {
			lastScanned, exists := scanTimes[path]
			if exists && !info.ModTime().After(lastScanned) {
				return nil // unchanged, skip
			}
		}
		files = append(files, path)
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

	// Full scan: remove DB records for files that no longer exist on disk.
	// This preserves job history for files that still exist.
	if !quickMode {
		scanned := make(map[string]bool, len(files))
		for _, f := range files {
			scanned[f] = true
		}
		dbPaths, err := db.GetMediaFilePaths(root.ID)
		if err != nil {
			log.Printf("get media file paths for library %d: %v", root.ID, err)
		} else {
			for path, id := range dbPaths {
				if !scanned[path] {
					if err := db.DeleteMediaFileByID(id); err != nil {
						log.Printf("delete stale record %s: %v", path, err)
					}
				}
			}
		}
	}

	db.UpdateLibraryScanTime(root.ID)
	return nil
}

// RescanSeries re-probes all existing media files for a given series.
func RescanSeries(seriesTitle string, libraryRootID int64) error {
	paths, err := db.GetSeriesFilePaths(seriesTitle, libraryRootID)
	if err != nil {
		return fmt.Errorf("get series file paths: %w", err)
	}
	root, err := db.GetLibraryRoot(libraryRootID)
	if err != nil {
		return fmt.Errorf("get library root: %w", err)
	}
	preferredLangs, _ := db.GetPreferredLanguages()
	for _, path := range paths {
		if err := scanFile(root, path, preferredLangs); err != nil {
			log.Printf("rescan series file %s: %v", path, err)
		}
	}
	return nil
}

// RescanFile re-probes a single media file and updates its database record.
func RescanFile(mf *models.MediaFile) error {
	root, err := db.GetLibraryRoot(mf.LibraryRootID)
	if err != nil {
		return fmt.Errorf("get library root: %w", err)
	}
	preferredLangs, _ := db.GetPreferredLanguages()
	return scanFile(root, mf.Path, preferredLangs)
}

// subtitleCodecToFormat maps ffmpeg codec names to user-friendly format names.
func subtitleCodecToFormat(codec string) string {
	switch strings.ToLower(codec) {
	case "subrip", "mov_text":
		return "srt"
	case "ass", "ssa":
		return "ass"
	case "webvtt":
		return "vtt"
	case "hdmv_pgs_subtitle":
		return "pgs"
	case "dvd_subtitle", "dvdsub":
		return "dvd"
	default:
		return codec
	}
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
	// Resolve effective preferred subtitle format: per-item override takes precedence over global
	effectiveSubtitleFormat, _ := db.GetPreferredSubtitleFormat()
	switch root.Type {
	case "movies":
		if override, _ := db.GetLanguageOverride(root.ID, path, "movie"); len(override) > 0 {
			effectiveLangs = override
		}
		if override, _ := db.GetSubtitleFormatOverride(root.ID, path, "movie"); override != "" {
			effectiveSubtitleFormat = override
		}
	case "shows":
		seriesKey := title
		if seriesKey == "" {
			seriesKey = "Unknown Series"
		}
		if override, _ := db.GetLanguageOverride(root.ID, seriesKey, "series"); len(override) > 0 {
			effectiveLangs = override
		}
		if override, _ := db.GetSubtitleFormatOverride(root.ID, seriesKey, "series"); override != "" {
			effectiveSubtitleFormat = override
		}
	}

	// Probe streams
	audioTracks, subtitleTracks, err := Probe(path)
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}

	needsAttention, attentionReasons := ComputeAttentionReasons(audioTracks, subtitleTracks, effectiveLangs, effectiveSubtitleFormat)

	mf := &models.MediaFile{
		LibraryRootID:    root.ID,
		Path:             path,
		Filename:         filename,
		Title:            title,
		Year:             year,
		Season:           season,
		Episode:          episode,
		SizeBytes:        info.Size(),
		Container:        container,
		ScannedAt:        time.Now(),
		NeedsAttention:   needsAttention,
		AttentionReasons: attentionReasons,
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

// ComputeAttentionReasons returns whether a file needs attention and a human-readable description of why.
func ComputeAttentionReasons(audio []models.AudioTrack, subs []models.SubtitleTrack, preferredLangs []string, preferredSubtitleFormat string) (bool, string) {
	preferred := make(map[string]bool)
	for _, l := range preferredLangs {
		preferred[strings.ToLower(l)] = true
	}
	preferred["und"] = true
	preferred[""] = true

	var audioBad []string
	for _, a := range audio {
		lang := strings.ToLower(a.Language)
		if !preferred[lang] {
			audioBad = append(audioBad, fmt.Sprintf("stream %d (%s)", a.StreamIndex, a.Language))
		}
	}

	subPreferred := make(map[string]bool)
	for _, l := range preferredLangs {
		subPreferred[strings.ToLower(l)] = true
	}
	subPreferred[""] = true

	var subBad []string
	var subFormatBad []string
	for _, s := range subs {
		lang := strings.ToLower(s.Language)
		if !subPreferred[lang] && lang != "und" {
			subBad = append(subBad, fmt.Sprintf("stream %d (%s)", s.StreamIndex, s.Language))
		} else if preferredSubtitleFormat != "" && (subPreferred[lang] || lang == "und") {
			format := subtitleCodecToFormat(s.Codec)
			if format != strings.ToLower(preferredSubtitleFormat) {
				subFormatBad = append(subFormatBad, fmt.Sprintf("stream %d (%s, got %s)", s.StreamIndex, s.Language, format))
			}
		}
	}

	if len(audioBad) == 0 && len(subBad) == 0 && len(subFormatBad) == 0 {
		return false, ""
	}

	var parts []string
	if len(audioBad) > 0 {
		parts = append(parts, "Non-preferred audio: "+strings.Join(audioBad, ", "))
	}
	if len(subBad) > 0 {
		parts = append(parts, "Non-preferred subtitles: "+strings.Join(subBad, ", "))
	}
	if len(subFormatBad) > 0 {
		parts = append(parts, "Non-preferred subtitle format (want "+preferredSubtitleFormat+"): "+strings.Join(subFormatBad, ", "))
	}
	return true, strings.Join(parts, "\n")
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
