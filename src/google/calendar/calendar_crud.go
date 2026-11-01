package gcalendar

import (
	"context"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"
)

// --- CRUD -------------------------------------------------------------------

// CreateEvent inserts an event and returns its ID.
func (c *Client) CreateEvent(ctx context.Context, calendarID string, event *calendar.Event) (string, error) {
	insert := func(ctx context.Context) (string, error) {
		created, err := c.service.Events.Insert(calendarID, event).Do()
		if err != nil {
			// A conflict on a deterministic client-supplied ID means an earlier
			// attempt already created this desired event.
			if event.Id != "" && isConflict(err) {
				return event.Id, nil
			}
			return "", err
		}
		return created.Id, nil
	}
	if event.Id == "" {
		// Blind retries of server-assigned IDs can insert duplicate events when
		// the server commits but the response is lost.
		return insert(ctx)
	}
	return withRetry(ctx, insert)
}

// UpdateEvent replaces an event and returns its ID.
func (c *Client) UpdateEvent(ctx context.Context, calendarID, eventID string, event *calendar.Event) (string, error) {
	return withRetry(ctx, func(ctx context.Context) (string, error) {
		updated, err := c.service.Events.Update(calendarID, eventID, event).Do()
		if err != nil {
			return "", err
		}
		if updated.Id == "" {
			return eventID, nil
		}
		return updated.Id, nil
	})
}

// DeleteEvent removes an event.
func (c *Client) DeleteEvent(ctx context.Context, calendarID, eventID string) error {
	return retryVoid(ctx, func(ctx context.Context) error {
		err := c.service.Events.Delete(calendarID, eventID).Do()
		if IsGone(err) {
			return nil
		}
		return err
	})
}

// ListManagedEventIDs lists every owned event in a calendar.
func (c *Client) ListManagedEventIDs(ctx context.Context, calendarID string) ([]string, error) {
	ownership := c.opts.Ownership
	return withRetry(ctx, func(ctx context.Context) ([]string, error) {
		var ids []string
		call := c.service.Events.List(calendarID).
			MaxResults(2500).
			SingleEvents(false).
			ShowDeleted(false).
			Fields(googleapi.Field("nextPageToken,items(id,extendedProperties/private/" + ownership.Property + ")")).
			PrivateExtendedProperty(ownership.Filter())
		err := call.Pages(ctx, func(page *calendar.Events) error {
			for _, item := range page.Items {
				if item.ExtendedProperties != nil && item.ExtendedProperties.Private[ownership.Property] == ownership.Value {
					ids = append(ids, item.Id)
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return ids, nil
	})
}
