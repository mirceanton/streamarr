// Package mcp exposes a Model Context Protocol server so AI agents can
// inspect which media files need attention and trigger jobs to fix them.
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

	return mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
}
