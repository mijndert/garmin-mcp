package garmin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestResolveAPIKey(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "key")
	if err := os.WriteFile(file, []byte("  file-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("explicit wins", func(t *testing.T) {
		t.Setenv(APIKeyEnvVar, "env-key")
		k, src, err := ResolveAPIKey(" explicit ", file)
		if err != nil || k != "explicit" || src != KeySourceExplicit {
			t.Fatalf("got %q %q %v", k, src, err)
		}
	})
	t.Run("explicit file beats env", func(t *testing.T) {
		t.Setenv(APIKeyEnvVar, "env-key")
		k, src, err := ResolveAPIKey("", file)
		if err != nil || k != "file-key" || src != KeySourceFile {
			t.Fatalf("got %q %q %v", k, src, err)
		}
	})
	t.Run("env", func(t *testing.T) {
		t.Setenv(APIKeyEnvVar, "env-key")
		k, src, err := ResolveAPIKey("", "")
		if err != nil || k != "env-key" || src != KeySourceEnv {
			t.Fatalf("got %q %q %v", k, src, err)
		}
	})
	t.Run("empty env is an error", func(t *testing.T) {
		t.Setenv(APIKeyEnvVar, "  ")
		if _, _, err := ResolveAPIKey("", ""); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("missing explicit file", func(t *testing.T) {
		_, _, err := ResolveAPIKey("", filepath.Join(dir, "nope"))
		if err == nil || !strings.Contains(err.Error(), APIKeyEnvVar) {
			t.Fatalf("expected helpful error, got %v", err)
		}
	})
	t.Run("empty file", func(t *testing.T) {
		empty := filepath.Join(dir, "empty")
		os.WriteFile(empty, []byte("\n"), 0o600)
		if _, _, err := ResolveAPIKey("", empty); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestJWTExpiry(t *testing.T) {
	exp := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	got, err := jwtExpiry(fakeJWT(t, exp))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(exp) {
		t.Fatalf("got %v want %v", got, exp)
	}
	for _, bad := range []string{"", "abc", "a.b.c", "a." + "e30" + ".c"} {
		if _, err := jwtExpiry(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestAuthenticatorExchangeAndCache(t *testing.T) {
	exp := time.Now().Add(24 * time.Hour)
	srv := newFakeGarmin(t, "secret", exp)
	cache := filepath.Join(t.TempDir(), "sub", "token.json")

	a := &Authenticator{BaseURL: srv.URL, APIKey: "secret", HTTPClient: srv.Client(), CacheFile: cache}
	ctx := context.Background()

	tok, src, err := a.Token(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if src != TokenSourceFresh || tok.AccessToken != srv.token {
		t.Fatalf("first call: src=%s", src)
	}
	if tok.ExpiresAt.Unix() != exp.Unix() {
		t.Fatalf("expiry mismatch: %v vs %v", tok.ExpiresAt, exp)
	}
	if srv.tokenCalls.Load() != 1 {
		t.Fatalf("token calls = %d", srv.tokenCalls.Load())
	}

	// Second call is served from memory.
	if _, src, _ = a.Token(ctx); src != TokenSourceMemory {
		t.Fatalf("second call src=%s", src)
	}

	// Cache file exists, is private and matches the Python client's format.
	info, err := os.Stat(cache)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("cache perm = %o", info.Mode().Perm())
	}
	var entry cacheEntry
	data, _ := os.ReadFile(cache)
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.AccessToken != srv.token || entry.Fingerprint != a.fingerprint() || int64(entry.ExpiresAt) != exp.Unix() {
		t.Fatalf("unexpected cache entry %+v", entry)
	}

	// A fresh authenticator with the same key and host reads the cache.
	b := &Authenticator{BaseURL: srv.URL, APIKey: "secret", HTTPClient: srv.Client(), CacheFile: cache}
	if _, src, err := b.Token(ctx); err != nil || src != TokenSourceCache {
		t.Fatalf("cached read: src=%s err=%v", src, err)
	}
	if srv.tokenCalls.Load() != 1 {
		t.Fatalf("cache was not used; token calls = %d", srv.tokenCalls.Load())
	}

	// A different key must not reuse the cached token.
	c := &Authenticator{BaseURL: srv.URL, APIKey: "other", HTTPClient: srv.Client(), CacheFile: cache}
	if _, _, err := c.Token(ctx); err == nil {
		t.Fatal("expected exchange with wrong key to fail")
	}
	if srv.tokenCalls.Load() != 2 {
		t.Fatalf("expected a new exchange attempt, token calls = %d", srv.tokenCalls.Load())
	}

	// Refresh forces a new exchange.
	if _, err := a.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if srv.tokenCalls.Load() != 3 {
		t.Fatalf("refresh did not exchange; token calls = %d", srv.tokenCalls.Load())
	}
}

func TestAuthenticatorRefreshesStaleToken(t *testing.T) {
	// Token expires in 30 minutes: inside the one-hour refresh window.
	srv := newFakeGarmin(t, "secret", time.Now().Add(30*time.Minute))
	a := &Authenticator{BaseURL: srv.URL, APIKey: "secret", HTTPClient: srv.Client()}
	ctx := context.Background()
	if _, _, err := a.Token(ctx); err != nil {
		t.Fatal(err)
	}
	if _, src, _ := a.Token(ctx); src != TokenSourceFresh {
		t.Fatalf("stale token should be re-exchanged, got %s", src)
	}
	if srv.tokenCalls.Load() != 2 {
		t.Fatalf("token calls = %d", srv.tokenCalls.Load())
	}
}

func TestAuthenticatorNoCacheFile(t *testing.T) {
	srv := newFakeGarmin(t, "secret", time.Now().Add(time.Hour*5))
	a := &Authenticator{BaseURL: srv.URL, APIKey: "secret", HTTPClient: srv.Client()}
	if _, _, err := a.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(DefaultTokenCacheFile()); err == nil {
		// Only fail if the file was created *by this test*: we can't know, so
		// just make sure we didn't write the fake token there.
		data, _ := os.ReadFile(DefaultTokenCacheFile())
		if strings.Contains(string(data), srv.token) {
			t.Fatal("wrote token to default cache despite CacheFile being empty")
		}
	}
}

func TestAuthenticatorBadKey(t *testing.T) {
	srv := newFakeGarmin(t, "secret", time.Now().Add(time.Hour*5))
	a := &Authenticator{BaseURL: srv.URL, APIKey: "wrong", HTTPClient: srv.Client()}
	_, _, err := a.Token(context.Background())
	var he *HTTPError
	if !asHTTPError(err, &he) || he.StatusCode != 401 {
		t.Fatalf("expected 401 HTTPError, got %v", err)
	}
	if strings.Contains(err.Error(), "wrong") {
		t.Fatal("error message leaked the API key")
	}
}
