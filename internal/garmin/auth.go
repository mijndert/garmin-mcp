// Package garmin implements a small client for the Garmin Connect Personal API.
//
// Authentication works in two steps. A personal API key (created on Garmin
// Connect Web) is exchanged for a short-lived OAuth2 bearer token (a JWT valid
// for 24 hours), and that token is sent on every data request. This package
// caches the bearer token on disk in the same file format as Garmin's official
// Python client, so the two can share a cache.
package garmin

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultBaseURL is the production Garmin Connect Personal API host.
	DefaultBaseURL = "https://personal.connectapi.garmin.com"
	// ChinaBaseURL is the host for Garmin Connect accounts in China.
	ChinaBaseURL = "https://personal.connectapi.garmin.cn"

	// TokenPath is the endpoint that exchanges an API key for a bearer token.
	TokenPath = "/personalapitoken-service/personal-api/personal-api-key/token"

	// APIKeyEnvVar names the environment variable holding the personal API key.
	APIKeyEnvVar = "GARMIN_PERSONAL_API_KEY"
	// BaseURLEnvVar names the environment variable overriding the API host.
	BaseURLEnvVar = "GARMIN_PERSONAL_API_BASE_URL"

	// defaultKeyFile and defaultTokenCacheFile are relative to the user's home
	// directory and match Garmin's official Python client.
	defaultKeyFile        = ".config/garmin/connect-personal-api-key"
	defaultTokenCacheFile = ".config/garmin/connect-personal-api-bearer-token.json"

	// refreshWindow is how long before expiry a cached token is considered stale.
	refreshWindow = time.Hour
)

// DefaultKeyFile returns the default location of the API key file.
func DefaultKeyFile() string { return homePath(defaultKeyFile) }

// DefaultTokenCacheFile returns the default location of the bearer token cache.
func DefaultTokenCacheFile() string { return homePath(defaultTokenCacheFile) }

func homePath(rel string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return rel
	}
	return filepath.Join(home, filepath.FromSlash(rel))
}

// KeySource describes where an API key was found.
type KeySource string

const (
	KeySourceExplicit KeySource = "explicit"
	KeySourceEnv      KeySource = "environment"
	KeySourceFile     KeySource = "file"
)

// ResolveAPIKey finds the personal API key. Precedence: explicit value, then
// an explicit key file, then the environment variable, then the default key
// file. It mirrors the resolution order of Garmin's Python client.
func ResolveAPIKey(explicit, keyFile string) (string, KeySource, error) {
	if k := strings.TrimSpace(explicit); k != "" {
		return k, KeySourceExplicit, nil
	}
	if keyFile == "" {
		if v, ok := os.LookupEnv(APIKeyEnvVar); ok {
			k := strings.TrimSpace(v)
			if k == "" {
				return "", "", fmt.Errorf("environment variable %s is set but empty", APIKeyEnvVar)
			}
			return k, KeySourceEnv, nil
		}
		keyFile = DefaultKeyFile()
	}
	data, err := os.ReadFile(keyFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", fmt.Errorf("no personal API key found: set %s or create %s (see https://developer.garmin.com/gc-developer-program/personal/personal-api/)", APIKeyEnvVar, keyFile)
		}
		return "", "", fmt.Errorf("read API key file %s: %w", keyFile, err)
	}
	warnIfWorldReadable(keyFile)
	k := strings.TrimSpace(string(data))
	if k == "" {
		return "", "", fmt.Errorf("API key file %s is empty", keyFile)
	}
	return k, KeySourceFile, nil
}

func warnIfWorldReadable(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.Mode().Perm()&0o077 != 0 {
		slog.Warn("credential file is readable by other users; restrict it with chmod 600", "path", path)
	}
}

// Token is a bearer token with its expiry.
type Token struct {
	AccessToken string
	ExpiresAt   time.Time
}

// Valid reports whether the token is usable for at least the refresh window.
func (t Token) Valid(now time.Time) bool {
	return t.AccessToken != "" && t.ExpiresAt.Sub(now) > refreshWindow
}

// Authenticator exchanges an API key for bearer tokens and caches them.
type Authenticator struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	// CacheFile is where the bearer token is persisted. Empty disables caching.
	CacheFile string
	// Now is the clock used for expiry checks; defaults to time.Now.
	Now func() time.Time

	mu    sync.Mutex
	token Token
}

// TokenSource reports where the most recent token came from.
type TokenSource string

const (
	TokenSourceMemory TokenSource = "memory"
	TokenSourceCache  TokenSource = "cache"
	TokenSourceFresh  TokenSource = "fresh"
)

// Token returns a valid bearer token, exchanging the API key if required.
func (a *Authenticator) Token(ctx context.Context) (Token, TokenSource, error) {
	return a.getToken(ctx, false)
}

