package gcalendar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/option"

	"github.com/nimakalantar/calendar-agent/src/calendaragent"
	"google.golang.org/api/calendar/v3"
)

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	svc, err := calendar.NewService(context.Background(), option.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	svc.BasePath = server.URL + "/"
	return &Client{service: svc, opts: ClientOptions{Ownership: testOwnership, MetricsPrefix: "calendar_agent"}}
}

func TestListAllParses(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/calendars/src/events") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"id": "e1", "summary": "Meeting", "location": "Office",
				"start":     map[string]any{"dateTime": "2026-06-02T09:00:00+01:00"},
				"end":       map[string]any{"dateTime": "2026-06-02T10:00:00+01:00"},
				"attendees": []map[string]any{{"self": true, "responseStatus": "accepted"}},
			}},
		})
	}))

	loc, _ := time.LoadLocation("Europe/London")
	horizon := time.Date(2026, time.June, 1, 0, 0, 0, 0, loc)
	events, err := client.ListAll(context.Background(), "src", horizon, 7, "Europe/London", loc, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].ID != "e1" || events[0].Summary != "Meeting" {
		t.Fatalf("event = %+v", events[0])
	}
}

func TestListAllRejectsUnusableTimes(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/calendars/src/events") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"id": "malformed", "start": map[string]any{"dateTime": "not-a-time"},
				"end": map[string]any{"dateTime": "2026-06-02T10:00:00+01:00"},
			}},
		})
	}))
	loc, _ := time.LoadLocation("Europe/London")
	horizon := time.Date(2026, time.June, 1, 0, 0, 0, 0, loc)
	if _, err := client.ListAll(context.Background(), "src", horizon, 7, "Europe/London", loc, Config{}); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("malformed source event should fail the list, got %v", err)
	}
}

func TestListManagedParses(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/calendars/dst/events") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"id":           "g1",
				"start":        map[string]any{"dateTime": "2026-06-02T08:30:00+01:00"},
				"end":          map[string]any{"dateTime": "2026-06-02T09:00:00+01:00"},
				"transparency": "opaque",
				"colorId":      "4",
				"reminders": map[string]any{
					"useDefault": false,
					"overrides":  []map[string]any{{"method": "popup", "minutes": 5}},
				},
				"extendedProperties": map[string]any{
					"private": map[string]any{
						"managedBy":     "calendar-agent",
						"sourceEventId": "e1",
						"planKey":       "arrival:e1",
						"journeyKind":   "outward",
						"contentHash":   "abc",
					},
				},
			}},
		})
	}))

	loc, _ := time.LoadLocation("Europe/London")
	horizon := time.Date(2026, time.June, 1, 0, 0, 0, 0, loc)
	events, err := client.ListManaged(context.Background(), "dst", horizon, 7, "Europe/London", loc, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || !events[0].Owned || events[0].ContentHash != "abc" {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Kind != "outward" {
		t.Fatalf("kind = %q, want outward", events[0].Kind)
	}
	presentation := events[0].Presentation
	if presentation.Transparency != calendaragent.TransparencyOpaque || presentation.ColorID != "4" ||
		presentation.UseDefaultReminders || len(presentation.Reminders) != 1 ||
		presentation.Reminders[0] != (calendaragent.Reminder{Method: "popup", Minutes: 5}) {
		t.Fatalf("presentation = %+v", presentation)
	}
}

func TestListCalendarsParses(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/users/me/calendarList") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"id": "primary", "summary": "Home"},
				{"id": "imported", "summary": "Calendar", "summaryOverride": "Work"},
			},
		})
	}))

	calendars, err := client.ListCalendars(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(calendars) != 2 || calendars[0].ID != "primary" || calendars[0].Summary != "Home" {
		t.Fatalf("calendars = %+v", calendars)
	}
	if calendars[1].Summary != "Work" {
		t.Fatalf("summaryOverride not applied: %+v", calendars[1])
	}
}

