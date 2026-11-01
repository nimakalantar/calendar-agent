package gcalendar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type calendarRequest struct {
	SyncToken    string
	TimeMin      string
	TimeMax      string
	SingleEvents string
	ShowDeleted  string
}

type calendarResponder func(calendarRequest) (int, string)

func newCacheTestClient(t *testing.T, cachePath string, now *time.Time, responder calendarResponder) (*Client, *[]calendarRequest) {
	t.Helper()
	var mu sync.Mutex
	var requests []calendarRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		request := calendarRequest{
			SyncToken:    query.Get("syncToken"),
			TimeMin:      query.Get("timeMin"),
			TimeMax:      query.Get("timeMax"),
			SingleEvents: query.Get("singleEvents"),
			ShowDeleted:  query.Get("showDeleted"),
		}
		mu.Lock()
		requests = append(requests, request)
		mu.Unlock()
		status, body := responder(request)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	client, err := NewClientWithOptions(context.Background(),
		oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test"}),
		server.URL+"/",
		ClientOptions{
			CachePath:        cachePath,
			IncrementalSync:  true,
			FullSyncInterval: time.Hour,
			Now:              func() time.Time { return *now },
			Ownership:        testOwnership,
			MetricsPrefix:    "calendar_agent",
		})
	if err != nil {
		t.Fatal(err)
	}
	return client, &requests
}

func calendarEvent(id, summary string, start, end time.Time) map[string]any {
	return map[string]any{
		"id":      id,
		"summary": summary,
		"start":   map[string]any{"dateTime": start.Format(time.RFC3339)},
		"end":     map[string]any{"dateTime": end.Format(time.RFC3339)},
		"status":  "confirmed",
	}
}

