package models

import "time"

// LibraryRoot represents a configured media folder.
type LibraryRoot struct {
	ID            int64
	Name          string
	Path          string
	Type          string // "movies" or "shows"
	LastScannedAt *time.Time
}

// MediaFile represents a single media file on disk.
type MediaFile struct {
	ID             int64
	LibraryRootID  int64
	Path           string
	Filename       string
	Title          string
	Year           int
	Season         *int // nil for movies
	Episode        *int // nil for movies
	SizeBytes      int64
	Container      string
	ScannedAt      time.Time
	NeedsAttention bool

	// Joined fields (not always populated)
	AudioTracks    []AudioTrack
	SubtitleTracks []SubtitleTrack
	LibraryType    string // from library_roots.type
}

// AudioTrack represents an audio stream in a media file.
type AudioTrack struct {
	ID           int64
	MediaFileID  int64
	StreamIndex  int
	Codec        string
	Language     string
	Title        string
	Channels     int
	DefaultTrack bool
	Forced       bool
}

// SubtitleTrack represents a subtitle stream in a media file.
type SubtitleTrack struct {
	ID           int64
	MediaFileID  int64
	StreamIndex  int
	Codec        string
	Language     string
	Title        string
	DefaultTrack bool
	Forced       bool
	SDH          bool
}

// Job represents a processing job.
type Job struct {
	ID            int64
	MediaFileID   int64
	Status        string // pending, running, done, failed
	Operations    string // JSON array
	FfmpegCommand string
	Error         string
	CreatedAt     time.Time
	StartedAt     *time.Time
	FinishedAt    *time.Time

	// Joined field
	MediaFilename string
	MediaPath     string
}

// Operation represents a single action within a job.
type Operation struct {
	Type        string `json:"type"` // remove_audio, remove_subtitle, extract_subtitle
	StreamIndex int    `json:"stream_index"`
	OutputPath  string `json:"output_path,omitempty"`
}

// Series groups episodes for the shows view.
type Series struct {
	Title          string
	Path           string
	Episodes       []MediaFile
	NeedsAttention bool
}

// ScanStatus tracks whether a scan is in progress.
type ScanStatus struct {
	Running   bool
	LibraryID int64
	Current   string
	Total     int
	Done      int
}
