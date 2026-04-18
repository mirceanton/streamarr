package db

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"time"

	"github.com/mirceanton/streamarr/internal/models"
)

// --- Library Roots ---

func GetLibraryRoots() ([]models.LibraryRoot, error) {
	rows, err := DB.Query(`SELECT id, name, path, type, last_scanned_at, scan_schedule FROM library_roots ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roots []models.LibraryRoot
	for rows.Next() {
		var r models.LibraryRoot
		if err := rows.Scan(&r.ID, &r.Name, &r.Path, &r.Type, &r.LastScannedAt, &r.ScanSchedule); err != nil {
			return nil, err
		}
		roots = append(roots, r)
	}
	return roots, rows.Err()
}

func GetLibraryRoot(id int64) (*models.LibraryRoot, error) {
	var r models.LibraryRoot
	err := DB.QueryRow(`SELECT id, name, path, type, last_scanned_at, scan_schedule FROM library_roots WHERE id = ?`, id).
		Scan(&r.ID, &r.Name, &r.Path, &r.Type, &r.LastScannedAt, &r.ScanSchedule)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func UpdateLibraryScanSchedule(id int64, schedule string) error {
	if schedule == "" {
		_, err := DB.Exec(`UPDATE library_roots SET scan_schedule = NULL WHERE id = ?`, id)
		return err
	}
	_, err := DB.Exec(`UPDATE library_roots SET scan_schedule = ? WHERE id = ?`, schedule, id)
	return err
}

func CreateLibraryRoot(name, path, typ string) (int64, error) {
	res, err := DB.Exec(`INSERT INTO library_roots (name, path, type) VALUES (?, ?, ?)`, name, path, typ)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func DeleteLibraryRoot(id int64) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete tracks for all media files in this library
	_, err = tx.Exec(`DELETE FROM audio_tracks WHERE media_file_id IN (SELECT id FROM media_files WHERE library_root_id = ?)`, id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM subtitle_tracks WHERE media_file_id IN (SELECT id FROM media_files WHERE library_root_id = ?)`, id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM external_subtitle_files WHERE media_file_id IN (SELECT id FROM media_files WHERE library_root_id = ?)`, id)
	if err != nil {
		return err
	}
	// Delete jobs for files in this library
	_, err = tx.Exec(`DELETE FROM jobs WHERE media_file_id IN (SELECT id FROM media_files WHERE library_root_id = ?)`, id)
	if err != nil {
		return err
	}
	// Delete media files
	_, err = tx.Exec(`DELETE FROM media_files WHERE library_root_id = ?`, id)
	if err != nil {
		return err
	}
	// Delete the library root
	_, err = tx.Exec(`DELETE FROM library_roots WHERE id = ?`, id)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func UpdateLibraryScanTime(id int64) error {
	_, err := DB.Exec(`UPDATE library_roots SET last_scanned_at = ? WHERE id = ?`, time.Now(), id)
	return err
}

func GetMediaFileScanTimes(libraryRootID int64) (map[string]time.Time, error) {
	rows, err := DB.Query(`SELECT path, scanned_at FROM media_files WHERE library_root_id = ?`, libraryRootID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]time.Time)
	for rows.Next() {
		var path string
		var scannedAt time.Time
		if err := rows.Scan(&path, &scannedAt); err != nil {
			return nil, err
		}
		result[path] = scannedAt
	}
	return result, rows.Err()
}

// --- Media Files ---

func GetMediaFilesByLibraryType(libType string, needsAttentionOnly bool) ([]models.MediaFile, error) {
	query := `SELECT mf.id, mf.library_root_id, mf.path, mf.filename, mf.title, mf.year,
		mf.season, mf.episode, mf.size_bytes, mf.container, mf.scanned_at, mf.needs_attention,
		(SELECT COUNT(*) FROM audio_tracks WHERE media_file_id = mf.id) as audio_count,
		(SELECT COUNT(*) FROM subtitle_tracks WHERE media_file_id = mf.id) as sub_count,
		lr.type
		FROM media_files mf
		JOIN library_roots lr ON mf.library_root_id = lr.id
		WHERE lr.type = ?`
	if needsAttentionOnly {
		query += ` AND mf.needs_attention = 1`
	}
	query += ` ORDER BY mf.title, mf.season, mf.episode`

	rows, err := DB.Query(query, libType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []models.MediaFile
	for rows.Next() {
		var f models.MediaFile
		var audioCount, subCount int
		if err := rows.Scan(&f.ID, &f.LibraryRootID, &f.Path, &f.Filename, &f.Title, &f.Year,
			&f.Season, &f.Episode, &f.SizeBytes, &f.Container, &f.ScannedAt, &f.NeedsAttention,
			&audioCount, &subCount, &f.LibraryType); err != nil {
			return nil, err
		}
		// Store counts as fake tracks for template use
		f.AudioTracks = make([]models.AudioTrack, audioCount)
		f.SubtitleTracks = make([]models.SubtitleTrack, subCount)
		files = append(files, f)
	}
	return files, rows.Err()
}

func GetMediaFile(id int64) (*models.MediaFile, error) {
	var f models.MediaFile
	err := DB.QueryRow(`SELECT mf.id, mf.library_root_id, mf.path, mf.filename, mf.title, mf.year,
		mf.season, mf.episode, mf.size_bytes, mf.container, mf.scanned_at, mf.needs_attention, lr.type
		FROM media_files mf JOIN library_roots lr ON mf.library_root_id = lr.id
		WHERE mf.id = ?`, id).
		Scan(&f.ID, &f.LibraryRootID, &f.Path, &f.Filename, &f.Title, &f.Year,
			&f.Season, &f.Episode, &f.SizeBytes, &f.Container, &f.ScannedAt, &f.NeedsAttention, &f.LibraryType)
	if err != nil {
		return nil, err
	}

	f.AudioTracks, err = GetAudioTracks(id)
	if err != nil {
		return nil, err
	}
	f.SubtitleTracks, err = GetSubtitleTracks(id)
	if err != nil {
		return nil, err
	}
	f.ExternalSubtitleFiles, err = GetExternalSubtitleFiles(id)
	if err != nil {
		return nil, err
	}

	itemType := "movie"
	itemKey := f.Path
	if f.LibraryType == "shows" {
		itemType = "series"
		itemKey = f.Title
		if itemKey == "" {
			itemKey = "Unknown Series"
		}
	}
	f.LanguageOverride, err = GetLanguageOverride(f.LibraryRootID, itemKey, itemType)
	if err != nil {
		return nil, err
	}

	return &f, nil
}

func GetMediaFileByPath(path string) (*models.MediaFile, error) {
	var f models.MediaFile
	err := DB.QueryRow(`SELECT id, library_root_id, path, filename, title, year,
		season, episode, size_bytes, container, scanned_at, needs_attention
		FROM media_files WHERE path = ?`, path).
		Scan(&f.ID, &f.LibraryRootID, &f.Path, &f.Filename, &f.Title, &f.Year,
			&f.Season, &f.Episode, &f.SizeBytes, &f.Container, &f.ScannedAt, &f.NeedsAttention)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func UpsertMediaFile(f *models.MediaFile) (int64, error) {
	res, err := DB.Exec(`INSERT INTO media_files (library_root_id, path, filename, title, year, season, episode, size_bytes, container, scanned_at, needs_attention)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			filename = excluded.filename,
			title = excluded.title,
			year = excluded.year,
			season = excluded.season,
			episode = excluded.episode,
			size_bytes = excluded.size_bytes,
			container = excluded.container,
			scanned_at = excluded.scanned_at,
			needs_attention = excluded.needs_attention`,
		f.LibraryRootID, f.Path, f.Filename, f.Title, f.Year, f.Season, f.Episode,
		f.SizeBytes, f.Container, f.ScannedAt, f.NeedsAttention)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if id == 0 {
		// Was an update, get the existing ID
		existing, err := GetMediaFileByPath(f.Path)
		if err != nil {
			return 0, err
		}
		id = existing.ID
	}
	return id, nil
}

func DeleteTracksForFile(fileID int64) error {
	_, err := DB.Exec(`DELETE FROM audio_tracks WHERE media_file_id = ?`, fileID)
	if err != nil {
		return err
	}
	_, err = DB.Exec(`DELETE FROM subtitle_tracks WHERE media_file_id = ?`, fileID)
	return err
}

// --- Audio Tracks ---

func GetAudioTracks(mediaFileID int64) ([]models.AudioTrack, error) {
	rows, err := DB.Query(`SELECT id, media_file_id, stream_index, codec, language, title, channels, default_track, forced
		FROM audio_tracks WHERE media_file_id = ? ORDER BY stream_index`, mediaFileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []models.AudioTrack
	for rows.Next() {
		var t models.AudioTrack
		if err := rows.Scan(&t.ID, &t.MediaFileID, &t.StreamIndex, &t.Codec, &t.Language, &t.Title, &t.Channels, &t.DefaultTrack, &t.Forced); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

func InsertAudioTrack(t *models.AudioTrack) error {
	_, err := DB.Exec(`INSERT INTO audio_tracks (media_file_id, stream_index, codec, language, title, channels, default_track, forced)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.MediaFileID, t.StreamIndex, t.Codec, t.Language, t.Title, t.Channels, t.DefaultTrack, t.Forced)
	return err
}

