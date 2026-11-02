package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mijndert/garmin-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeAPI struct {
	*httptest.Server
	token    string
	requests []*http.Request
	respond  func(w http.ResponseWriter, r *http.Request) bool // return true if handled
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, _ := json.Marshal(map[string]any{"exp": time.Now().Add(24 * time.Hour).Unix()})
	f := &fakeAPI{token: hdr + "." + base64.RawURLEncoding.EncodeToString(payload) + ".s"}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == garmin.TokenPath {
			if r.Header.Get("Authorization") != "ApiKey test-key" {
				http.Error(w, "bad key", 401)
				return
			}
			fmt.Fprint(w, f.token)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+f.token {
			http.Error(w, "bad token", 401)
			return
		}
		f.requests = append(f.requests, r)
		if f.respond != nil && f.respond(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "{\n  \"path\": %q,\n  \"query\": %q\n}", r.URL.Path, r.URL.RawQuery)
	}))
	t.Cleanup(f.Close)
	return f
}

// newSession wires a real MCP client to our server over in-memory transports.
func newSession(t *testing.T, api *fakeAPI, downloadDir string) *mcp.ClientSession {
	t.Helper()
	client := garmin.NewClient(api.URL, "test-key", "", api.Client())
	server := mcp.NewServer(&mcp.Implementation{Name: "garmin-mcp-test", Version: "test"}, &mcp.ServerOptions{Instructions: Instructions})
	Register(server, &Deps{Client: client, Config: Config{DownloadDir: downloadDir}})

	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func callText(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String(), res.IsError
}

func TestListTools(t *testing.T) {
	cs := newSession(t, newFakeAPI(t), t.TempDir())
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"search_activities", "get_activity", "get_activity_workouts", "download_activity_file", "get_wellness_activity_summaries",
		"get_daily_summary", "get_daily_summaries", "get_daily_steps", "get_daily_stress", "get_daily_hydration",
		"get_daily_heart_rate", "get_heart_rate_zones", "get_hrv", "get_hrv_daily",
		"get_daily_sleep", "get_sleep_summaries", "get_body_battery", "get_spo2", "get_fitness_age",
		"get_weight_entries", "get_weight_range", "get_weight_goals", "get_nutrition_logs",
		"get_menstrual_cycle_summary", "get_menstrual_cycle_day_view",
		"get_user_profile_settings", "get_auth_status",
	}
	got := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		got[tool.Name] = tool
		if tool.Description == "" {
			t.Errorf("%s has no description", tool.Name)
		}
		if tool.Annotations == nil || tool.Annotations.Title == "" {
			t.Errorf("%s has no title annotation", tool.Name)
		}
		if tool.Name != "download_activity_file" && !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s should be read-only", tool.Name)
		}
	}
	for _, name := range want {
		if _, ok := got[name]; !ok {
			t.Errorf("missing tool %s", name)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d tools, want %d", len(got), len(want))
	}
}

func TestRangeToolBuildsPathAndCompactsJSON(t *testing.T) {
	api := newFakeAPI(t)
	cs := newSession(t, api, t.TempDir())
	text, isErr := callText(t, cs, "get_daily_steps", map[string]any{"start_date": "2026-01-01", "end_date": "2026-01-07"})
	if isErr {
		t.Fatalf("unexpected tool error: %s", text)
	}
	if text != `{"path":"/usersummary-service/stats/steps/daily/2026-01-01/2026-01-07","query":""}` {
		t.Fatalf("got %s", text)
	}
}

func TestRangeLimitIsEnforcedLocally(t *testing.T) {
	api := newFakeAPI(t)
	cs := newSession(t, api, t.TempDir())
	text, isErr := callText(t, cs, "get_daily_steps", map[string]any{"start_date": "2026-01-01", "end_date": "2026-03-01"})
	if !isErr || !strings.Contains(text, "at most 28") {
		t.Fatalf("expected range error, got isErr=%v %s", isErr, text)
	}
	if len(api.requests) != 0 {
		t.Fatal("invalid input should not reach the API")
	}
}

func TestMissingRequiredArgumentIsRejected(t *testing.T) {
	cs := newSession(t, newFakeAPI(t), t.TempDir())
	_, isErr := callText(t, cs, "get_activity", map[string]any{})
	if !isErr {
		t.Fatal("expected schema validation error")
	}
}

func TestOptionalDateDefaultsToToday(t *testing.T) {
	api := newFakeAPI(t)
	cs := newSession(t, api, t.TempDir())
	text, isErr := callText(t, cs, "get_daily_sleep", map[string]any{})
	if isErr {
		t.Fatal(text)
	}
	if !strings.Contains(text, "date="+garmin.Today()) {
		t.Fatalf("expected today's date in query, got %s", text)
	}
}

