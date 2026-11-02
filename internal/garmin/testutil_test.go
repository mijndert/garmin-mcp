package garmin

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fakeJWT builds an unsigned JWT-shaped token with the given expiry.
func fakeJWT(t *testing.T, exp time.Time) string {
	t.Helper()
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, _ := json.Marshal(map[string]any{"exp": exp.Unix(), "sub": "user"})
	return hdr + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

// fakeGarmin is an httptest server that issues tokens and serves one data path.
type fakeGarmin struct {
	*httptest.Server
	tokenCalls atomic.Int32
	dataCalls  atomic.Int32
	apiKey     string
	token      string
	// handler for non-token requests; set per test.
	data http.HandlerFunc
}

func newFakeGarmin(t *testing.T, apiKey string, tokenExp time.Time) *fakeGarmin {
	t.Helper()
	f := &fakeGarmin{apiKey: apiKey, token: fakeJWT(t, tokenExp)}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == TokenPath {
			f.tokenCalls.Add(1)
			if r.Method != http.MethodPost {
				http.Error(w, "method", http.StatusMethodNotAllowed)
				return
			}
			if r.Header.Get("Authorization") != "ApiKey "+f.apiKey {
				http.Error(w, "bad key", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, f.token)
			return
		}
		f.dataCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+f.token {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		if f.data != nil {
			f.data(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"path":%q,"query":%q}`, r.URL.Path, r.URL.RawQuery)
	}))
	t.Cleanup(f.Close)
	return f
}
