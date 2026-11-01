package gcalendar

import (
	"testing"
	"time"

	"google.golang.org/api/calendar/v3"
)

func TestAllDayEventTimesAcrossDSTBoundaries(t *testing.T) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		date string
		want time.Time
	}{
		{
			name: "spring forward",
			date: "2026-03-29",
			want: time.Date(2026, time.March, 29, 0, 0, 0, 0, loc),
		},
		{
			name: "fall back",
			date: "2026-10-25",
			want: time.Date(2026, time.October, 25, 0, 0, 0, 0, loc),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := &calendar.Event{
				Start: &calendar.EventDateTime{Date: tc.date},
				End:   &calendar.EventDateTime{Date: tc.date},
			}
			start, end, ok := eventTimes(item, loc)
			if !ok {
				t.Fatal("event times were rejected")
			}
			if !start.Equal(tc.want) || !end.Equal(tc.want) {
				t.Fatalf("times = %v/%v, want %v", start, end, tc.want)
			}
			if _, ok := toManagedEvent(item, loc, Config{}); !ok {
				t.Fatal("all-day event did not map")
			}
		})
	}
}

func TestFilterHorizonIncludesDSTDay(t *testing.T) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	horizon := time.Date(2026, time.March, 29, 0, 0, 0, 0, loc)
	item := &calendar.Event{
		Id:    "dst",
		Start: &calendar.EventDateTime{Date: "2026-03-29"},
		End:   &calendar.EventDateTime{Date: "2026-03-29"},
	}
	events, err := filterEventsByHorizon([]*calendar.Event{item}, horizon, 1, loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Id != "dst" {
		t.Fatalf("events = %+v", events)
	}
}
