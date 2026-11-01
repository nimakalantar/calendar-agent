package gcalendar

import (
	"context"

	"google.golang.org/api/calendar/v3"
)

// CalendarDescriptor is an entry from the calendar list.
type CalendarDescriptor struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

// ListCalendars returns the user's calendar list.
func (c *Client) ListCalendars(ctx context.Context) ([]CalendarDescriptor, error) {
	return withRetry(ctx, func(ctx context.Context) ([]CalendarDescriptor, error) {
		call := c.service.CalendarList.List().MaxResults(250)
		return collectPages(ctx, call.Pages, func(page *calendar.CalendarList) []CalendarDescriptor {
			result := make([]CalendarDescriptor, 0, len(page.Items))
			for _, item := range page.Items {
				result = append(result, CalendarDescriptor{ID: item.Id, Summary: item.Summary})
			}
			return result
		})
	})
}
