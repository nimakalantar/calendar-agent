package gcalendar

import (
	"context"
	"fmt"
	"io"
	"strings"

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

// WriteCalendarList prints the available calendars in a stable, column-aligned
// form suitable for CLI output.
func WriteCalendarList(w io.Writer, calendars []CalendarDescriptor) {
	fmt.Fprintln(w, "Available calendars:")
	for _, calendar := range calendars {
		fmt.Fprintf(w, "  %-60s  %s\n", calendar.ID, calendar.Summary)
	}
}

// ResolveCalendar returns the ID for a calendar matching either its ID or its
// summary, ignoring summary case.
func ResolveCalendar(calendars []CalendarDescriptor, value string) (string, error) {
	for _, calendar := range calendars {
		if calendar.ID == value || strings.EqualFold(calendar.Summary, value) {
			return calendar.ID, nil
		}
	}
	return "", fmt.Errorf("no calendar matches %q", value)
}
