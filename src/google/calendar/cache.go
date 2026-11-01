package gcalendar

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"time"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"

	"github.com/nimakalantar/calendar-agent/src/calendaragent"
	"github.com/nimakalantar/calendar-agent/src/platform/fsutil"
)

const (
	calendarCacheVersion  = 1
	calendarCachePageSize = 2500
	// Full sync fetches history (pruned below after 90 days) and enough future
	// instances to cover a maximum write horizon, even if the server truncates
	// recurring-event expansion. Omitting timeMin keeps long-running events
	// that started before the cache existed visible to planning.
	calendarCachePastDays   = 90
	calendarCacheFutureDays = 400
	// Cached events outside this window cannot affect a future sync. They are
	// pruned so an incremental cache does not grow forever.
	calendarCacheFutureRetentionDays = 730
)

type calendarCache struct {
	Version   int                            `json:"version"`
	Calendars map[string]*calendarCacheEntry `json:"calendars"`
}

type calendarCacheEntry struct {
	SyncToken      string                     `json:"syncToken,omitempty"`
	LastFullSyncAt *time.Time                 `json:"lastFullSyncAt,omitempty"`
	UpdatedAt      time.Time                  `json:"updatedAt,omitempty"`
	Events         map[string]*calendar.Event `json:"events"`
}

// listEventsWithCache uses a durable, sync-token-updated cache. The cache is
// per calendar and only ever advances its token after a complete page set has
// been applied, so a mid-request failure leaves the previous state intact.
func (c *Client) listEventsWithCache(ctx context.Context, calendarID string, managedOnly bool, timezone string, loc *time.Location, horizonStart time.Time, days int) ([]*calendar.Event, error) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	cache, corruptReason := c.loadCache()
	if corruptReason != "" {
		c.recordResync(corruptReason)
	}
	entry := cache.Calendars[calendarID]
	if entry == nil {
		entry = &calendarCacheEntry{Events: map[string]*calendar.Event{}}
		cache.Calendars[calendarID] = entry
	}
	if entry.Events == nil {
		entry.Events = map[string]*calendar.Event{}
	}

	now := c.opts.Now()
	mode := "incremental"
	needFull := entry.SyncToken == "" || entry.LastFullSyncAt == nil ||
		now.Sub(*entry.LastFullSyncAt) >= c.opts.FullSyncInterval
	if needFull {
		mode = "full"
		if err := c.fullSync(ctx, entry, calendarID, managedOnly, timezone, now); err != nil {
			return nil, err
		}
	} else {
		err := c.incrementalSync(ctx, entry, calendarID, managedOnly, timezone)
		if err != nil {
			var apiErr *googleapi.Error
			if errors.As(err, &apiErr) && apiErr.Code == http.StatusGone {
				// The token expired or related ACLs changed. Wipe and rebuild.
				slog.Warn("calendar sync token is no longer valid; performing a full resync")
				mode = "resync"
				c.recordResync("gone")
				if err := c.fullSync(ctx, entry, calendarID, managedOnly, timezone, now); err != nil {
					return nil, err
				}
			} else {
				return nil, err
			}
		}
	}

	pruneCalendarCache(entry, now, loc)
	entry.UpdatedAt = now
	if !c.saveCache(cache) {
		c.opts.Metrics.Inc(c.metricName("cache_write_failures_total"), nil)
	}
	c.recordCacheMetrics(cache, mode)
	events, err := filterEventsByHorizon(eventSlice(entry.Events), horizonStart, days, loc)
	if err != nil {
		return nil, err
	}
	if managedOnly {
		owned := events[:0]
		for _, item := range events {
			if isManagedEvent(item, c.opts.Ownership) {
				owned = append(owned, item)
			}
		}
		events = owned
	}
	return events, nil
}

