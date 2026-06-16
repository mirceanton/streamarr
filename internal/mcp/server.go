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
		Description: "Get the specific reasons a single media file needs attention.",
	}, getAttentionReasons)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "trigger_track_job",
		Description: "Trigger a background job that removes audio/subtitle tracks or extracts subtitle tracks from a media file, in order to clear its needs-attention status.",
	}, triggerTrackJob)

	return mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
}