func TestSearchActivitiesQuery(t *testing.T) {
	api := newFakeAPI(t)
	cs := newSession(t, api, t.TempDir())
	text, isErr := callText(t, cs, "search_activities", map[string]any{
		"limit": 5, "activity_type": "running", "start_date": "2026-01-01", "end_date": "2026-01-31",
		"exclude_children": false, "excluded_activity_sub_types": []string{"treadmill_running", "indoor_track_running"},
	})
	if isErr {
		t.Fatal(text)
	}
	var got struct{ Query string }
	json.Unmarshal([]byte(text), &got)
	for _, want := range []string{"limit=5", "activityType=running", "startDate=2026-01-01", "endDate=2026-01-31", "excludeChildren=false", "excludedActivitySubType=treadmill_running", "excludedActivitySubType=indoor_track_running"} {
		if !strings.Contains(got.Query, want) {
			t.Errorf("query %q missing %s", got.Query, want)
		}
	}
	if strings.Contains(got.Query, "start=") {
		t.Errorf("unset start should be omitted: %s", got.Query)
	}
}

func TestNoContentBecomesFriendlyText(t *testing.T) {
	api := newFakeAPI(t)
	api.respond = func(w http.ResponseWriter, r *http.Request) bool { w.WriteHeader(204); return true }
	cs := newSession(t, api, t.TempDir())
	text, isErr := callText(t, cs, "get_hrv", map[string]any{"date": "2026-01-01"})
	if isErr || !strings.Contains(text, "No data") {
		t.Fatalf("got isErr=%v %s", isErr, text)
	}
}

func TestAPIErrorsAreToolErrors(t *testing.T) {
	api := newFakeAPI(t)
	api.respond = func(w http.ResponseWriter, r *http.Request) bool { http.Error(w, "gone", 404); return true }
	cs := newSession(t, api, t.TempDir())
	text, isErr := callText(t, cs, "get_activity", map[string]any{"activity_id": 42})
	if !isErr || !strings.Contains(text, "HTTP 404") {
		t.Fatalf("got isErr=%v %s", isErr, text)
	}
}

func TestAuthStatus(t *testing.T) {
	cs := newSession(t, newFakeAPI(t), t.TempDir())
	text, isErr := callText(t, cs, "get_auth_status", map[string]any{})
	if isErr {
		t.Fatal(text)
	}
	var st authStatus
	if err := json.Unmarshal([]byte(text), &st); err != nil {
		t.Fatal(err)
	}
	if st.TokenSource != "fresh" || st.Today != garmin.Today() || st.TokenExpiresAt == "" {
		t.Fatalf("got %+v", st)
	}
	if strings.Contains(text, "eyJ") {
		t.Fatal("auth status must not include the token")
	}
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	zw.Close()
	return buf.Bytes()
}

func TestDownloadActivityFile(t *testing.T) {
	api := newFakeAPI(t)
	zipData := makeZip(t, map[string]string{"nested/dir/123.fit": "FITDATA", "../../evil.txt": "nope"})
	api.respond = func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasPrefix(r.URL.Path, "/download-service/files/activity/") {
			return false
		}
		w.Header().Set("Content-Type", "application/x-zip-compressed")
		w.Write(zipData)
		return true
	}
	dir := t.TempDir()
	cs := newSession(t, api, dir)

	text, isErr := callText(t, cs, "download_activity_file", map[string]any{"activity_id": 123, "extract": true})
	if isErr {
		t.Fatal(text)
	}
	var res downloadResult
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		t.Fatal(err)
	}
	if res.ZipPath != filepath.Join(dir, "activity_123.zip") || res.SizeBytes != len(zipData) || res.AlreadyExists {
		t.Fatalf("got %+v", res)
	}
	if data, err := os.ReadFile(res.ZipPath); err != nil || !bytes.Equal(data, zipData) {
		t.Fatalf("zip not saved correctly: %v", err)
	}
	if len(res.Extracted) != 2 {
		t.Fatalf("extracted = %v", res.Extracted)
	}
	if fit, err := os.ReadFile(filepath.Join(dir, "123.fit")); err != nil || string(fit) != "FITDATA" {
		t.Fatalf("flattened FIT file missing: %v", err)
	}
	// Path traversal entries are flattened into the output dir, never above it.
	if _, err := os.Stat(filepath.Join(dir, "evil.txt")); err != nil {
		t.Fatal("expected traversal entry to be flattened into dir")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(dir)), "evil.txt")); err == nil {
		t.Fatal("zip slip: file written outside output dir")
	}

	// Second call keeps the existing file without hitting the API.
	before := len(api.requests)
	text, isErr = callText(t, cs, "download_activity_file", map[string]any{"activity_id": 123})
	if isErr {
		t.Fatal(text)
	}
	json.Unmarshal([]byte(text), &res)
	if !res.AlreadyExists || len(api.requests) != before {
		t.Fatalf("expected cached file to be reused, got %+v (requests %d->%d)", res, before, len(api.requests))
	}

	// Explicit output_dir is honoured.
	other := filepath.Join(t.TempDir(), "sub")
	text, isErr = callText(t, cs, "download_activity_file", map[string]any{"activity_id": 123, "output_dir": other})
	if isErr {
		t.Fatal(text)
	}
	if _, err := os.Stat(filepath.Join(other, "activity_123.zip")); err != nil {
		t.Fatal(err)
	}
}
