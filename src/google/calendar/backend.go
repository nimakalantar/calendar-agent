package gcalendar

import (
	"context"
	"time"

	"github.com/nimakalantar/calendar-agent/src/calendaragent"
	"google.golang.org/api/calendar/v3"
)

var _ calendaragent.Backend = (*Backend)(nil)

// Backend implements calendaragent.Backend against Google Calendar.
type Backend struct {
	client *Client
	cfg    Config
	loc    *time.Location
}

// NewBackend builds a Google Calendar backend for one destination calendar.
func NewBackend(client *Client, cfg Config) *Backend {
	if client != nil {
		cfg.Ownership = client.Ownership()
	}
	cfg = cfg.normalized()
	return &Backend{client: client, cfg: cfg, loc: cfg.Location}
}

// Config returns the normalised backend configuration.
func (b *Backend) Config() Config {
	return b.cfg
}

// ListManaged implements calendaragent.Backend.
func (b *Backend) ListManaged(ctx context.Context, horizonStart time.Time, days int) ([]calendaragent.ManagedEvent, error) {
	return b.client.ListManaged(ctx, b.cfg.CalendarID, horizonStart, days, b.cfg.Timezone, b.loc, b.cfg)
}

// ListAll lists every event in the destination calendar for the horizon.
func (b *Backend) ListAll(ctx context.Context, horizonStart time.Time, days int) ([]calendaragent.ManagedEvent, error) {
	return b.client.ListAll(ctx, b.cfg.CalendarID, horizonStart, days, b.cfg.Timezone, b.loc, b.cfg)
}

// Create implements calendaragent.Backend.
func (b *Backend) Create(ctx context.Context, event calendaragent.Event) (string, error) {
	payload := b.eventPayload(event)
	payload.Id = b.cfg.ID(event)
	return b.client.CreateEvent(ctx, b.cfg.CalendarID, payload)
}

// Update implements calendaragent.Backend.
func (b *Backend) Update(ctx context.Context, eventID string, event calendaragent.Event) (string, error) {
	return b.client.UpdateEvent(ctx, b.cfg.CalendarID, eventID, b.eventPayload(event))
}

// Delete implements calendaragent.Backend.
func (b *Backend) Delete(ctx context.Context, eventID string) error {
	return b.client.DeleteEvent(ctx, b.cfg.CalendarID, eventID)
}

func (b *Backend) eventPayload(event calendaragent.Event) *calendar.Event {
	presentation := b.cfg.Presentation(event)
	schema := b.cfg.Schema
	startTimeZone := presentation.StartTimeZone
	if startTimeZone == "" {
		startTimeZone = b.cfg.Timezone
	}
	endTimeZone := presentation.EndTimeZone
	if endTimeZone == "" {
		endTimeZone = b.cfg.Timezone
	}

	properties := map[string]string{
		schema.ManagedBy:   b.cfg.Ownership.Value,
		schema.Key:         event.Key,
		schema.SourceKey:   event.SourceKey,
		schema.Kind:        event.Kind,
		schema.Link:        event.Link,
		schema.ContentHash: b.cfg.Hash(event),
	}
	if event.Link != "" {
		properties[schema.LinkHash] = calendaragent.Fingerprint(event.Link)
	}
	for key, value := range properties {
		if value == "" {
			delete(properties, key)
		}
	}

	payload := &calendar.Event{
		Summary:     event.Summary,
		Description: event.Description,
		Location:    event.Location,
		Start: &calendar.EventDateTime{
			DateTime: event.Start.In(b.loc).Format(time.RFC3339),
			TimeZone: startTimeZone,
		},
		End: &calendar.EventDateTime{
			DateTime: event.End.In(b.loc).Format(time.RFC3339),
			TimeZone: endTimeZone,
		},
		Transparency: string(presentation.Transparency),
		ExtendedProperties: &calendar.EventExtendedProperties{
			Private: properties,
		},
	}
	if presentation.ColorID != "" {
		payload.ColorId = presentation.ColorID
	}
	if !presentation.UseDefaultReminders && len(presentation.Reminders) > 0 {
		reminders := make([]*calendar.EventReminder, 0, len(presentation.Reminders))
		for _, reminder := range presentation.Reminders {
			reminders = append(reminders, &calendar.EventReminder{
				Method: reminder.Method, Minutes: int64(reminder.Minutes),
			})
		}
		payload.Reminders = &calendar.EventReminders{UseDefault: false, Overrides: reminders}
	}
	return payload
}
