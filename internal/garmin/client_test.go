package garmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func asHTTPError(err error, target **HTTPError) bool { return errors.As(err, target) }

func newTestClient(t *testing.T, srv *fakeGarmin) *Client {
	t.Helper()
	c := NewClient(srv.URL, srv.apiKey, "", srv.Client())
	return c
}

func TestGetJSONSendsBearerAndQuery(t *testing.T) {
	srv := newFakeGarmin(t, "k", time.Now().Add(5*time.Hour))
	c := newTestClient(t, srv)
	raw, err := c.GetJSON(context.Background(), "/usersummary-service/usersummary/daily", Query{}.Set("calendarDate", "2026-01-02").Set("empty", ""))
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Path, Query string }
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Path != "/usersummary-service/usersummary/daily" || got.Query != "calendarDate=2026-01-02" {
		t.Fatalf("got %+v", got)
	}
}

func TestQueryHelpers(t *testing.T) {
	i, f, b := int64(7), 1.5, true
	q := Query{}.SetInt("i", &i).SetInt("ni", nil).SetFloat("f", &f).SetBool("b", &b).Add("r", []string{"a", "", "b"})
	enc := fmt.Sprint(q)
	for _, want := range []string{"i:[7]", "f:[1.5]", "b:[true]", "r:[a b]"} {
		if !strings.Contains(enc, want) {
			t.Errorf("missing %s in %s", want, enc)
		}
	}
	if strings.Contains(enc, "ni") {
		t.Error("nil int should be omitted")
	}
}

func TestGetJSONNoContent(t *testing.T) {
	srv := newFakeGarmin(t, "k", time.Now().Add(5*time.Hour))
	srv.data = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }
	c := newTestClient(t, srv)
	_, err := c.GetJSON(context.Background(), "/hrv-service/hrv/2026-01-01", nil)
	if !errors.Is(err, ErrNoContent) {
		t.Fatalf("want ErrNoContent, got %v", err)
	}
}

func TestGetJSONHTTPError(t *testing.T) {
	srv := newFakeGarmin(t, "k", time.Now().Add(5*time.Hour))
	srv.data = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"nope"}`, http.StatusForbidden)
	}
	c := newTestClient(t, srv)
	_, err := c.GetJSON(context.Background(), "/x", Query{}.Set("secret", "value"))
	var he *HTTPError
	if !errors.As(err, &he) || he.StatusCode != 403 {
		t.Fatalf("want 403, got %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "HTTP 403") || !strings.Contains(msg, "nope") || !strings.Contains(msg, "not authorized") {
		t.Fatalf("unhelpful message: %s", msg)
	}
	if strings.Contains(msg, "secret=value") {
		t.Fatal("error message should not include the query string")
	}
}

func TestGetJSONRetriesOnceAfter401(t *testing.T) {
	srv := newFakeGarmin(t, "k", time.Now().Add(5*time.Hour))
	c := newTestClient(t, srv)
	ctx := context.Background()

	// Prime a token, then rotate the server-side token so the cached one fails.
	if _, err := c.GetJSON(ctx, "/a", nil); err != nil {
		t.Fatal(err)
	}
	srv.token = fakeJWT(t, time.Now().Add(6*time.Hour))

	if _, err := c.GetJSON(ctx, "/b", nil); err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if srv.tokenCalls.Load() != 2 {
		t.Fatalf("token calls = %d, want 2", srv.tokenCalls.Load())
	}
	if srv.dataCalls.Load() != 3 {
		t.Fatalf("data calls = %d, want 3 (ok, 401, ok)", srv.dataCalls.Load())
	}
}

func TestGetJSONRejectsNonJSON(t *testing.T) {
	srv := newFakeGarmin(t, "k", time.Now().Add(5*time.Hour))
	srv.data = func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>") }
	c := newTestClient(t, srv)
	if _, err := c.GetJSON(context.Background(), "/x", nil); err == nil {
		t.Fatal("expected error for non-JSON body")
	}
}

func TestDownload(t *testing.T) {
	srv := newFakeGarmin(t, "k", time.Now().Add(5*time.Hour))
	srv.data = func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept"), "zip") {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/x-zip-compressed")
		w.Header().Set("Content-Disposition", `attachment; filename="12345.zip"`)
		w.Write([]byte("PK\x03\x04data"))
	}
	c := newTestClient(t, srv)
	data, name, ct, err := c.Download(context.Background(), "/download-service/files/activity/12345")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "PK\x03\x04data" || name != "12345.zip" || ct != "application/x-zip-compressed" {
		t.Fatalf("got %q %q %q", data, name, ct)
	}
}
