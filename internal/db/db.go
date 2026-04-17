package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func Init(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create db directory: %w", err)
	}

	var err error
	DB, err = sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(on)")
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
	}

	for _, m := range migrations {
		if _, err := DB.Exec(m); err != nil {
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, m)
		}
	}

	// Insert default settings if not exists
	_, err := DB.Exec(`INSERT OR IGNORE INTO settings (key, value) VALUES ('preferred_languages', '["eng"]')`)
	return err
}

func Close() {
	if DB != nil {
		DB.Close()
	}
}
