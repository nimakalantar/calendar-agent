package gcalendar

import (
	"encoding/json"
	"testing"
	"time"

	"google.golang.org/api/calendar/v3"
)

func FuzzCalendarEventMapping(f *testing.F) {
	f.Add([]byte(`{"id":"x","start":{"dateTime":"2026-01-01T00:00:00Z"},"end":{"dateTime":"2026-01-01T01:00:00Z"}}`))
	f.Add([]byte(`{"id":"x","start":{"date":"2026-01-01"},"end":{"date":"2026-01-02"},"recurrence":["RRULE:FREQ=DAILY","EXDATE;TZID=Europe/London:20260102T090000"]}`))
	f.Add([]byte(`{"id":"x","start":null,"end":null}`))
	f.Add([]byte(`{"id":"x","start":{"dateTime":"not-a-time"},"end":{"dateTime":"also-bad"}}`))
	f.Add([]byte(`null`))

	loc := time.UTC
	f.Fuzz(func(t *testing.T, data []byte) {
		var event calendar.Event
		if err := json.Unmarshal(data, &event); err != nil {
			return
		}
		_, _ = toManagedEvent(&event, loc, Config{})
	})
}