// Refresh discards any cached token and obtains a new one.
func (a *Authenticator) Refresh(ctx context.Context) (Token, error) {
	t, _, err := a.getToken(ctx, true)
	return t, err
}

func (a *Authenticator) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Authenticator) fingerprint() string {
	sum := sha256.Sum256([]byte(strings.TrimRight(a.BaseURL, "/") + "\x00" + a.APIKey))
	return hex.EncodeToString(sum[:])
}

func (a *Authenticator) getToken(ctx context.Context, force bool) (Token, TokenSource, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := a.now()
	if !force {
		if a.token.Valid(now) {
			return a.token, TokenSourceMemory, nil
		}
		if t, ok := a.readCache(); ok && t.Valid(now) {
			a.token = t
			return t, TokenSourceCache, nil
		}
	}

	t, err := a.exchange(ctx)
	if err != nil {
		return Token{}, "", err
	}
	a.token = t
	a.writeCache(t)
	return t, TokenSourceFresh, nil
}

func (a *Authenticator) exchange(ctx context.Context) (Token, error) {
	if a.APIKey == "" {
		return Token{}, errors.New("no API key configured")
	}
	url := strings.TrimRight(a.BaseURL, "/") + TokenPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Authorization", "ApiKey "+a.APIKey)

	slog.Debug("exchanging API key for bearer token", "url", url)
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Token{}, fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Token{}, &HTTPError{Method: http.MethodPost, URL: url, StatusCode: resp.StatusCode, Body: string(body)}
	}
	access := strings.TrimSpace(string(body))
	// Be lenient: some gateways wrap the token in JSON.
	if strings.HasPrefix(access, "{") {
		var wrapped struct {
			AccessToken string `json:"access_token"`
			Token       string `json:"token"`
		}
		if json.Unmarshal(body, &wrapped) == nil {
			if wrapped.AccessToken != "" {
				access = wrapped.AccessToken
			} else if wrapped.Token != "" {
				access = wrapped.Token
			}
		}
	}
	exp, err := jwtExpiry(access)
	if err != nil {
		return Token{}, fmt.Errorf("token endpoint did not return a JWT with an exp claim: %w", err)
	}
	return Token{AccessToken: access, ExpiresAt: exp}, nil
}

func (a *Authenticator) httpClient() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return http.DefaultClient
}

// jwtExpiry extracts the exp claim from a JWT without verifying its signature.
// Verification is unnecessary here: the token is only presented back to the
// server that issued it, and exp is used purely to schedule refreshes.
func jwtExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return time.Time{}, errors.New("not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}, fmt.Errorf("decode JWT payload: %w", err)
	}
	var claims struct {
		Exp json.Number `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, fmt.Errorf("parse JWT payload: %w", err)
	}
	f, err := claims.Exp.Float64()
	if err != nil || f <= 0 {
		return time.Time{}, errors.New("missing exp claim")
	}
	sec, frac := int64(f), f-float64(int64(f))
	return time.Unix(sec, int64(frac*1e9)), nil
}

// cacheEntry matches the on-disk format of Garmin's Python client.
type cacheEntry struct {
	Fingerprint string  `json:"fingerprint"`
	AccessToken string  `json:"access_token"`
	ExpiresAt   float64 `json:"expires_at"`
}

func (a *Authenticator) readCache() (Token, bool) {
	if a.CacheFile == "" {
		return Token{}, false
	}
	data, err := os.ReadFile(a.CacheFile)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("cannot read token cache", "path", a.CacheFile, "err", err)
		}
		return Token{}, false
	}
	warnIfWorldReadable(a.CacheFile)
	var e cacheEntry
	if err := json.Unmarshal(data, &e); err != nil || e.Fingerprint != a.fingerprint() || e.AccessToken == "" {
		return Token{}, false
	}
	sec, frac := int64(e.ExpiresAt), e.ExpiresAt-float64(int64(e.ExpiresAt))
	return Token{AccessToken: e.AccessToken, ExpiresAt: time.Unix(sec, int64(frac*1e9))}, true
}

func (a *Authenticator) writeCache(t Token) {
	if a.CacheFile == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(a.CacheFile), 0o700); err != nil {
		slog.Warn("cannot create token cache directory", "err", err)
		return
	}
	data, err := json.Marshal(cacheEntry{
		Fingerprint: a.fingerprint(),
		AccessToken: t.AccessToken,
		ExpiresAt:   float64(t.ExpiresAt.UnixNano()) / 1e9,
	})
	if err != nil {
		return
	}
	tmp := a.CacheFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		slog.Warn("cannot write token cache", "path", a.CacheFile, "err", err)
		return
	}
	if err := os.Rename(tmp, a.CacheFile); err != nil {
		slog.Warn("cannot replace token cache", "path", a.CacheFile, "err", err)
		_ = os.Remove(tmp)
	}
}
