package gcalendar

import (
	"errors"
	"fmt"
	"time"

	"github.com/nimakalantar/calendar-agent/src/calendaragent"
	"google.golang.org/api/calendar/v3"
)

// parseFlexibleTime accepts either a date-only value or an RFC3339 timestamp.
func parseFlexibleTime(value string, loc *time.Location) (time.Time, bool) {
	if t, err := time.ParseInLocation("2006-01-02", value, loc); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.UTC(), true
	}
	return time.Time{}, false
}

func parseEventTime(dt *calendar.EventDateTime, loc *time.Location) (time.Time, error) {
	if dt == nil {
		return time.Time{}, errors.New("event has no start or end")
	}
	value := dt.Date
	if value == "" {
		value = dt.DateTime
	}
	t, ok := parseFlexibleTime(value, loc)
	if !ok {
		return time.Time{}, fmt.Errorf("invalid event time %q", value)
	}
	return t, nil
}

// eventTimes parses an event's start and end, reporting false when either is
// unusable so listEvents can fail closed on an incomplete calendar view.
func eventTimes(item *calendar.Event, loc *time.Location) (time.Time, time.Time, bool) {
	start, err := parseEventTime(item.Start, loc)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	end, err := parseEventTime(item.End, loc)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	return start, end, true
}

// toManagedEvent maps one Google event into the provider-independent managed
// shape using the configured property schema.
func selfResponseStatus(attendees []*calendar.EventAttendee) string {
	for _, attendee := range attendees {
		if attendee != nil && attendee.Self {
			if attendee.ResponseStatus == "" {
				return "accepted"
			}
			return attendee.ResponseStatus
		}
	}
	return "accepted"
}

func toManagedEvent(item *calendar.Event, loc *time.Location, cfg Config) (calendaragent.ManagedEvent, bool) {
	start, end, ok := eventTimes(item, loc)
	if !ok {
		return calendaragent.ManagedEvent{}, false
	}
	priv := map[string]string{}
	if item.ExtendedProperties != nil {
		priv = item.ExtendedProperties.Private
	}
	schema := cfg.Schema

	link := priv[schema.Link]
	if link == "" && item.Source != nil {
		link = item.Source.Url
	}

	transparency := calendaragent.Transparency(item.Transparency)
	if transparency == "" {
		// Google Calendar defaults transparency to opaque when omitted.
		transparency = calendaragent.TransparencyOpaque
	}
	startTimeZone, endTimeZone := item.Start.TimeZone, item.End.TimeZone
	if loc != nil {
		if startTimeZone == "" {
			startTimeZone = loc.String()
		}
		if endTimeZone == "" {
			endTimeZone = loc.String()
		}
	}
	presentation := calendaragent.Presentation{
		Transparency:        transparency,
		ColorID:             item.ColorId,
		StartTimeZone:       startTimeZone,
		EndTimeZone:         endTimeZone,
		UseDefaultReminders: item.Reminders == nil || item.Reminders.UseDefault,
	}
	if item.Reminders != nil {
		for _, reminder := range item.Reminders.Overrides {
			if reminder == nil {
				continue
			}
			presentation.Reminders = append(presentation.Reminders, calendaragent.Reminder{
				Method: reminder.Method, Minutes: int(reminder.Minutes),
			})
		}
	}
	status := item.Status
	if status == "" {
		status = "confirmed"
	}
	return calendaragent.ManagedEvent{
		ID:           item.Id,
		Key:          priv[schema.Key],
		SourceKey:    priv[schema.SourceKey],
		Summary:      item.Summary,
		Description:  item.Description,
		Location:     item.Location,
		Kind:         priv[schema.Kind],
		Start:        start,
		End:          end,
		AllDay:       item.Start != nil && item.Start.Date != "",
		Status:       status,
		SelfResponse: selfResponseStatus(item.Attendees),
		Link:         link,
		LinkHash:     priv[schema.LinkHash],
		Presentation: presentation,
		Owned:        priv[schema.ManagedBy] == cfg.Ownership.Value,
		ContentHash:  priv[schema.ContentHash],
	}, true
}

// ParseFlexibleTime exposes the date-or-RFC3339 parser for adapters that map
// provider events into their own source-event shape.
func ParseFlexibleTime(value string, loc *time.Location) (time.Time, bool) {
	return parseFlexibleTime(value, loc)
}

// EventTimes exposes start/end parsing for adapters that need provider fields
// beyond calendaragent.ManagedEvent.
func EventTimes(item *calendar.Event, loc *time.Location) (time.Time, time.Time, bool) {
	return eventTimes(item, loc)
}
