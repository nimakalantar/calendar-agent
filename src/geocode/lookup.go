package geocode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/nimakalantar/calendar-agent/src/platform/retry"
)

func (g *Geocoder) cacheGet(key string) (Coord, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.cache.get(key)
}

func (g *Geocoder) cachePut(key string, coord Coord) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cache.put(key, coord)
}

func (g *Geocoder) failureFor(key string) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	msg, ok := g.failures[key]
	return msg, ok
}

func (g *Geocoder) fail(key, msg string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Bound the memoised failures so a long-lived process cannot grow the map
	// forever; correctness does not depend on keeping old entries.
	if len(g.failures) >= MaxCacheEntries {
		drop := max(1, len(g.failureOrder)/2)
		for i := 0; i < drop; i++ {
			delete(g.failures, g.failureOrder[i])
		}
		g.failureOrder = g.failureOrder[drop:]
	}
	if _, exists := g.failures[key]; !exists {
		g.failureOrder = append(g.failureOrder, key)
	}
	g.failures[key] = msg
}

// getBody performs an HTTP GET and returns a successful response body. The
// label prefixes every error and is used by the shared retry classifier.
func (g *Geocoder) getBody(ctx context.Context, endpoint, label string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	req.Header.Set("User-Agent", g.userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &retry.StatusError{Label: label, Status: resp.StatusCode, Header: resp.Header}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	return body, nil
}

func parseNominatimCoordinate(body []byte) (Coord, error) {
	var results []struct {
		Lat string `json:"lat"`
		Lon string `json:"lon"`
	}
	if err := json.Unmarshal(body, &results); err != nil {
		return Coord{}, errors.New("Nominatim response parse failed: " + err.Error())
	}
	if len(results) == 0 {
		return Coord{}, errors.New("nominatim returned no matches")
	}
	lat, latErr := strconv.ParseFloat(results[0].Lat, 64)
	lon, lonErr := strconv.ParseFloat(results[0].Lon, 64)
	if latErr != nil || lonErr != nil || !finiteCoordinate(lat, lon) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return Coord{}, errors.New("nominatim returned invalid coordinates")
	}
	return Coord{Lat: lat, Lon: lon}, nil
}

// geocodeOne resolves a single candidate string, using the cache and
// memoising failures. The returned error describes this lookup only, so
// concurrent Resolve calls cannot report each other's failures.
func (g *Geocoder) geocodeOne(ctx context.Context, location string) (Coord, error) {
	key := strings.TrimSpace(location)
	if key == "" {
		return Coord{}, errors.New("location is empty")
	}
	if coord, ok := g.cacheGet(key); ok {
		return coord, nil
	}
	// Memoise failures for the lifetime of the process so a repeatedly
	// referenced unresolvable address is not re-requested from Nominatim.
	if msg, ok := g.failureFor(key); ok {
		return Coord{}, errors.New(msg)
	}
	if coord, ok := ParseCoordinates(key); ok {
		g.cachePut(key, coord)
		return coord, nil
	}

	body, err := g.fetchNominatim(ctx, key)
	if err != nil {
		g.fail(key, err.Error())
		return Coord{}, err
	}

	coord, err := parseNominatimCoordinate(body)
	if err != nil {
		g.fail(key, err.Error())
		return Coord{}, err
	}
	g.cachePut(key, coord)
	return coord, nil
}
