package garmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPError is returned when the API responds with a non-2xx status.
type HTTPError struct {
	Method     string
	URL        string
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	msg := fmt.Sprintf("%s %s returned HTTP %d", e.Method, redactURL(e.URL), e.StatusCode)
	if hint := statusHint(e.StatusCode); hint != "" {
		msg += " (" + hint + ")"
	}
	if b := strings.TrimSpace(e.Body); b != "" {
		if len(b) > 500 {
			b = b[:500] + "…"
		}
		msg += ": " + b
	}
	return msg
}

func statusHint(code int) string {
	switch code {
	case http.StatusUnauthorized:
		return "authentication failed; the API key may be expired or revoked"
	case http.StatusForbidden:
		return "not authorized for this resource"
	case http.StatusNotFound:
		return "not found"
	case http.StatusTooManyRequests:
		return "rate limited; wait before retrying"
	}
	return ""
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.RawQuery = ""
	return u.String()
}

// Client calls the Garmin Connect Personal API.
type Client struct {
	BaseURL    string
	Auth       *Authenticator
	HTTPClient *http.Client
	UserAgent  string
}

// NewClient builds a client and its authenticator for the given key.
func NewClient(baseURL, apiKey, tokenCacheFile string, httpClient *http.Client) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		BaseURL:    baseURL,
		HTTPClient: httpClient,
		UserAgent:  "garmin-mcp",
		Auth: &Authenticator{
			BaseURL:    baseURL,
			APIKey:     apiKey,
			HTTPClient: httpClient,
			CacheFile:  tokenCacheFile,
		},
	}
}

// Query holds URL query parameters. Nil-safe helpers make building it terse.
type Query url.Values

// Set adds a parameter when value is non-empty.
func (q Query) Set(key, value string) Query {
	if value != "" {
		url.Values(q).Set(key, value)
	}
	return q
}

// SetInt adds an integer parameter when ptr is non-nil.
func (q Query) SetInt(key string, v *int64) Query {
	if v != nil {
		url.Values(q).Set(key, fmt.Sprint(*v))
	}
	return q
}

// SetFloat adds a float parameter when ptr is non-nil.
func (q Query) SetFloat(key string, v *float64) Query {
	if v != nil {
		url.Values(q).Set(key, fmt.Sprint(*v))
	}
	return q
}

// SetBool adds a boolean parameter when ptr is non-nil.
func (q Query) SetBool(key string, v *bool) Query {
	if v != nil {
		url.Values(q).Set(key, fmt.Sprint(*v))
	}
	return q
}

// Add appends a repeated parameter for each value.
func (q Query) Add(key string, values []string) Query {
	for _, v := range values {
		if v != "" {
			url.Values(q).Add(key, v)
		}
	}
	return q
}

// ErrNoContent is returned when the API answers 204, meaning no data exists.
var ErrNoContent = errors.New("no data")

// GetJSON performs an authenticated GET and returns the raw JSON body.
func (c *Client) GetJSON(ctx context.Context, path string, q Query) (json.RawMessage, error) {
	body, _, err := c.get(ctx, path, q, "application/json")
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, ErrNoContent
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("GET %s returned a non-JSON response", path)
	}
	return json.RawMessage(body), nil
}

// Download performs an authenticated GET for a binary payload. It returns the
// bytes, the server-suggested filename (may be empty) and the content type.
func (c *Client) Download(ctx context.Context, path string) ([]byte, string, string, error) {
	body, hdr, err := c.get(ctx, path, nil, "application/zip, application/x-zip-compressed, application/octet-stream, */*")
	if err != nil {
		return nil, "", "", err
	}
	filename := ""
	if cd := hdr.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			filename = params["filename"]
		}
	}
	return body, filename, hdr.Get("Content-Type"), nil
}

func (c *Client) get(ctx context.Context, path string, q Query, accept string) ([]byte, http.Header, error) {
	body, hdr, status, err := c.doOnce(ctx, path, q, accept, false)
	if status == http.StatusUnauthorized {
		// The cached token may have been revoked; refresh once and retry.
		slog.Debug("got 401, refreshing bearer token and retrying", "path", path)
		body, hdr, _, err = c.doOnce(ctx, path, q, accept, true)
	}
	return body, hdr, err
}

func (c *Client) doOnce(ctx context.Context, path string, q Query, accept string, forceRefresh bool) ([]byte, http.Header, int, error) {
	var tok Token
	var err error
	if forceRefresh {
		tok, err = c.Auth.Refresh(ctx)
	} else {
		tok, _, err = c.Auth.Token(ctx)
	}
	if err != nil {
		return nil, nil, 0, err
	}

	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + url.Values(q).Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, 0, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	slog.Debug("GET", "url", redactURL(u))
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, resp.StatusCode, fmt.Errorf("GET %s: read body: %w", path, err)
	}
	if resp.StatusCode == http.StatusNoContent {
		return nil, resp.Header, resp.StatusCode, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header, resp.StatusCode, &HTTPError{Method: http.MethodGet, URL: u, StatusCode: resp.StatusCode, Body: string(body)}
	}
	return body, resp.Header, resp.StatusCode, nil
}
