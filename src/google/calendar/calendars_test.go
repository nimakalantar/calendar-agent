package gcalendar

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteCalendarList(t *testing.T) {
	var out bytes.Buffer
	WriteCalendarList(&out, []CalendarDescriptor{
		{ID: "primary", Summary: "Events"},
		{ID: "work", Summary: "Work"},
	})
	for _, want := range []string{"Available calendars:", "primary", "Events", "work", "Work"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q: %q", want, out.String())
		}
	}
}

func TestResolveCalendar(t *testing.T) {
	calendars := []CalendarDescriptor{{ID: "primary", Summary: "Home"}}
	for _, value := range []string{"primary", "home", "HOME"} {
		id, err := ResolveCalendar(calendars, value)
		if err != nil || id != "primary" {
			t.Fatalf("ResolveCalendar(%q) = %q, %v", value, id, err)
		}
	}
	if _, err := ResolveCalendar(calendars, "missing"); err == nil {
		t.Fatal("expected missing calendar error")
	}
}