func (c *Client) fullSync(ctx context.Context, entry *calendarCacheEntry, calendarID string, managedOnly bool, timezone string, now time.Time) error {
	call := c.service.Events.List(calendarID).
		SingleEvents(true).
		ShowDeleted(true).
		MaxResults(calendarCachePageSize).
		TimeMax(now.AddDate(0, 0, calendarCacheFutureDays).Format(time.RFC3339)).
		TimeZone(timezone)
	items, token, err := c.fetchEvents(ctx, call)
	if err != nil {
		return fmt.Errorf("full calendar sync: %w", err)
	}

	events := make(map[string]*calendar.Event, len(items))
	for _, item := range items {
		if err := applyCachedEvent(events, item, managedOnly, c.opts.Ownership); err != nil {
			return err
		}
	}
	entry.Events = events
	entry.SyncToken = token
	last := now
	entry.LastFullSyncAt = &last
	return nil
}

func (c *Client) incrementalSync(ctx context.Context, entry *calendarCacheEntry, calendarID string, managedOnly bool, timezone string) error {
	call := c.service.Events.List(calendarID).
		SingleEvents(true).
		ShowDeleted(true).
		MaxResults(calendarCachePageSize).
		SyncToken(entry.SyncToken).
		TimeZone(timezone)
	items, token, err := c.fetchEvents(ctx, call)
	if err != nil {
		return fmt.Errorf("incremental calendar sync: %w", err)
	}
	for _, item := range items {
		if err := applyCachedEvent(entry.Events, item, managedOnly, c.opts.Ownership); err != nil {
			return err
		}
	}
	if token != "" {
		entry.SyncToken = token
	}
	return nil
}

type fetchedEvents struct {
	items []*calendar.Event
	token string
}

func (c *Client) fetchEvents(ctx context.Context, call *calendar.EventsListCall) ([]*calendar.Event, string, error) {
	result, err := withRetry(ctx, func(ctx context.Context) (fetchedEvents, error) {
		var items []*calendar.Event
		token := ""
		err := call.Pages(ctx, func(page *calendar.Events) error {
			for _, item := range page.Items {
				if item == nil {
					return fmt.Errorf("calendar returned a nil event")
				}
				items = append(items, item)
			}
			if page.NextSyncToken != "" {
				token = page.NextSyncToken
			}
			return nil
		})
		if err != nil {
			return fetchedEvents{}, err
		}
		return fetchedEvents{items: items, token: token}, nil
	})
	if err != nil {
		return nil, "", err
	}
	return result.items, result.token, nil
}

func applyCachedEvent(events map[string]*calendar.Event, item *calendar.Event, managedOnly bool, ownership calendaragent.Ownership) error {
	if item == nil || item.Id == "" {
		return fmt.Errorf("calendar returned an event without an ID")
	}
	if item.Status == "cancelled" {
		delete(events, item.Id)
		return nil
	}
	if managedOnly && !isManagedEvent(item, ownership) {
		// The marker may have been removed from an event that used to be ours.
		delete(events, item.Id)
		return nil
	}
	events[item.Id] = item
	return nil
}

func isManagedEvent(item *calendar.Event, ownership calendaragent.Ownership) bool {
	return item.ExtendedProperties != nil &&
		item.ExtendedProperties.Private[ownership.Property] == ownership.Value
}

func eventSlice(events map[string]*calendar.Event) []*calendar.Event {
	result := make([]*calendar.Event, 0, len(events))
	for _, item := range events {
		result = append(result, item)
	}
	return result
}

func filterEventsByHorizon(events []*calendar.Event, horizonStart time.Time, days int, loc *time.Location) ([]*calendar.Event, error) {
	horizonEnd := horizonStart.AddDate(0, 0, days)
	type candidate struct {
		event *calendar.Event
		start time.Time
	}
	candidates := make([]candidate, 0, len(events))
	for _, item := range events {
		start, err := parseEventTime(item.Start, loc)
		if err != nil {
			return nil, fmt.Errorf("calendar event %q has unusable start or end time: %w", item.Id, err)
		}
		if start.Before(horizonStart) || !start.Before(horizonEnd) {
			continue
		}
		// Validate the end only for events that could enter the plan, matching
		// the fail-closed behaviour of the direct listing path.
		if _, err := parseEventTime(item.End, loc); err != nil {
			return nil, fmt.Errorf("calendar event %q has unusable start or end time: %w", item.Id, err)
		}
		candidates = append(candidates, candidate{event: item, start: start})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].start.Equal(candidates[j].start) {
			return candidates[i].event.Id < candidates[j].event.Id
		}
		return candidates[i].start.Before(candidates[j].start)
	})
	result := make([]*calendar.Event, len(candidates))
	for i, item := range candidates {
		result[i] = item.event
	}
	return result, nil
}

