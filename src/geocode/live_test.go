package geocode

import (
	"context"
	"os"
	"testing"
)

// TestLiveGeocode is an opt-in probe against Nominatim:
//
//	CALENDAR_AGENT_LIVE_GEOCODE=1 go test ./internal/geocode/ -run TestLiveGeocode -v
func TestLiveGeocode(t *testing.T) {
	if os.Getenv("CALENDAR_AGENT_LIVE_GEOCODE") == "" {
		t.Skip("set CALENDAR_AGENT_LIVE_GEOCODE=1 to geocode live")
	}
	address := os.Getenv("CALENDAR_AGENT_GEOCODE_ADDRESS")
	if address == "" {
		address = "33 Holders Hill Crescent, NW4 1NE"
	}
	g := New("calendar-agent/1.0 (+https://github.com/nimakalantar/calendar-agent)", "")
	coord, err := g.Resolve(context.Background(), address)
	if err != nil {
		t.Fatalf("resolve %q: %v", address, err)
	}
	t.Logf("%q -> %.5f,%.5f", address, coord.Lat, coord.Lon)
}
