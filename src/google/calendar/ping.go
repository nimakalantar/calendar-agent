package gcalendar

import (
	"context"
	"fmt"
	"strings"
)

// Ping verifies that the stored token can read the destination calendar. It
// uses events.list with a one-item page so the same OAuth scope as a sync is
// exercised.
func (c *Client) Ping(ctx context.Context, calendarID string) error {
	if strings.TrimSpace(calendarID) == "" {
		return fmt.Errorf("destination calendar is not configured")
	}
	if _, err := c.service.Events.List(calendarID).MaxResults(1).Context(ctx).Do(); err != nil {
		return fmt.Errorf("calendar read failed: %w", err)
	}
	return nil
}
