package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mijndert/garmin-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type emptyInput struct{}

type authStatus struct {
	BaseURL        string `json:"base_url"`
	TokenSource    string `json:"token_source"`
	TokenExpiresAt string `json:"token_expires_at"`
	TokenValidFor  string `json:"token_valid_for"`
	Today          string `json:"today"`
}

func registerProfile(s *mcp.Server, d *Deps) {
	addJSON(s, d, "get_user_profile_settings", "Get user profile settings",
		"Get the user's profile settings: gender, birth date, height, weight, measurement units, time and date formats, time zone, activity level, VO2max, lactate threshold, sleep window and more. Useful for interpreting units in other responses.",
		func(ctx context.Context, d *Deps, _ emptyInput) (json.RawMessage, error) {
			return d.Client.GetJSON(ctx, "/userprofile-service/userprofile/settings", nil)
		})
}

func registerMeta(s *mcp.Server, d *Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_auth_status",
		Description: "Check that the Garmin personal API key works by obtaining (or reusing) a bearer token. Returns the API host, whether the token came from cache or a fresh exchange, when it expires, and today's local date. Does not return the token itself. Use this first when other tools fail with authentication errors.",
		Annotations: readOnly("Check authentication"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
		tok, src, err := d.Client.Auth.Token(ctx)
		if err != nil {
			return nil, nil, err
		}
		r, err := marshalResult(authStatus{
			BaseURL:        d.Client.BaseURL,
			TokenSource:    string(src),
			TokenExpiresAt: tok.ExpiresAt.Format(time.RFC3339),
			TokenValidFor:  time.Until(tok.ExpiresAt).Truncate(time.Minute).String(),
			Today:          garmin.Today(),
		})
		return r, nil, err
	})
}
