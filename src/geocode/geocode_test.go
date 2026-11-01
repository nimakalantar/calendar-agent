package geocode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/nimakalantar/calendar-agent/src/location"
)

func TestCoordinatesResolver(t *testing.T) {
	got, err := (Coordinates{}).Resolve(context.Background(), "51.5,-0.1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Latitude != 51.5 || got.Longitude != -0.1 {
		t.Fatalf("coords = %+v", got)
	}
	if _, err := (Coordinates{}).Resolve(context.Background(), "London"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestNominatim(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "London" {
			t.Errorf("query = %q", r.URL.Query().Get("q"))
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing user agent")
		}
		_, _ = w.Write([]byte(`[{"lat":"51.5074","lon":"-0.1278"}]`))
	}))
	defer server.Close()
	got, err := (Nominatim{BaseURL: server.URL, UserAgent: "test"}).Resolve(context.Background(), "London")
	if err != nil {
		t.Fatal(err)
	}
	if got.Latitude != 51.5074 || got.Longitude != -0.1278 {
		t.Fatalf("coords = %+v", got)
	}
}

func TestChainAndCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "geocode.json")
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	resolver := Chain{
		Coordinates{},
		Cache{Path: path, Inner: Nominatim{BaseURL: "http://127.0.0.1:1", UserAgent: "test"}, Now: func() time.Time { return now }},
	}
	if _, err := resolver.Resolve(context.Background(), "London"); err == nil {
		t.Fatal("expected a network error")
	}
	if _, err := resolver.Resolve(context.Background(), "51.5,-0.1"); err != nil {
		t.Fatal(err)
	}
}

func TestCacheHit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "geocode.json")
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	calls := 0
	inner := resolverFunc(func(context.Context, string) (location.Coordinates, error) {
		calls++
		return location.Coordinates{Latitude: 1, Longitude: 2}, nil
	})
	cache := Cache{Path: path, Inner: inner, Now: func() time.Time { return now }}
	if _, err := cache.Resolve(context.Background(), "Paris"); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Resolve(context.Background(), "paris"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}

type resolverFunc func(context.Context, string) (location.Coordinates, error)

func (f resolverFunc) Resolve(ctx context.Context, query string) (location.Coordinates, error) {
	return f(ctx, query)
}

func TestEmptyChain(t *testing.T) {
	if _, err := (Chain{}).Resolve(context.Background(), "London"); err == nil {
		t.Fatal("expected an error")
	}
}
