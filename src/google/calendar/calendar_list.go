package gcalendar

import (
	"context"
	"fmt"
	"time"

	"github.com/nimakalantar/calendar-agent/src/calendaragent"
	"google.golang.org/api/calendar/v3"
)

// collectPages drives a Google API paginated call and concatenates the items
// extracted from each page.
func collectPages[Page, Item any](ctx context.Context, pages func(context.Context, func(Page) error) error, extract func(Page) []Item) ([]Item, error) {
	var result []Item
	err := pages(ctx, func(page Page) error {
		result = append(result, extract(page)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// mapEvents lists and converts events, failing closed if any item cannot be
// decoded so callers never reconcile against an incomplete calendar view.
func mapEvents[T any](list func() ([]*calendar.Event, error), convert func(*calendar.Event) (T, bool)) ([]T, error) {
	events, err := list()
	if err != nil {
		return nil, err
	}
	var result []T
	for _, item := range events {
		if item == nil {
			return nil, fmt.Errorf("calendar returned a nil event")
		}
		value, ok := convert(item)
		if !ok {
			return nil, fmt.Errorf("calendar event %q has unusable start or end time", item.Id)
		}
		result = append(result, value)
	}
	return result, nil
}

func (c *Client) listEvents(ctx context.Context, calendarID string, horizonStart time.Time, days int, timezone string, loc *time.Location, managedOnly bool) ([]*calendar.Event, error) {
	if c.opts.IncrementalSync && c.opts.CachePath != "" {
		return c.listEventsWithCache(ctx, calendarID, managedOnly, timezone, loc, horizonStart, days)
	}
	return c.listEventsWindow(ctx, calendarID, horizonStart, days, timezone, managedOnly)
}

func (c *Client) listEventsWindow(ctx context.Context, calendarID string, horizonStart time.Time, days int, timezone string, managedOnly bool) ([]*calendar.Event, error) {
	horizonEnd := horizonStart.AddDate(0, 0, days)
	return withRetry(ctx, func(ctx context.Context) ([]*calendar.Event, error) {
		call := c.service.Events.List(calendarID).
			SingleEvents(true).
			OrderBy("startTime").
			TimeMin(horizonStart.Format(time.RFC3339)).
			TimeMax(horizonEnd.Format(time.RFC3339)).
			TimeZone(timezone)
		if managedOnly {
			call = call.PrivateExtendedProperty(c.opts.Ownership.Filter())
		}
		return collectPages(ctx, call.Pages, func(page *calendar.Events) []*calendar.Event {
			return page.Items
		})
	})
}

// ListRawEvents returns the raw Google events of a calendar in the horizon. It
// is the escape hatch for applications that need provider fields (recurrence,
// attendees, extended properties) not represented by calendaragent.ManagedEvent.
func (c *Client) ListRawEvents(ctx context.Context, calendarID string, horizonStart time.Time, days int, timezone string, loc *time.Location, managedOnly bool) ([]*calendar.Event, error) {
	return c.listEvents(ctx, calendarID, horizonStart, days, timezone, loc, managedOnly)
}

// ListManaged returns events carrying this client's ownership marker, mapped to
// the provider-independent form.
func (c *Client) ListManaged(ctx context.Context, calendarID string, horizonStart time.Time, days int, timezone string, loc *time.Location, cfg Config) ([]calendaragent.ManagedEvent, error) {
	cfg = cfg.normalized()
	cfg.Ownership = c.opts.Ownership
	return mapEvents(
		func() ([]*calendar.Event, error) {
			return c.listEvents(ctx, calendarID, horizonStart, days, timezone, loc, true)
		},
		func(item *calendar.Event) (calendaragent.ManagedEvent, bool) {
			return toManagedEvent(item, loc, cfg)
		},
	)
}

// ListAll returns every event in the horizon, owned or not. Applications use it
// to build busy intervals from monitored calendars.
func (c *Client) ListAll(ctx context.Context, calendarID string, horizonStart time.Time, days int, timezone string, loc *time.Location, cfg Config) ([]calendaragent.ManagedEvent, error) {
	cfg = cfg.normalized()
	cfg.Ownership = c.opts.Ownership
	return mapEvents(
		func() ([]*calendar.Event, error) {
			return c.listEvents(ctx, calendarID, horizonStart, days, timezone, loc, false)
		},
		func(item *calendar.Event) (calendaragent.ManagedEvent, bool) {
			return toManagedEvent(item, loc, cfg)
		},
	)
}
