package geocode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/time/rate"
)

func TestParseCoordinates(t *testing.T) {
	if coord, ok := ParseCoordinates("51.5074, -0.1278"); !ok || coord.Lat != 51.5074 {
		t.Fatalf("parse = %+v %v", coord, ok)
	}
	for _, bad := range []string{"not,a,coord", "91,0", "0,181", "1", "a,b", "NaN,0", "0,NaN", "Inf,0", "0,+Inf"} {
		if _, ok := ParseCoordinates(bad); ok {
			t.Fatalf("%q should not parse", bad)
		}
	}
}

func TestStripVenueName(t *testing.T) {
	if got := StripVenueName("The Venue, 42 Sample Road, London"); got != "42 Sample Road, London" {
		t.Fatalf("strip = %q", got)
	}
	if got := StripVenueName("No comma"); got != "No comma" {
		t.Fatalf("strip = %q", got)
	}
}

func TestExtractUKPostcode(t *testing.T) {
	cases := map[string]string{
		"42 Sample Road, London sw1a 1aa": "SW1A 1AA",
		"Somewhere EC2A4BX":               "EC2A4BX",
		"no postcode here":                "",
	}
	for input, want := range cases {
		if got := ExtractUKPostcode(input); got != want {
			t.Fatalf("postcode(%q) = %q want %q", input, got, want)
		}
	}
}

func TestGeocoderUsesInjectedPostcodesEndpoint(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/postcodes/SW1A1AA" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"status":200,"result":{"latitude":51.5,"longitude":-0.1}}`))
	}))
	defer server.Close()

	geocoder := NewWithOptions(Options{UserAgent: "test", PostcodesURL: server.URL})
	coord, err := geocoder.Resolve(context.Background(), "SW1A 1AA")
	if err != nil {
		t.Fatal(err)
	}
	if coord.Lat != 51.5 || coord.Lon != -0.1 {
		t.Fatalf("coord = %+v", coord)
	}
	if _, err := geocoder.Resolve(context.Background(), "SW1A 1AA"); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("postcode service called %d times for a cached lookup, want 1", requests)
	}
}

func TestNominatimStrategySkipsPostcodeService(t *testing.T) {
	postcodeRequests := 0
	postcodes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		postcodeRequests++
		http.NotFound(w, nil)
	}))
	defer postcodes.Close()

	nominatimRequests := 0
	nominatim := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nominatimRequests++
		_, _ = w.Write([]byte(`[{"lat":"51.5","lon":"-0.1"}]`))
	}))
	defer nominatim.Close()

	geocoder := NewWithOptions(Options{
		UserAgent:    "test",
		Strategy:     StrategyNominatim,
		NominatimURL: nominatim.URL,
		PostcodesURL: postcodes.URL,
		HTTPClient:   nominatim.Client(),
		Limiter:      rate.NewLimiter(rate.Inf, 1),
	})
	if _, err := geocoder.Resolve(context.Background(), "SW1A 1AA"); err != nil {
		t.Fatal(err)
	}
	if postcodeRequests != 0 {
		t.Fatalf("postcode service called %d times with nominatim strategy", postcodeRequests)
	}
	if nominatimRequests == 0 {
		t.Fatal("nominatim was not called")
	}
}

func TestCoordinatesStrategyNeverUsesTheNetwork(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.NotFound(w, nil)
	}))
	defer server.Close()

	geocoder := NewWithOptions(Options{
		UserAgent:    "test",
		Strategy:     StrategyCoordinates,
		NominatimURL: server.URL,
		PostcodesURL: server.URL,
		HTTPClient:   server.Client(),
	})
	if _, err := geocoder.Resolve(context.Background(), "Some Address"); err == nil {
		t.Fatal("expected coordinates strategy to reject a non-coordinate")
	}
	coord, err := geocoder.Resolve(context.Background(), "51.5,-0.1")
	if err != nil {
		t.Fatal(err)
	}
	if coord.Lat != 51.5 || coord.Lon != -0.1 {
		t.Fatalf("coord = %+v", coord)
	}
	if requests != 0 {
		t.Fatalf("coordinates strategy made %d network requests", requests)
	}
}

func TestGeocoderUsesInjectedNominatimClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "" {
			http.Error(w, "missing query", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`[{"lat":"51.5","lon":"-0.1"}]`))
	}))
	defer server.Close()

	geocoder := NewWithOptions(Options{
		UserAgent:    "test",
		NominatimURL: server.URL,
		HTTPClient:   server.Client(),
		Limiter:      rate.NewLimiter(rate.Inf, 1),
	})
	coord, err := geocoder.Resolve(context.Background(), "Some Address")
	if err != nil {
		t.Fatal(err)
	}
	if coord.Lat != 51.5 || coord.Lon != -0.1 {
		t.Fatalf("coord = %+v", coord)
	}
}

func TestParseNominatimCoordinateErrors(t *testing.T) {
	cases := [][]byte{
		[]byte(`not json`),
		[]byte(`[]`),
		[]byte(`[{"lat":"x","lon":"0"}]`),
		[]byte(`[{"lat":"91","lon":"0"}]`),
		[]byte(`[{"lat":"0","lon":"181"}]`),
	}
	for _, body := range cases {
		if _, err := parseNominatimCoordinate(body); err == nil {
			t.Fatalf("expected error for %s", body)
		}
	}
}

func TestGeocoderMemoisesFailures(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()
	geocoder := NewWithOptions(Options{
		UserAgent: "test", NominatimURL: server.URL, HTTPClient: server.Client(),
		Limiter: rate.NewLimiter(rate.Inf, 1),
	})
	if _, err := geocoder.Resolve(context.Background(), "Some Address"); err == nil {
		t.Fatal("expected a failure")
	}
	if _, err := geocoder.Resolve(context.Background(), "Some Address"); err == nil {
		t.Fatal("expected a cached failure")
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1 after failure memoisation", requests)
	}
}

func TestNewCoordCacheFromEntriesTrims(t *testing.T) {
	cache := newCoordCacheFromEntries(map[string]Coord{
		"a": {Lat: 1}, "b": {Lat: 2},
	}, []string{"b", "a"}, 1)
	if cache.len() != 1 {
		t.Fatalf("cache size = %d", cache.len())
	}
	if _, ok := cache.get("a"); !ok {
		t.Fatal("newest entry should remain")
	}
}

func TestPruneCacheEdgeCases(t *testing.T) {
	if _, err := PruneCache("", 1); err == nil {
		t.Fatal("expected empty path error")
	}
	if _, err := PruneCache(filepath.Join(t.TempDir(), "cache.json"), 0); err == nil {
		t.Fatal("expected invalid maxEntries error")
	}
	path := filepath.Join(t.TempDir(), "cache.json")
	removed, err := PruneCache(path, 2)
	if err != nil || removed != 0 {
		t.Fatalf("missing cache = %d, %v", removed, err)
	}
	if err := os.WriteFile(path, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PruneCache(path, 2); err == nil {
		t.Fatal("expected corrupt cache error")
	}
}

func TestPostcodesIOInvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":404,"result":null}`))
	}))
	defer server.Close()
	geocoder := NewWithOptions(Options{UserAgent: "test", PostcodesURL: server.URL, NominatimURL: server.URL, HTTPClient: server.Client(), Limiter: rate.NewLimiter(rate.Inf, 1)})
	if _, err := geocoder.Resolve(context.Background(), "SW1A 1AA"); err == nil {
		t.Fatal("expected postcode lookup to fall through and fail")
	}
}