func eventsPage(token string, events ...map[string]any) string {
	payload := map[string]any{"items": events}
	if token != "" {
		payload["nextSyncToken"] = token
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func TestIncrementalCacheFullThenUpdatesThenDeletes(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/London")
	now := time.Date(2026, time.June, 2, 0, 0, 0, 0, loc)
	horizon := now
	cachePath := filepath.Join(t.TempDir(), "calendar-cache.json")

	call := 0
	client, requests := newCacheTestClient(t, cachePath, &now, func(calendarRequest) (int, string) {
		call++
		switch call {
		case 1:
			return http.StatusOK, eventsPage("tok-1",
				calendarEvent("e1", "first", horizon.Add(10*time.Hour), horizon.Add(11*time.Hour)),
				calendarEvent("e2", "second", horizon.Add(12*time.Hour), horizon.Add(13*time.Hour)),
			)
		case 2:
			return http.StatusOK, eventsPage("tok-2",
				calendarEvent("e1", "first updated", horizon.Add(10*time.Hour), horizon.Add(11*time.Hour)),
				calendarEvent("e3", "third", horizon.Add(14*time.Hour), horizon.Add(15*time.Hour)),
			)
		default:
			return http.StatusOK, eventsPage("tok-3",
				map[string]any{"id": "e2", "status": "cancelled"},
			)
		}
	})

	events, err := client.ListAll(context.Background(), "src", horizon, 7, "Europe/London", loc, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].ID != "e1" || events[1].ID != "e2" {
		t.Fatalf("full sync events = %+v", events)
	}

	events, err = client.ListAll(context.Background(), "src", horizon, 7, "Europe/London", loc, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Summary != "first updated" {
		t.Fatalf("incremental events = %+v", events)
	}

	events, err = client.ListAll(context.Background(), "src", horizon, 7, "Europe/London", loc, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("post-delete events = %+v", events)
	}
	for _, event := range events {
		if event.ID == "e2" {
			t.Fatalf("deleted event remained: %+v", events)
		}
	}

	if len(*requests) != 3 {
		t.Fatalf("requests = %+v", *requests)
	}
	if (*requests)[0].SyncToken != "" || (*requests)[0].TimeMax == "" {
		t.Fatalf("full sync request = %+v", (*requests)[0])
	}
	if (*requests)[1].SyncToken != "tok-1" || (*requests)[1].TimeMin != "" || (*requests)[1].TimeMax != "" {
		t.Fatalf("incremental request = %+v", (*requests)[1])
	}
	if (*requests)[2].SyncToken != "tok-2" {
		t.Fatalf("second incremental request = %+v", (*requests)[2])
	}
	for _, request := range *requests {
		if request.SingleEvents != "true" || request.ShowDeleted != "true" {
			t.Fatalf("query shape changed between syncs: %+v", request)
		}
	}
}

func TestIncrementalCacheResyncsOnGone(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/London")
	now := time.Date(2026, time.June, 2, 0, 0, 0, 0, loc)
	cachePath := filepath.Join(t.TempDir(), "calendar-cache.json")

	call := 0
	client, requests := newCacheTestClient(t, cachePath, &now, func(calendarRequest) (int, string) {
		call++
		switch call {
		case 1:
			return http.StatusOK, eventsPage("tok-1",
				calendarEvent("old", "old", now.Add(10*time.Hour), now.Add(11*time.Hour)))
		case 2:
			return http.StatusGone, `{"error":{"code":410,"message":"sync token is no longer valid"}}`
		default:
			return http.StatusOK, eventsPage("tok-2",
				calendarEvent("new", "new", now.Add(10*time.Hour), now.Add(11*time.Hour)))
		}
	})
	registry := newFakeMetrics()
	client.opts.Metrics = registry

	if _, err := client.ListAll(context.Background(), "src", now, 7, "Europe/London", loc, Config{}); err != nil {
		t.Fatal(err)
	}
	events, err := client.ListAll(context.Background(), "src", now, 7, "Europe/London", loc, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != "new" {
		t.Fatalf("resynced events = %+v", events)
	}
	if len(*requests) != 3 {
		t.Fatalf("requests = %+v", *requests)
	}
	if (*requests)[1].SyncToken != "tok-1" || (*requests)[2].SyncToken != "" {
		t.Fatalf("resync request sequence = %+v", *requests)
	}
	if rendered := registry.Render(); !strings.Contains(rendered, `calendar_agent_calendar_cache_resyncs_total{reason="gone"} 1`) {
		t.Fatalf("metrics missing gone resync:\n%s", rendered)
	}
}

func TestIncrementalCacheRebuildsCorruptFile(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/London")
	now := time.Date(2026, time.June, 2, 0, 0, 0, 0, loc)
	cachePath := filepath.Join(t.TempDir(), "calendar-cache.json")
	if err := os.WriteFile(cachePath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	client, requests := newCacheTestClient(t, cachePath, &now, func(calendarRequest) (int, string) {
		return http.StatusOK, eventsPage("tok-new",
			calendarEvent("e1", "first", now.Add(10*time.Hour), now.Add(11*time.Hour)))
	})
	registry := newFakeMetrics()
	client.opts.Metrics = registry
	events, err := client.ListAll(context.Background(), "src", now, 7, "Europe/London", loc, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}
	if len(*requests) != 1 || (*requests)[0].SyncToken != "" {
		t.Fatalf("requests = %+v", *requests)
	}
	var cache calendarCache
	if err := json.Unmarshal(mustReadFile(t, cachePath), &cache); err != nil {
		t.Fatal(err)
	}
	if cache.Version != calendarCacheVersion || cache.Calendars["src"].SyncToken != "tok-new" {
		t.Fatalf("cache = %+v", cache)
	}
	rendered := registry.Render()
	for _, want := range []string{
		`calendar_agent_calendar_cache_resyncs_total{reason="corrupt"} 1`,
		"calendar_agent_calendar_cache_last_full_sync_timestamp_seconds",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("metrics missing %q:\n%s", want, rendered)
		}
	}
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode = %o, want 600", info.Mode().Perm())
	}
}

func TestIncrementalCacheFiltersManagedOnly(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/London")
	now := time.Date(2026, time.June, 2, 0, 0, 0, 0, loc)
	cachePath := filepath.Join(t.TempDir(), "calendar-cache.json")

	managed := calendarEvent("managed", "owned", now.Add(10*time.Hour), now.Add(11*time.Hour))
	managed["extendedProperties"] = map[string]any{"private": map[string]string{"managedBy": "calendar-agent"}}
	personal := calendarEvent("personal", "personal", now.Add(12*time.Hour), now.Add(13*time.Hour))

	client, _ := newCacheTestClient(t, cachePath, &now, func(calendarRequest) (int, string) {
		return http.StatusOK, eventsPage("tok-1", managed, personal)
	})
	all, err := client.ListAll(context.Background(), "dst", now, 7, "Europe/London", loc, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("all events = %+v", all)
	}
	events, err := client.ListManaged(context.Background(), "dst", now, 7, "Europe/London", loc, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != "managed" {
		t.Fatalf("managed events = %+v", events)
	}
	var cache calendarCache
	if err := json.Unmarshal(mustReadFile(t, cachePath), &cache); err != nil {
		t.Fatal(err)
	}
	if len(cache.Calendars["dst"].Events) != 1 {
		t.Fatalf("cache = %+v", cache.Calendars["dst"].Events)
	}
}

func TestIncrementalCachePeriodicFullSync(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/London")
	now := time.Date(2026, time.June, 2, 0, 0, 0, 0, loc)
	cachePath := filepath.Join(t.TempDir(), "calendar-cache.json")

	call := 0
	client, requests := newCacheTestClient(t, cachePath, &now, func(calendarRequest) (int, string) {
		call++
		return http.StatusOK, eventsPage(fmt.Sprintf("tok-%d", call),
			calendarEvent("e1", "first", now.Add(10*time.Hour), now.Add(11*time.Hour)))
	})
	if _, err := client.ListAll(context.Background(), "src", now, 7, "Europe/London", loc, Config{}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Minute)
	if _, err := client.ListAll(context.Background(), "src", now, 7, "Europe/London", loc, Config{}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	if _, err := client.ListAll(context.Background(), "src", now, 7, "Europe/London", loc, Config{}); err != nil {
		t.Fatal(err)
	}
	if len(*requests) != 3 {
		t.Fatalf("requests = %+v", *requests)
	}
	if (*requests)[1].SyncToken != "tok-1" {
		t.Fatalf("second request = %+v", (*requests)[1])
	}
	if (*requests)[2].SyncToken != "" {
		t.Fatalf("periodic full sync request = %+v", (*requests)[2])
	}
}

func TestIncrementalCacheFailsClosedOnMalformedEvent(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/London")
	now := time.Date(2026, time.June, 2, 0, 0, 0, 0, loc)
	cachePath := filepath.Join(t.TempDir(), "calendar-cache.json")
	client, _ := newCacheTestClient(t, cachePath, &now, func(calendarRequest) (int, string) {
		return http.StatusOK, eventsPage("tok-1", map[string]any{
			"id": "bad", "status": "confirmed",
			"start": map[string]any{"dateTime": "not-a-time"},
			"end":   map[string]any{"dateTime": "not-a-time"},
		})
	})
	if _, err := client.ListAll(context.Background(), "src", now, 7, "Europe/London", loc, Config{}); err == nil {
		t.Fatal("expected malformed cached event to fail closed")
	}
}

func TestIncrementalCacheFiltersHorizon(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/London")
	now := time.Date(2026, time.June, 2, 0, 0, 0, 0, loc)
	cachePath := filepath.Join(t.TempDir(), "calendar-cache.json")
	client, _ := newCacheTestClient(t, cachePath, &now, func(calendarRequest) (int, string) {
		return http.StatusOK, eventsPage("tok-1",
			calendarEvent("past", "past", now.Add(-48*time.Hour), now.Add(-47*time.Hour)),
			calendarEvent("in", "in", now.Add(10*time.Hour), now.Add(11*time.Hour)),
			calendarEvent("far", "far", now.AddDate(0, 0, 60), now.AddDate(0, 0, 61)),
		)
	})
	events, err := client.ListAll(context.Background(), "src", now, 7, "Europe/London", loc, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != "in" {
		t.Fatalf("horizon events = %+v", events)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestIncrementalCacheTokenPersistsAcrossClients(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/London")
	now := time.Date(2026, time.June, 2, 0, 0, 0, 0, loc)
	cachePath := filepath.Join(t.TempDir(), "calendar-cache.json")
	call := 0
	responder := func(calendarRequest) (int, string) {
		call++
		return http.StatusOK, eventsPage(fmt.Sprintf("tok-%d", call),
			calendarEvent("e1", "first", now.Add(10*time.Hour), now.Add(11*time.Hour)))
	}
	first, _ := newCacheTestClient(t, cachePath, &now, responder)
	if _, err := first.ListAll(context.Background(), "src", now, 7, "Europe/London", loc, Config{}); err != nil {
		t.Fatal(err)
	}
	second, requests := newCacheTestClient(t, cachePath, &now, responder)
	if _, err := second.ListAll(context.Background(), "src", now, 7, "Europe/London", loc, Config{}); err != nil {
		t.Fatal(err)
	}
	if len(*requests) != 1 || (*requests)[0].SyncToken != "tok-1" {
		t.Fatalf("requests = %+v", *requests)
	}
}

func TestIncrementalCacheTokenStringIsNotLeakedInErrors(t *testing.T) {
	// Guards against accidentally putting the token into an error message in a
	// future refactor: tokens are credentials for the sync stream.
	loc, _ := time.LoadLocation("Europe/London")
	now := time.Date(2026, time.June, 2, 0, 0, 0, 0, loc)
	client, _ := newCacheTestClient(t, filepath.Join(t.TempDir(), "cache.json"), &now, func(calendarRequest) (int, string) {
		return http.StatusInternalServerError, `{"error":{"code":500,"message":"boom"}}`
	})
	_, err := client.ListAll(context.Background(), "src", now, 7, "Europe/London", loc, Config{})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "tok-") {
		t.Fatalf("error leaked a sync token: %v", err)
	}
}

func TestIncrementalCacheCapturesTokenFromLastPage(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/London")
	now := time.Date(2026, time.June, 2, 0, 0, 0, 0, loc)
	cachePath := filepath.Join(t.TempDir(), "calendar-cache.json")

	page := 0
	client, _ := newCacheTestClient(t, cachePath, &now, func(request calendarRequest) (int, string) {
		page++
		if page == 1 {
			return http.StatusOK, `{"items":[` + mustJSON(t, calendarEvent("e1", "first", now.Add(10*time.Hour), now.Add(11*time.Hour))) + `],"nextPageToken":"page-2"}`
		}
		return http.StatusOK, `{"items":[` + mustJSON(t, calendarEvent("e2", "second", now.Add(12*time.Hour), now.Add(13*time.Hour))) + `],"nextSyncToken":"tok-last"}`
	})
	events, err := client.ListAll(context.Background(), "src", now, 7, "Europe/London", loc, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v", events)
	}
	var cache calendarCache
	if err := json.Unmarshal(mustReadFile(t, cachePath), &cache); err != nil {
		t.Fatal(err)
	}
	if cache.Calendars["src"].SyncToken != "tok-last" {
		t.Fatalf("cache token = %q", cache.Calendars["src"].SyncToken)
	}
}

func TestIncrementalCacheVersionMismatchRebuilds(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/London")
	now := time.Date(2026, time.June, 2, 0, 0, 0, 0, loc)
	cachePath := filepath.Join(t.TempDir(), "calendar-cache.json")
	old := `{"version":999,"calendars":{"src":{"syncToken":"stale","events":{}}}}`
	if err := os.WriteFile(cachePath, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	client, requests := newCacheTestClient(t, cachePath, &now, func(calendarRequest) (int, string) {
		return http.StatusOK, eventsPage("tok-new",
			calendarEvent("e1", "first", now.Add(10*time.Hour), now.Add(11*time.Hour)))
	})
	if _, err := client.ListAll(context.Background(), "src", now, 7, "Europe/London", loc, Config{}); err != nil {
		t.Fatal(err)
	}
	if len(*requests) != 1 || (*requests)[0].SyncToken != "" {
		t.Fatalf("requests = %+v", *requests)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestIncrementalCacheWriteFailureIsCounted(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/London")
	now := time.Date(2026, time.June, 2, 0, 0, 0, 0, loc)
	cacheDir := filepath.Join(t.TempDir(), "cache-is-a-directory")
	if err := os.Mkdir(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	client, _ := newCacheTestClient(t, cacheDir, &now, func(calendarRequest) (int, string) {
		return http.StatusOK, eventsPage("tok-1",
			calendarEvent("e1", "first", now.Add(10*time.Hour), now.Add(11*time.Hour)))
	})
	registry := newFakeMetrics()
	client.opts.Metrics = registry
	if _, err := client.ListAll(context.Background(), "src", now, 7, "Europe/London", loc, Config{}); err != nil {
		t.Fatal(err)
	}
	rendered := registry.Render()
	if !strings.Contains(rendered, `calendar_agent_calendar_cache_write_failures_total 1`) {
		t.Fatalf("metrics missing write failure:\n%s", rendered)
	}
	if !strings.Contains(rendered, `calendar_agent_calendar_cache_resyncs_total{reason="corrupt"} 1`) {
		t.Fatalf("metrics missing corrupt resync:\n%s", rendered)
	}
}
