package gcalendar

import (
	"fmt"
	"testing"
	"time"

	"google.golang.org/api/calendar/v3"
)

func benchmarkEvents(count int, start time.Time) []*calendar.Event {
	events := make([]*calendar.Event, 0, count)
	for i := 0; i < count; i++ {
		at := start.Add(time.Duration(i) * 15 * time.Minute)
		events = append(events, &calendar.Event{
			Id:    fmt.Sprintf("event-%05d", i),
			Start: &calendar.EventDateTime{DateTime: at.Format(time.RFC3339)},
			End:   &calendar.EventDateTime{DateTime: at.Add(30 * time.Minute).Format(time.RFC3339)},
		})
	}
	return events
}

func BenchmarkFilterEventsByHorizon10k(b *testing.B) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		b.Fatal(err)
	}
	horizon := time.Date(2026, time.June, 2, 0, 0, 0, 0, loc)
	events := benchmarkEvents(10000, horizon)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := filterEventsByHorizon(events, horizon, 7, loc)
		if err != nil {
			b.Fatal(err)
		}
		if len(result) == 0 {
			b.Fatal("empty result")
		}
	}
}

func BenchmarkApplyCachedEvents10k(b *testing.B) {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		b.Fatal(err)
	}
	events := benchmarkEvents(10000, time.Date(2026, time.June, 2, 0, 0, 0, 0, loc))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache := make(map[string]*calendar.Event, len(events))
		for _, event := range events {
			if err := applyCachedEvent(cache, event, false, testOwnership); err != nil {
				b.Fatal(err)
			}
		}
		if len(cache) != len(events) {
			b.Fatalf("cache = %d, want %d", len(cache), len(events))
		}
	}
}
