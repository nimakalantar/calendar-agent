package gcalendar_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/calendar/v3"

	"github.com/nimakalantar/calendar-agent/src/calendaragent"
	"github.com/nimakalantar/calendar-agent/src/google/auth"
	gcalendar "github.com/nimakalantar/calendar-agent/src/google/calendar"
)

var liveOwnership = calendaragent.Ownership{Property: "managedBy", Value: "calendar-agent"}

// TestLiveGoogleIncrementalSync is destructive only within two dedicated test
// calendars named by the environment. It is skipped unless explicitly enabled.
//
// Required:
//
//	CALENDAR_AGENT_LIVE_GOOGLE=1
//	CALENDAR_AGENT_GOOGLE_CREDENTIALS=/path/google_credentials.json
//	CALENDAR_AGENT_GOOGLE_TOKEN=/path/google_token.json
//	CALENDAR_AGENT_LIVE_SOURCE_CALENDAR=<calendar id>
//	CALENDAR_AGENT_LIVE_DEST_CALENDAR=<calendar id>
func TestLiveGoogleIncrementalSync(t *testing.T) {
	if os.Getenv("CALENDAR_AGENT_LIVE_GOOGLE") != "1" {
		t.Skip("set CALENDAR_AGENT_LIVE_GOOGLE=1 to run against real Google calendars")
	}
	sourceCalendar := os.Getenv("CALENDAR_AGENT_LIVE_SOURCE_CALENDAR")
	destCalendar := os.Getenv("CALENDAR_AGENT_LIVE_DEST_CALENDAR")
	if sourceCalendar == "" || destCalendar == "" {
		t.Fatal("CALENDAR_AGENT_LIVE_SOURCE_CALENDAR and CALENDAR_AGENT_LIVE_DEST_CALENDAR are required")
	}
	if sourceCalendar == destCalendar {
		t.Fatal("source and destination calendars must differ")
	}
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cachePath := filepath.Join(t.TempDir(), "calendar-cache.json")
	client := newLiveClient(t, cachePath, time.Hour)
	now := time.Now().In(loc)
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, loc)
	horizon := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	var cleanupIDs []string
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanupCancel()
		for _, id := range cleanupIDs {
			_ = client.DeleteEvent(cleanupCtx, sourceCalendar, id)
			_ = client.DeleteEvent(cleanupCtx, destCalendar, id)
		}
	})

	// Source events for the full sync.
	firstID := insertEvent(t, ctx, client, sourceCalendar, &calendar.Event{
		Summary:  "calendar-agent live " + suffix,
		Location: "51.51,-0.12",
		Start:    &calendar.EventDateTime{DateTime: tomorrow.Add(10 * time.Hour).Format(time.RFC3339), TimeZone: "Europe/London"},
		End:      &calendar.EventDateTime{DateTime: tomorrow.Add(11 * time.Hour).Format(time.RFC3339), TimeZone: "Europe/London"},
	})
	cleanupIDs = append(cleanupIDs, firstID)
	secondID := insertEvent(t, ctx, client, sourceCalendar, &calendar.Event{
		Summary:  "calendar-agent live second " + suffix,
		Location: "51.52,-0.13",
		Start:    &calendar.EventDateTime{DateTime: tomorrow.Add(12 * time.Hour).Format(time.RFC3339), TimeZone: "Europe/London"},
		End:      &calendar.EventDateTime{DateTime: tomorrow.Add(13 * time.Hour).Format(time.RFC3339), TimeZone: "Europe/London"},
	})
	cleanupIDs = append(cleanupIDs, secondID)

	// Destination events: one owned event and one unrelated personal event.
	managedID := insertEvent(t, ctx, client, destCalendar, &calendar.Event{
		Summary: "calendar-agent live managed " + suffix,
		Start:   &calendar.EventDateTime{DateTime: tomorrow.Add(9 * time.Hour).Format(time.RFC3339), TimeZone: "Europe/London"},
		End:     &calendar.EventDateTime{DateTime: tomorrow.Add(9*time.Hour + 30*time.Minute).Format(time.RFC3339), TimeZone: "Europe/London"},
		ExtendedProperties: &calendar.EventExtendedProperties{Private: map[string]string{
			"managedBy": "calendar-agent",
			"planKey":   "arrival:live-" + suffix,
		}},
	})
	cleanupIDs = append(cleanupIDs, managedID)
	personalID := insertEvent(t, ctx, client, destCalendar, &calendar.Event{
		Summary: "calendar-agent live personal " + suffix,
		Start:   &calendar.EventDateTime{DateTime: tomorrow.Add(15 * time.Hour).Format(time.RFC3339), TimeZone: "Europe/London"},
		End:     &calendar.EventDateTime{DateTime: tomorrow.Add(16 * time.Hour).Format(time.RFC3339), TimeZone: "Europe/London"},
	})
	cleanupIDs = append(cleanupIDs, personalID)

	// Full sync: both source events and only the managed destination event.
	source := listSource(t, ctx, client, sourceCalendar, horizon, loc)
	if _, ok := eventByID(source, firstID); !ok {
		t.Fatalf("full sync missing first source event %s", firstID)
	}
	if _, ok := eventByID(source, secondID); !ok {
		t.Fatalf("full sync missing second source event %s", secondID)
	}
	managed := listManaged(t, ctx, client, destCalendar, horizon, loc)
	if _, ok := managedByID(managed, managedID); !ok {
		t.Fatalf("full sync missing managed destination event %s", managedID)
	}
	if _, ok := managedByID(managed, personalID); ok {
		t.Fatalf("unmanaged destination event %s leaked into the managed view", personalID)
	}

	cacheBefore := readLiveCache(t, cachePath)
	sourceBefore, ok := cacheBefore.Calendars[sourceCalendar]
	if !ok || sourceBefore.SyncToken == "" || sourceBefore.LastFullSyncAt == nil {
		t.Fatalf("cache after full sync = %+v", cacheBefore)
	}

	// Incremental update and delete across a simulated process restart.
	updateEvent(t, ctx, client, sourceCalendar, firstID, &calendar.Event{
		Summary: "calendar-agent live updated " + suffix,
		Start:   &calendar.EventDateTime{DateTime: tomorrow.Add(10 * time.Hour).Format(time.RFC3339), TimeZone: "Europe/London"},
		End:     &calendar.EventDateTime{DateTime: tomorrow.Add(11 * time.Hour).Format(time.RFC3339), TimeZone: "Europe/London"},
	})
	if err := client.DeleteEvent(ctx, sourceCalendar, secondID); err != nil {
		t.Fatal(err)
	}
	cleanupIDs = removeID(cleanupIDs, secondID)

	restarted := newLiveClient(t, cachePath, time.Hour)
	source = listSource(t, ctx, restarted, sourceCalendar, horizon, loc)
	updated, ok := eventByID(source, firstID)
	if !ok || !strings.Contains(updated.Summary, "updated") {
		t.Fatalf("incremental sync did not apply update: %+v", updated)
	}
	if _, ok := eventByID(source, secondID); ok {
		t.Fatalf("incremental sync did not remove deleted event %s", secondID)
	}
	cacheAfter := readLiveCache(t, cachePath)
	sourceAfter := cacheAfter.Calendars[sourceCalendar]
	if sourceAfter.LastFullSyncAt == nil || !sourceAfter.LastFullSyncAt.Equal(*sourceBefore.LastFullSyncAt) {
		t.Fatalf("incremental run performed a full refresh: before=%v after=%v", sourceBefore.LastFullSyncAt, sourceAfter.LastFullSyncAt)
	}

	// Corrupt the token to force a 410 Gone full resync.
	corruptLiveToken(t, cachePath, sourceCalendar)
	resynced := newLiveClient(t, cachePath, time.Hour)
	source = listSource(t, ctx, resynced, sourceCalendar, horizon, loc)
	if _, ok := eventByID(source, firstID); !ok {
		t.Fatalf("410 resync lost the updated source event")
	}
	if _, ok := eventByID(source, secondID); ok {
		t.Fatalf("410 resync resurrected the deleted source event")
	}
	cacheResynced := readLiveCache(t, cachePath)
	if cacheResynced.Calendars[sourceCalendar].SyncToken == "" {
		t.Fatalf("410 resync did not store a new sync token")
	}

	// Recurring series deletion: the periodic full refresh is what removes
	// stale expanded instances when Google does not report the series object.
	recurringCache := filepath.Join(t.TempDir(), "recurring-cache.json")
	recurringClient := newLiveClient(t, recurringCache, time.Hour)
	seriesID := insertEvent(t, ctx, recurringClient, sourceCalendar, &calendar.Event{
		Summary:    "calendar-agent live recurring " + suffix,
		Start:      &calendar.EventDateTime{DateTime: tomorrow.Add(9 * time.Hour).Format(time.RFC3339), TimeZone: "Europe/London"},
		End:        &calendar.EventDateTime{DateTime: tomorrow.Add(9*time.Hour + 30*time.Minute).Format(time.RFC3339), TimeZone: "Europe/London"},
		Recurrence: []string{"RRULE:FREQ=DAILY;COUNT=3"},
	})
	cleanupIDs = append(cleanupIDs, seriesID)
	instances := listSource(t, ctx, recurringClient, sourceCalendar, horizon, loc)
	countInstances := func(events []calendaragent.ManagedEvent) int {
		count := 0
		for _, event := range events {
			if strings.Contains(event.Summary, "recurring "+suffix) {
				count++
			}
		}
		return count
	}
	if countInstances(instances) == 0 {
		t.Fatal("recurring event produced no instances")
	}
	if err := recurringClient.DeleteEvent(ctx, sourceCalendar, seriesID); err != nil {
		t.Fatal(err)
	}
	cleanupIDs = removeID(cleanupIDs, seriesID)

	fullRefresh := newLiveClient(t, recurringCache, time.Nanosecond)
	instances = listSource(t, ctx, fullRefresh, sourceCalendar, horizon, loc)
	if got := countInstances(instances); got != 0 {
		t.Fatalf("full refresh kept %d stale recurring instances", got)
	}
}

