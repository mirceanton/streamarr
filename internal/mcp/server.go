// Package mcp exposes a Model Context Protocol server so AI agents can
// inspect which media files need attention, trigger jobs that modify files
// to fix them, or set per-item preferences so the existing file already
// matches the criteria (no job required).
package mcp

import (
	"net/http"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewHTTPHandler builds the streamarr MCP server and returns an http.Handler
// that serves it over the streamable HTTP transport.
func NewHTTPHandler() http.Handler {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "streamarr", Version: "1.0.0"}, nil)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "list_attention_media",
		Description: "List media files that currently need attention (e.g. wrong audio/subtitle languages or subtitle format), optionally filtered by library type (movies, shows, music).",
	}, listAttentionMedia)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "get_media_attention_reasons",
		Description: "Get the specific reasons a single media file needs attention, including any external subtitle sidecar files (e.g. .srt files next to the media file) found on disk.",
	}, getAttentionReasons)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "trigger_track_job",
		Description: "Trigger a background job that removes audio/subtitle tracks, extracts subtitle tracks, or deletes external subtitle sidecar files from a media file, in order to clear its needs-attention status.",
	}, triggerTrackJob)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "run_ffprobe",
		Description: "Run ffprobe on a media file and return its raw JSON output (streams and format), for detailed inspection beyond the summarized attention reasons.",
	}, runFFprobe)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "get_job_status",
		Description: "Check the status of a job triggered by trigger_track_job (pending, running, done, or failed), including the failure reason if it failed. Jobs run in the background, so poll this until the status is done or failed before re-checking attention reasons or ffprobe output.",
	}, getJobStatus)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "set_language_preference",
		Description: "Set the preferred audio/subtitle languages for a movie or show, without modifying the file. Files whose tracks already match the new preference will immediately stop needing attention. For shows, defaults to just this episode; pass scope=series to apply to the whole series.",
	}, setLanguagePreference)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "clear_language_preference",
		Description: "Remove a previously set language preference for a movie or show, reverting it to the series-wide or global default.",
	}, clearLanguagePreference)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "set_subtitle_format_preference",
		Description: "Set the preferred subtitle format for a movie or show, without modifying the file. Files whose subtitles already match the new preference will immediately stop needing attention. For shows, defaults to just this episode; pass scope=series to apply to the whole series.",
	}, setSubtitleFormatPreference)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "clear_subtitle_format_preference",
		Description: "Remove a previously set subtitle format preference for a movie or show, reverting it to the series-wide or global default.",
	}, clearSubtitleFormatPreference)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "set_audio_preference",
		Description: "Set the preferred audio format and/or minimum bitrate for a music track's album, without modifying the file. Tracks that already match the new preference will immediately stop needing attention.",
	}, setAudioPreference)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "clear_audio_preference",
		Description: "Remove a previously set audio format and/or minimum bitrate preference for a music track's album, reverting it to the global default.",
	}, clearAudioPreference)

	return mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
}