// --- Subtitle Tracks ---

func GetSubtitleTracks(mediaFileID int64) ([]models.SubtitleTrack, error) {
	rows, err := DB.Query(`SELECT id, media_file_id, stream_index, codec, language, title, default_track, forced, sdh
		FROM subtitle_tracks WHERE media_file_id = ? ORDER BY stream_index`, mediaFileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []models.SubtitleTrack
	for rows.Next() {
		var t models.SubtitleTrack
		if err := rows.Scan(&t.ID, &t.MediaFileID, &t.StreamIndex, &t.Codec, &t.Language, &t.Title, &t.DefaultTrack, &t.Forced, &t.SDH); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

func InsertSubtitleTrack(t *models.SubtitleTrack) error {
	_, err := DB.Exec(`INSERT INTO subtitle_tracks (media_file_id, stream_index, codec, language, title, default_track, forced, sdh)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.MediaFileID, t.StreamIndex, t.Codec, t.Language, t.Title, t.DefaultTrack, t.Forced, t.SDH)
	return err
}

// --- External Subtitle Files ---

func GetExternalSubtitleFiles(mediaFileID int64) ([]models.ExternalSubtitleFile, error) {
	rows, err := DB.Query(`SELECT id, media_file_id, path, filename, language, format, forced, sdh
		FROM external_subtitle_files WHERE media_file_id = ? ORDER BY filename`, mediaFileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []models.ExternalSubtitleFile
	for rows.Next() {
		var f models.ExternalSubtitleFile
		if err := rows.Scan(&f.ID, &f.MediaFileID, &f.Path, &f.Filename, &f.Language, &f.Format, &f.Forced, &f.SDH); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func InsertExternalSubtitleFile(f *models.ExternalSubtitleFile) error {
	_, err := DB.Exec(`INSERT INTO external_subtitle_files (media_file_id, path, filename, language, format, forced, sdh)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			media_file_id = excluded.media_file_id,
			filename = excluded.filename,
			language = excluded.language,
			format = excluded.format,
			forced = excluded.forced,
			sdh = excluded.sdh`,
		f.MediaFileID, f.Path, f.Filename, f.Language, f.Format, f.Forced, f.SDH)
	return err
}

func DeleteExternalSubtitleFilesForFile(mediaFileID int64) error {
	_, err := DB.Exec(`DELETE FROM external_subtitle_files WHERE media_file_id = ?`, mediaFileID)
	return err
}

// --- Jobs ---

func CreateJob(mediaFileID int64, operations []models.Operation) (int64, error) {
	opsJSON, err := json.Marshal(operations)
	if err != nil {
		return 0, err
	}

	res, err := DB.Exec(`INSERT INTO jobs (media_file_id, status, operations, created_at) VALUES (?, 'pending', ?, ?)`,
		mediaFileID, string(opsJSON), time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func GetJob(id int64) (*models.Job, error) {
	var j models.Job
	err := DB.QueryRow(`SELECT j.id, j.media_file_id, j.status, j.operations, COALESCE(j.ffmpeg_command,''), COALESCE(j.error,''),
		j.created_at, j.started_at, j.finished_at, mf.filename, mf.path
		FROM jobs j JOIN media_files mf ON j.media_file_id = mf.id
		WHERE j.id = ?`, id).
		Scan(&j.ID, &j.MediaFileID, &j.Status, &j.Operations, &j.FfmpegCommand, &j.Error,
			&j.CreatedAt, &j.StartedAt, &j.FinishedAt, &j.MediaFilename, &j.MediaPath)
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func GetJobs() ([]models.Job, error) {
	rows, err := DB.Query(`SELECT j.id, j.media_file_id, j.status, j.operations, COALESCE(j.ffmpeg_command,''), COALESCE(j.error,''),
		j.created_at, j.started_at, j.finished_at, mf.filename, mf.path
		FROM jobs j JOIN media_files mf ON j.media_file_id = mf.id
		ORDER BY j.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []models.Job
	for rows.Next() {
		var j models.Job
		if err := rows.Scan(&j.ID, &j.MediaFileID, &j.Status, &j.Operations, &j.FfmpegCommand, &j.Error,
			&j.CreatedAt, &j.StartedAt, &j.FinishedAt, &j.MediaFilename, &j.MediaPath); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func UpdateJobStatus(id int64, status string) error {
	now := time.Now()
	switch status {
	case "running":
		_, err := DB.Exec(`UPDATE jobs SET status = ?, started_at = ? WHERE id = ?`, status, now, id)
		return err
	case "done", "failed":
		_, err := DB.Exec(`UPDATE jobs SET status = ?, finished_at = ? WHERE id = ?`, status, now, id)
		return err
	default:
		_, err := DB.Exec(`UPDATE jobs SET status = ? WHERE id = ?`, status, id)
		return err
	}
}

func UpdateJobCommand(id int64, cmd string) error {
	_, err := DB.Exec(`UPDATE jobs SET ffmpeg_command = ? WHERE id = ?`, cmd, id)
	return err
}

func UpdateJobError(id int64, errMsg string) error {
	_, err := DB.Exec(`UPDATE jobs SET error = ? WHERE id = ?`, errMsg, id)
	return err
}

func HasPendingJob(mediaFileID int64) (bool, error) {
	var count int
	err := DB.QueryRow(`SELECT COUNT(*) FROM jobs WHERE media_file_id = ? AND status IN ('pending', 'running')`, mediaFileID).Scan(&count)
	return count > 0, err
}

func GetPendingJobs() ([]models.Job, error) {
	rows, err := DB.Query(`SELECT j.id, j.media_file_id, j.status, j.operations, COALESCE(j.ffmpeg_command,''), COALESCE(j.error,''),
		j.created_at, j.started_at, j.finished_at, mf.filename, mf.path
		FROM jobs j JOIN media_files mf ON j.media_file_id = mf.id
		WHERE j.status = 'pending'
		ORDER BY j.created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []models.Job
	for rows.Next() {
		var j models.Job
		if err := rows.Scan(&j.ID, &j.MediaFileID, &j.Status, &j.Operations, &j.FfmpegCommand, &j.Error,
			&j.CreatedAt, &j.StartedAt, &j.FinishedAt, &j.MediaFilename, &j.MediaPath); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// --- Dashboard ---

func GetDashboardStats() (*models.DashboardStats, error) {
	stats := &models.DashboardStats{}

	err := DB.QueryRow(`
		SELECT
			COUNT(*),
			SUM(CASE WHEN mf.needs_attention = 1 THEN 1 ELSE 0 END)
		FROM media_files mf
		JOIN library_roots lr ON mf.library_root_id = lr.id
		WHERE lr.type = 'movies'`).
		Scan(&stats.TotalMovies, &stats.MoviesNeedAttention)
	if err != nil {
		return nil, err
	}

	err = DB.QueryRow(`
		SELECT
			COUNT(DISTINCT mf.title),
			COUNT(DISTINCT CASE WHEN mf.needs_attention = 1 THEN mf.title END),
			COUNT(*),
			SUM(CASE WHEN mf.needs_attention = 1 THEN 1 ELSE 0 END)
		FROM media_files mf
		JOIN library_roots lr ON mf.library_root_id = lr.id
		WHERE lr.type = 'shows'`).
		Scan(&stats.TotalSeries, &stats.SeriesNeedAttention, &stats.TotalEpisodes, &stats.EpisodesNeedAttention)
	if err != nil {
		return nil, err
	}

	err = DB.QueryRow(`
		SELECT
			COUNT(*),
			SUM(CASE WHEN status = 'running' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END)
		FROM jobs`).
		Scan(&stats.TotalJobs, &stats.RunningJobs, &stats.PendingJobs)
	if err != nil {
		return nil, err
	}

	total := stats.TotalMovies + stats.TotalEpisodes
	attention := stats.MoviesNeedAttention + stats.EpisodesNeedAttention
	if total > 0 {
		stats.HealthPct = (total - attention) * 100 / total
	} else {
		stats.HealthPct = 100
	}

	return stats, nil
}

// --- Settings ---

func GetSetting(key string) (string, error) {
	var value string
	err := DB.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func SetSetting(key, value string) error {
	_, err := DB.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func GetPreferredLanguages() ([]string, error) {
	val, err := GetSetting("preferred_languages")
	if err != nil {
		return []string{"eng"}, err
	}
	if val == "" {
		return []string{"eng"}, nil
	}
	var langs []string
	if err := json.Unmarshal([]byte(val), &langs); err != nil {
		return []string{"eng"}, nil
	}
	return langs, nil
}

func GetParallelJobs() int {
	val, err := GetSetting("parallel_jobs")
	if err != nil || val == "" {
		return 1
	}
	n, err := strconv.Atoi(val)
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// --- Language Overrides ---

// GetLanguageOverride returns the language override for a movie (itemType="movie", itemKey=path)
// or a series (itemType="series", itemKey=title). Returns nil if no override is set.
func GetLanguageOverride(libraryRootID int64, itemKey, itemType string) ([]string, error) {
	var val string
	err := DB.QueryRow(`SELECT preferred_languages FROM language_overrides WHERE library_root_id = ? AND item_key = ? AND item_type = ?`,
		libraryRootID, itemKey, itemType).Scan(&val)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var langs []string
	if err := json.Unmarshal([]byte(val), &langs); err != nil {
		return nil, err
	}
	return langs, nil
}

func SetLanguageOverride(libraryRootID int64, itemKey, itemType string, langs []string) error {
	langsJSON, err := json.Marshal(langs)
	if err != nil {
		return err
	}
	_, err = DB.Exec(`INSERT INTO language_overrides (library_root_id, item_key, item_type, preferred_languages)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(library_root_id, item_key, item_type) DO UPDATE SET preferred_languages = excluded.preferred_languages`,
		libraryRootID, itemKey, itemType, string(langsJSON))
	return err
}

func DeleteLanguageOverride(libraryRootID int64, itemKey, itemType string) error {
	_, err := DB.Exec(`DELETE FROM language_overrides WHERE library_root_id = ? AND item_key = ? AND item_type = ?`,
		libraryRootID, itemKey, itemType)
	return err
}