func newLiveClient(t *testing.T, cachePath string, fullSyncInterval time.Duration) *gcalendar.Client {
	t.Helper()
	// The client and token source outlive this helper, so they must not be
	// tied to a short-lived context.
	oauthClient, err := auth.LoadClient(os.Getenv("CALENDAR_AGENT_GOOGLE_CREDENTIALS"))
	if err != nil {
		t.Fatal(err)
	}
	tokenSource, err := auth.NewTokenSource(context.Background(), oauthClient.Config(""), os.Getenv("CALENDAR_AGENT_GOOGLE_TOKEN"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := gcalendar.NewClientWithOptions(context.Background(), tokenSource, "", gcalendar.ClientOptions{
		CachePath:        cachePath,
		IncrementalSync:  true,
		FullSyncInterval: fullSyncInterval,
		Ownership:        liveOwnership,
		MetricsPrefix:    "calendar_agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func insertEvent(t *testing.T, ctx context.Context, client *gcalendar.Client, calendarID string, event *calendar.Event) string {
	t.Helper()
	id, err := client.CreateEvent(ctx, calendarID, event)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func updateEvent(t *testing.T, ctx context.Context, client *gcalendar.Client, calendarID, eventID string, event *calendar.Event) {
	t.Helper()
	if _, err := client.UpdateEvent(ctx, calendarID, eventID, event); err != nil {
		t.Fatal(err)
	}
}

func listSource(t *testing.T, ctx context.Context, client *gcalendar.Client, calendarID string, horizon time.Time, loc *time.Location) []calendaragent.ManagedEvent {
	t.Helper()
	events, err := client.ListAll(ctx, calendarID, horizon, 7, "Europe/London", loc, gcalendar.Config{Ownership: liveOwnership})
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func listManaged(t *testing.T, ctx context.Context, client *gcalendar.Client, calendarID string, horizon time.Time, loc *time.Location) []calendaragent.ManagedEvent {
	t.Helper()
	events, err := client.ListManaged(ctx, calendarID, horizon, 7, "Europe/London", loc, gcalendar.Config{Ownership: liveOwnership})
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func eventByID(events []calendaragent.ManagedEvent, id string) (calendaragent.ManagedEvent, bool) {
	for _, event := range events {
		if event.ID == id {
			return event, true
		}
	}
	return calendaragent.ManagedEvent{}, false
}

func managedByID(events []calendaragent.ManagedEvent, id string) (calendaragent.ManagedEvent, bool) {
	for _, event := range events {
		if event.ID == id {
			return event, true
		}
	}
	return calendaragent.ManagedEvent{}, false
}

func removeID(ids []string, remove string) []string {
	result := ids[:0]
	for _, id := range ids {
		if id != remove {
			result = append(result, id)
		}
	}
	return result
}

type liveCache struct {
	Calendars map[string]struct {
		SyncToken      string     `json:"syncToken"`
		LastFullSyncAt *time.Time `json:"lastFullSyncAt"`
	} `json:"calendars"`
}

func readLiveCache(t *testing.T, path string) liveCache {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cache liveCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		t.Fatal(err)
	}
	return cache
}

func corruptLiveToken(t *testing.T, path, calendarID string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	calendars, ok := document["calendars"].(map[string]any)
	if !ok {
		t.Fatalf("cache has no calendars: %s", raw)
	}
	entry, ok := calendars[calendarID].(map[string]any)
	if !ok {
		t.Fatalf("cache has no entry for %s", calendarID)
	}
	entry["syncToken"] = "invalid-token-from-live-test"
	updated, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
}
