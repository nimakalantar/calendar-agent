package gcalendar

import (
	"testing"
	"time"

	"github.com/nimakalantar/calendar-agent/src/calendaragent"
	"google.golang.org/api/calendar/v3"
)

func TestBackendPayloadWritesSchemaAndPresentation(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/London")
	start := time.Date(2026, time.June, 2, 9, 0, 0, 0, loc)
	cfg := testConfig()
	cfg.CalendarID = "dst"
	cfg.Timezone = "Europe/London"
	cfg.Location = loc
	cfg.DefaultPresentation = calendaragent.Presentation{
		Transparency:        calendaragent.TransparencyOpaque,
		ColorID:             "4",
		UseDefaultReminders: false,
		Reminders:           []calendaragent.Reminder{{Method: "popup", Minutes: 5}},
	}
	backend := NewBackend(nil, cfg)

	event := calendaragent.Event{
		Key:         "arrival:e1",
		SourceKey:   "e1",
		Start:       start,
		End:         start.Add(30 * time.Minute),
		Summary:     "Event: Home -> Office",
		Description: "Route detail",
		Location:    "Office",
		Kind:        "outward",
		Link:        "https://otp/1",
		ContentHash: "hash-1",
	}
	payload := backend.eventPayload(event)

	if payload.Summary != event.Summary || payload.Description != event.Description || payload.Location != "Office" {
		t.Fatalf("payload text = %q/%q", payload.Summary, payload.Description)
	}
	if payload.Start.TimeZone != "Europe/London" || payload.End.TimeZone != "Europe/London" {
		t.Fatalf("timezones = %+v / %+v", payload.Start, payload.End)
	}
	if payload.Transparency != "opaque" || payload.ColorId != "4" {
		t.Fatalf("presentation = %q/%q", payload.Transparency, payload.ColorId)
	}
	if payload.Reminders == nil || payload.Reminders.UseDefault || len(payload.Reminders.Overrides) != 1 {
		t.Fatalf("reminders = %+v", payload.Reminders)
	}
	props := payload.ExtendedProperties.Private
	for key, want := range map[string]string{
		"managedBy":     "calendar-agent",
		"planKey":       "arrival:e1",
		"sourceEventId": "e1",
		"journeyKind":   "outward",
		"searchUri":     "https://otp/1",
		"contentHash":   "hash-1",
	} {
		if props[key] != want {
			t.Fatalf("property %q = %q, want %q (all: %+v)", key, props[key], want, props)
		}
	}
	if props["searchUriHash"] != calendaragent.Fingerprint("https://otp/1") {
		t.Fatalf("searchUriHash = %q", props["searchUriHash"])
	}
}

func TestBackendDeterministicID(t *testing.T) {
	cfg := testConfig()
	backend := NewBackend(nil, cfg)
	event := calendaragent.Event{Key: "arrival:event-1"}
	first := backend.cfg.ID(event)
	second := backend.cfg.ID(event)
	if first != second || len(first) != 64 {
		t.Fatalf("ID = %q, %q; want a stable 64-character hash", first, second)
	}
	for _, char := range first {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			t.Fatalf("ID contains non-hex character %q", char)
		}
	}
}

func TestBackendUsesDefaultPresentationOnlyWhenEventHasNone(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/London")
	cfg := testConfig()
	cfg.Location = loc
	cfg.DefaultPresentation = calendaragent.Presentation{Transparency: calendaragent.TransparencyTransparent}
	backend := NewBackend(nil, cfg)
	start := time.Date(2026, time.June, 2, 9, 0, 0, 0, loc)

	inherited := backend.eventPayload(calendaragent.Event{Start: start, End: start.Add(time.Hour)})
	if inherited.Transparency != "transparent" {
		t.Fatalf("default presentation not applied: %+v", inherited)
	}

	explicit := backend.eventPayload(calendaragent.Event{
		Start: start, End: start.Add(time.Hour),
		Presentation: calendaragent.Presentation{Transparency: calendaragent.TransparencyOpaque},
	})
	if explicit.Transparency != "opaque" {
		t.Fatalf("explicit presentation overridden: %+v", explicit)
	}
}

func TestToManagedEventMapsSchema(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/London")
	start := time.Date(2026, time.June, 2, 9, 0, 0, 0, loc)
	item := &calendar.Event{
		Id:           "g1",
		Summary:      "Event",
		Description:  "desc",
		Location:     "Office",
		Status:       "cancelled",
		Transparency: "opaque",
		Attendees:    []*calendar.EventAttendee{{Self: true, ResponseStatus: "declined"}},
		ColorId:      "4",
		Start:        &calendar.EventDateTime{DateTime: start.Format(time.RFC3339), TimeZone: "Europe/London"},
		End:          &calendar.EventDateTime{DateTime: start.Add(30 * time.Minute).Format(time.RFC3339), TimeZone: "Europe/London"},
		ExtendedProperties: &calendar.EventExtendedProperties{Private: map[string]string{
			"managedBy":     "calendar-agent",
			"planKey":       "arrival:e1",
			"sourceEventId": "e1",
			"journeyKind":   "outward",
			"searchUri":     "https://otp/1",
			"searchUriHash": "link-hash",
			"contentHash":   "hash-1",
		}},
	}
	event, ok := toManagedEvent(item, loc, testConfig())
	if !ok {
		t.Fatal("event did not map")
	}
	if event.Key != "arrival:e1" || event.SourceKey != "e1" || event.Kind != "outward" {
		t.Fatalf("identity = %+v", event)
	}
	if event.Link != "https://otp/1" || event.LinkHash != "link-hash" || event.ContentHash != "hash-1" {
		t.Fatalf("metadata = %+v", event)
	}
	if !event.Owned {
		t.Fatal("event should be owned")
	}
	if event.Location != "Office" || event.AllDay || event.Status != "cancelled" || event.SelfResponse != "declined" {
		t.Fatalf("common fields = %+v", event)
	}
	if event.Presentation.Transparency != calendaragent.TransparencyOpaque || event.Presentation.ColorID != "4" {
		t.Fatalf("presentation = %+v", event.Presentation)
	}
}
