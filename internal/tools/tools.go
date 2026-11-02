// Package tools registers the Garmin Connect Personal API as MCP tools.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mijndert/garmin-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Config holds runtime settings for the tool handlers.
type Config struct {
	// DownloadDir is where activity files are saved when no output_dir is given.
	DownloadDir string
}

// Deps bundles what handlers need.
type Deps struct {
	Client *garmin.Client
	Config Config
}

// Instructions is sent to MCP clients on initialize.
const Instructions = `Tools for reading the authenticated user's own Garmin Connect data (activities, sleep, heart rate, HRV, stress, Body Battery, steps, weight, nutrition and more) through the Garmin Connect Personal API.

Conventions:
- All dates are calendar dates in YYYY-MM-DD, interpreted in the user's local time zone. Single-date tools default to today when the date is omitted.
- Date-range tools have server-side span limits (noted per tool). Split longer periods into several calls.
- Activity IDs come from search_activities; pass them to get_activity, get_activity_workouts or download_activity_file.
- Distances are in meters, durations in seconds, weights in grams unless a field says otherwise.
- Responses are raw Garmin JSON. Some (daily heart rate, daily summaries over long ranges) are large; prefer narrower ranges.
- Everything is read-only except download_activity_file, which writes a file to the local disk.`

// Register adds every tool to the server.
func Register(s *mcp.Server, d *Deps) {
	registerActivity(s, d)
	registerHealth(s, d)
	registerWomensHealth(s, d)
	registerProfile(s, d)
	registerMeta(s, d)
}

// readOnly marks a tool as not modifying anything.
func readOnly(title string) *mcp.ToolAnnotations {
	f := false
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &f}
}

// jsonHandler is the shape of most handlers: typed input in, raw JSON out.
type jsonHandler[In any] func(ctx context.Context, d *Deps, in In) (json.RawMessage, error)

// addJSON registers a read-only tool whose result is the API's JSON body.
func addJSON[In any](s *mcp.Server, d *Deps, name, title, desc string, h jsonHandler[In]) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        name,
		Description: desc,
		Annotations: readOnly(title),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		raw, err := h(ctx, d, in)
		if err != nil {
			if errors.Is(err, garmin.ErrNoContent) {
				return textResult("No data is available for the requested date(s)."), nil, nil
			}
			return nil, nil, err
		}
		return jsonResult(raw), nil, nil
	})
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// jsonResult compacts the JSON to keep token usage down.
func jsonResult(raw json.RawMessage) *mcp.CallToolResult {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return textResult(string(raw))
	}
	return textResult(buf.String())
}

// marshalResult encodes a Go value for tools that build their own output.
func marshalResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode result: %w", err)
	}
	return textResult(string(b)), nil
}
