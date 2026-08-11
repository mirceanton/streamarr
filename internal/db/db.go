package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func Init(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create db directory: %w", err)
	}

	var err error
	DB, err = sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)")
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	if err := DB.Ping(); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	return migrate()
}

func migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS library_roots (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			path TEXT NOT NULL UNIQUE,
			type TEXT NOT NULL,
			last_scanned_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS media_files (
			id INTEGER PRIMARY KEY,
			library_root_id INTEGER NOT NULL REFERENCES library_roots(id),
			path TEXT NOT NULL UNIQUE,
			filename TEXT NOT NULL,
			title TEXT,
			year INTEGER,
			season INTEGER,
			episode INTEGER,
			size_bytes INTEGER,
			container TEXT,
			scanned_at DATETIME NOT NULL,
			needs_attention BOOLEAN DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS audio_tracks (
			id INTEGER PRIMARY KEY,
			media_file_id INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
			stream_index INTEGER NOT NULL,
			codec TEXT,
			language TEXT,
			title TEXT,
			channels INTEGER,
			default_track BOOLEAN,
			forced BOOLEAN
		)`,
		`CREATE TABLE IF NOT EXISTS subtitle_tracks (
			id INTEGER PRIMARY KEY,
			media_file_id INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
			stream_index INTEGER NOT NULL,
			codec TEXT,
			language TEXT,
			title TEXT,
			default_track BOOLEAN,
			forced BOOLEAN,
			sdh BOOLEAN
		)`,
		`CREATE TABLE IF NOT EXISTS jobs (
			id INTEGER PRIMARY KEY,
			media_file_id INTEGER NOT NULL REFERENCES media_files(id),
			status TEXT NOT NULL DEFAULT 'pending',
			operations TEXT NOT NULL,
			ffmpeg_command TEXT,
			error TEXT,
			created_at DATETIME NOT NULL,
			started_at DATETIME,
			finished_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS external_subtitle_files (
			id INTEGER PRIMARY KEY,
			media_file_id INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
			path TEXT NOT NULL UNIQUE,
			filename TEXT NOT NULL,
			language TEXT,
			format TEXT,
			forced BOOLEAN DEFAULT 0,
			sdh BOOLEAN DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS language_overrides (
			id INTEGER PRIMARY KEY,
			library_root_id INTEGER NOT NULL REFERENCES library_roots(id) ON DELETE CASCADE,
			item_key TEXT NOT NULL,
			item_type TEXT NOT NULL CHECK(item_type IN ('movie', 'series', 'episode')),
			preferred_languages TEXT NOT NULL,
			UNIQUE(library_root_id, item_key, item_type)
		)`,
		`CREATE TABLE IF NOT EXISTS subtitle_format_overrides (
			id INTEGER PRIMARY KEY,
			library_root_id INTEGER NOT NULL REFERENCES library_roots(id) ON DELETE CASCADE,
			item_key TEXT NOT NULL,
			item_type TEXT NOT NULL CHECK(item_type IN ('movie', 'series', 'episode')),
			preferred_subtitle_format TEXT NOT NULL,
			UNIQUE(library_root_id, item_key, item_type)
		)`,
		`CREATE TABLE IF NOT EXISTS audio_format_overrides (
			id INTEGER PRIMARY KEY,
			library_root_id INTEGER NOT NULL REFERENCES library_roots(id) ON DELETE CASCADE,
			item_key TEXT NOT NULL,
			item_type TEXT NOT NULL CHECK(item_type IN ('artist', 'album')),
			preferred_audio_format TEXT NOT NULL,
			min_bitrate INTEGER,
			UNIQUE(library_root_id, item_key, item_type)
		)`,
	}

	for _, m := range migrations {
		if _, err := DB.Exec(m); err != nil {
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, m)
		}
	}

	// Insert default settings if not exists
	defaults := []struct{ key, value string }{
		{"preferred_languages", `["eng"]`},
		{"parallel_jobs", "1"},
		{"preferred_subtitle_format", ""},
		{"preferred_audio_format", ""},
		{"preferred_min_bitrate", "0"},
	}
	for _, d := range defaults {
		if _, err := DB.Exec(`INSERT OR IGNORE INTO settings (key, value) VALUES (?, ?)`, d.key, d.value); err != nil {
			return err
		}
	}

	// Add scan_schedule column to library_roots if it doesn't exist (idempotent)
	var scanScheduleColCount int
	DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('library_roots') WHERE name = 'scan_schedule'`).Scan(&scanScheduleColCount)
	if scanScheduleColCount == 0 {
		if _, err := DB.Exec(`ALTER TABLE library_roots ADD COLUMN scan_schedule TEXT`); err != nil {
			return fmt.Errorf("add scan_schedule column: %w", err)
		}
	}

	// Add attention_reasons column to media_files if it doesn't exist (idempotent)
	var attentionReasonsColCount int
	DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('media_files') WHERE name = 'attention_reasons'`).Scan(&attentionReasonsColCount)
	if attentionReasonsColCount == 0 {
		if _, err := DB.Exec(`ALTER TABLE media_files ADD COLUMN attention_reasons TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add attention_reasons column: %w", err)
		}
	}

	// Add warnings_suppressed column to media_files if it doesn't exist (idempotent)
	var warningsSuppressedColCount int
	DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('media_files') WHERE name = 'warnings_suppressed'`).Scan(&warningsSuppressedColCount)
	if warningsSuppressedColCount == 0 {
		if _, err := DB.Exec(`ALTER TABLE media_files ADD COLUMN warnings_suppressed BOOLEAN NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add warnings_suppressed column: %w", err)
		}
	}

	// Add music-specific columns to media_files if they don't exist (idempotent)
	musicCols := []struct {
		name    string
		colType string
	}{
		{"bitrate", "INTEGER"},
		{"sample_rate", "INTEGER"},
		{"bit_depth", "INTEGER"},
		{"audio_codec", "TEXT"},
		{"artist", "TEXT"},
		{"album", "TEXT"},
		{"track_num", "INTEGER"},
	}
	for _, col := range musicCols {
		var count int
		DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('media_files') WHERE name = ?`, col.name).Scan(&count)
		if count == 0 {
			if _, err := DB.Exec(`ALTER TABLE media_files ADD COLUMN ` + col.name + ` ` + col.colType); err != nil {
				return fmt.Errorf("add %s column: %w", col.name, err)
			}
		}
	}

	// Add min_bitrate column to audio_format_overrides if it doesn't exist (idempotent)
	var minBitrateColCount int
	DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('audio_format_overrides') WHERE name = 'min_bitrate'`).Scan(&minBitrateColCount)
	if minBitrateColCount == 0 {
		if _, err := DB.Exec(`ALTER TABLE audio_format_overrides ADD COLUMN min_bitrate INTEGER`); err != nil {
			return fmt.Errorf("add min_bitrate column: %w", err)
		}
	}

	// Widen item_type to allow 'episode' (per-episode overrides) on existing databases.
	if err := migrateAddEpisodeItemType("language_overrides", "preferred_languages TEXT"); err != nil {
		return err
	}
	if err := migrateAddEpisodeItemType("subtitle_format_overrides", "preferred_subtitle_format TEXT"); err != nil {
		return err
	}

	return nil
}