func pruneCalendarCache(entry *calendarCacheEntry, now time.Time, loc *time.Location) {
	pastCutoff := now.AddDate(0, 0, -calendarCachePastDays)
	futureCutoff := now.AddDate(0, 0, calendarCacheFutureRetentionDays)
	for id, item := range entry.Events {
		start, end, ok := eventTimes(item, loc)
		if !ok {
			// Keep unparseable entries so the horizon conversion fails closed
			// instead of silently hiding a source event.
			continue
		}
		if end.Before(pastCutoff) || start.After(futureCutoff) {
			delete(entry.Events, id)
		}
	}
}

func (c *Client) loadCache() (calendarCache, string) {
	empty := func() (calendarCache, string) {
		return calendarCache{Version: calendarCacheVersion, Calendars: map[string]*calendarCacheEntry{}}, ""
	}
	if c.opts.CachePath == "" {
		return empty()
	}
	var cache calendarCache
	if err := fsutil.ReadJSON(c.opts.CachePath, &cache); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return empty()
		}
		slog.Warn("failed to read calendar cache; a full sync will rebuild it", "path", c.opts.CachePath, "error", err)
		return calendarCache{Version: calendarCacheVersion, Calendars: map[string]*calendarCacheEntry{}}, "corrupt"
	}
	if cache.Version != calendarCacheVersion || cache.Calendars == nil {
		return calendarCache{Version: calendarCacheVersion, Calendars: map[string]*calendarCacheEntry{}}, "version"
	}
	for id, entry := range cache.Calendars {
		if entry == nil {
			delete(cache.Calendars, id)
			continue
		}
		if entry.Events == nil {
			entry.Events = map[string]*calendar.Event{}
		}
		for eventID, item := range entry.Events {
			if item == nil || item.Id == "" {
				delete(entry.Events, eventID)
			}
		}
	}
	return cache, ""
}

func (c *Client) saveCache(cache calendarCache) bool {
	if c.opts.CachePath == "" {
		return true
	}
	cache.Version = calendarCacheVersion
	if err := fsutil.WriteJSONAtomic(c.opts.CachePath, cache, 0o600); err != nil {
		// A failed write leaves the previous token on disk; the next run
		// re-fetches changes since that token, which is safe.
		slog.Warn("failed to persist calendar cache", "path", c.opts.CachePath, "error", err)
		return false
	}
	return true
}

func (c *Client) recordResync(reason string) {
	if c.opts.Metrics == nil {
		return
	}
	c.opts.Metrics.Inc(c.metricName("cache_resyncs_total"), map[string]string{"reason": reason})
}

func (c *Client) recordCacheMetrics(cache calendarCache, mode string) {
	if c.opts.Metrics == nil {
		return
	}
	c.opts.Metrics.Inc(c.metricName("cache_syncs_total"), map[string]string{"mode": mode})
	total := 0
	var oldestFullSync *time.Time
	for _, entry := range cache.Calendars {
		total += len(entry.Events)
		if entry.LastFullSyncAt != nil && (oldestFullSync == nil || entry.LastFullSyncAt.Before(*oldestFullSync)) {
			value := *entry.LastFullSyncAt
			oldestFullSync = &value
		}
	}
	c.opts.Metrics.Set(c.metricName("cache_events"), float64(total), nil)
	if oldestFullSync != nil {
		c.opts.Metrics.Set(c.metricName("cache_last_full_sync_timestamp_seconds"), float64(oldestFullSync.Unix()), nil)
	}
}