func TestCreateAndDeleteEvent(t *testing.T) {
	var created bool
	var deleted bool
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/calendars/dst/events"):
			created = true
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "new"})
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/events/new"):
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))

	id, err := client.CreateEvent(context.Background(), "dst", &calendar.Event{Summary: "Event"})
	if err != nil || id != "new" {
		t.Fatalf("create = %q, %v", id, err)
	}
	if err := client.DeleteEvent(context.Background(), "dst", "new"); err != nil {
		t.Fatal(err)
	}
	if !created || !deleted {
		t.Fatalf("created=%v deleted=%v", created, deleted)
	}
}

func TestListCalendarsFollowsPages(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/users/me/calendarList") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("pageToken") == "next" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{{"id": "second", "summary": "Second"}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"nextPageToken": "next",
			"items":         []map[string]any{{"id": "first", "summary": "First"}},
		})
	}))

	calendars, err := client.ListCalendars(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(calendars) != 2 || calendars[0].ID != "first" || calendars[1].ID != "second" {
		t.Fatalf("calendars = %+v", calendars)
	}
}

func TestListAllFollowsPages(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/calendars/src/events") {
			http.NotFound(w, r)
			return
		}
		second := r.URL.Query().Get("pageToken") == "next"
		item := map[string]any{
			"id":    "e1",
			"start": map[string]any{"dateTime": "2026-06-02T09:00:00+01:00"},
			"end":   map[string]any{"dateTime": "2026-06-02T10:00:00+01:00"},
		}
		if second {
			item["id"] = "e2"
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{item}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"nextPageToken": "next", "items": []map[string]any{item}})
	}))

	loc, _ := time.LoadLocation("Europe/London")
	horizon := time.Date(2026, time.June, 1, 0, 0, 0, 0, loc)
	events, err := client.ListAll(context.Background(), "src", horizon, 7, "Europe/London", loc, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].ID != "e1" || events[1].ID != "e2" {
		t.Fatalf("events = %+v", events)
	}
}

func TestCreateEventTreatsDeterministicIDConflictAsSuccess(t *testing.T) {
	requests := 0
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/calendars/dst/events") {
			http.NotFound(w, r)
			return
		}
		requests++
		var payload calendar.Event
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode insert payload: %v", err)
			return
		}
		if payload.Id != "stable-event-id" {
			t.Errorf("insert ID = %q, want stable-event-id", payload.Id)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": http.StatusConflict, "message": "duplicate"},
		})
	}))

	id, err := client.CreateEvent(context.Background(), "dst", &calendar.Event{Id: "stable-event-id"})
	if err != nil || id != "stable-event-id" {
		t.Fatalf("create after conflict = %q, %v", id, err)
	}
	if requests != 1 {
		t.Fatalf("insert requests = %d, want one deterministic insert", requests)
	}
}

func TestCreateEventWithoutIDDoesNotBlindlyRetry(t *testing.T) {
	requests := 0
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "temporary failure", http.StatusInternalServerError)
	}))
	_, err := client.CreateEvent(context.Background(), "dst", &calendar.Event{Summary: "Event"})
	if err == nil {
		t.Fatal("expected insert failure")
	}
	if requests != 1 {
		t.Fatalf("server-assigned-ID inserts attempted %d times, want once", requests)
	}
}

func TestUpdateEventFallsBackToRequestedID(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.HasSuffix(r.URL.Path, "/calendars/dst/events/known") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))

	id, err := client.UpdateEvent(context.Background(), "dst", "known", &calendar.Event{Summary: "Event"})
	if err != nil || id != "known" {
		t.Fatalf("update = %q, %v", id, err)
	}
}

func TestDeleteMissingEventIsIdempotent(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	if err := client.DeleteEvent(context.Background(), "dst", "already-gone"); err != nil {
		t.Fatalf("delete of missing event = %v, want success", err)
	}
}

func TestListManagedEventIDsFiltersUnownedEvents(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/calendars/dst/events") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"id": "owned", "extendedProperties": map[string]any{"private": map[string]any{"managedBy": "calendar-agent"}}},
				{"id": "foreign", "extendedProperties": map[string]any{"private": map[string]any{"managedBy": "other"}}},
				{"id": "plain"},
			},
		})
	}))

	ids, err := client.ListManagedEventIDs(context.Background(), "dst")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "owned" {
		t.Fatalf("ids = %+v", ids)
	}
}