// migrateAddEpisodeItemType recreates an overrides table so its item_type CHECK
// constraint also allows 'episode', preserving existing rows. It is a no-op if
// the table already allows 'episode'.
func migrateAddEpisodeItemType(table, valueColumnDef string) error {
	var schemaSQL string
	if err := DB.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&schemaSQL); err != nil {
		return fmt.Errorf("inspect %s schema: %w", table, err)
	}
	if strings.Contains(schemaSQL, "'episode'") {
		return nil
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	newTable := table + "_new"
	createSQL := fmt.Sprintf(`CREATE TABLE %s (
		id INTEGER PRIMARY KEY,
		library_root_id INTEGER NOT NULL REFERENCES library_roots(id) ON DELETE CASCADE,
		item_key TEXT NOT NULL,
		item_type TEXT NOT NULL CHECK(item_type IN ('movie', 'series', 'episode')),
		%s NOT NULL,
		UNIQUE(library_root_id, item_key, item_type)
	)`, newTable, valueColumnDef)

	if _, err := tx.Exec(createSQL); err != nil {
		return fmt.Errorf("create %s: %w", newTable, err)
	}
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s SELECT * FROM %s`, newTable, table)); err != nil {
		return fmt.Errorf("copy data into %s: %w", newTable, err)
	}
	if _, err := tx.Exec(`DROP TABLE ` + table); err != nil {
		return fmt.Errorf("drop old %s: %w", table, err)
	}
	if _, err := tx.Exec(fmt.Sprintf(`ALTER TABLE %s RENAME TO %s`, newTable, table)); err != nil {
		return fmt.Errorf("rename %s: %w", newTable, err)
	}

	return tx.Commit()
}

func Close() {
	if DB != nil {
		DB.Close()
	}
}
