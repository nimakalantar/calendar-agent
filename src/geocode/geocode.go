// Package geocode resolves location strings to coordinates.
package geocode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nimakalantar/calendar-agent/src/location"
	"github.com/nimakalantar/calendar-agent/src/platform/fsutil"
)

// Resolver converts a location string into coordinates.
type Resolver interface {
	Resolve(ctx context.Context, query string) (location.Coordinates, error)
}

// Chain tries resolvers in order and returns the first success.
type Chain []Resolver

// Resolve implements Resolver.
func (c Chain) Resolve(ctx context.Context, query string) (location.Coordinates, error) {
	var lastErr error
	for _, resolver := range c {
		if resolver == nil {
			continue
		}
		coords, err := resolver.Resolve(ctx, query)
		if err == nil {
			return coords, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no geocoding resolver configured")
	}
	return location.Coordinates{}, lastErr
}

// Coordinates resolves only "lat,lon" strings.
type Coordinates struct{}

// Resolve implements Resolver.
func (Coordinates) Resolve(_ context.Context, query string) (location.Coordinates, error) {
	coords, ok := location.Parse(query)
	if !ok {
		return location.Coordinates{}, fmt.Errorf("%q is not a coordinate pair", query)
	}
	return coords, nil
}

// Nominatim resolves free-text locations with a Nominatim-compatible endpoint.
type Nominatim struct {
	BaseURL    string
	UserAgent  string
	HTTPClient *http.Client
}

// Resolve implements Resolver.
func (n Nominatim) Resolve(ctx context.Context, query string) (location.Coordinates, error) {
	base := strings.TrimRight(n.BaseURL, "/")
	if base == "" {
		base = "https://nominatim.openstreetmap.org"
	}
	client := n.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	values := url.Values{}
	values.Set("q", query)
	values.Set("format", "jsonv2")
	values.Set("limit", "1")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/search?"+values.Encode(), nil)
	if err != nil {
		return location.Coordinates{}, err
	}
	req.Header.Set("User-Agent", n.UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return location.Coordinates{}, fmt.Errorf("geocode %q: %w", query, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return location.Coordinates{}, fmt.Errorf("geocode %q: HTTP %d", query, resp.StatusCode)
	}
	var results []struct {
		Lat string `json:"lat"`
		Lon string `json:"lon"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&results); err != nil {
		return location.Coordinates{}, fmt.Errorf("geocode %q: %w", query, err)
	}
	if len(results) == 0 {
		return location.Coordinates{}, fmt.Errorf("geocode %q: no result", query)
	}
	lat, err := strconv.ParseFloat(results[0].Lat, 64)
	if err != nil {
		return location.Coordinates{}, fmt.Errorf("geocode %q: invalid latitude", query)
	}
	lon, err := strconv.ParseFloat(results[0].Lon, 64)
	if err != nil {
		return location.Coordinates{}, fmt.Errorf("geocode %q: invalid longitude", query)
	}
	coords := location.Coordinates{Latitude: lat, Longitude: lon}
	if err := coords.Validate(); err != nil {
		return location.Coordinates{}, err
	}
	return coords, nil
}

type cacheEntry struct {
	Coordinates location.Coordinates `json:"coordinates"`
	FetchedAt   time.Time            `json:"fetchedAt"`
}

// Cache persists successful lookups in a JSON file.
type Cache struct {
	Path  string
	Inner Resolver
	TTL   time.Duration
	Now   func() time.Time
}

// Resolve implements Resolver.
func (c Cache) Resolve(ctx context.Context, query string) (location.Coordinates, error) {
	key := strings.ToLower(Clean(query))
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	if c.Path != "" {
		var entries map[string]cacheEntry
		if err := fsutil.ReadJSON(c.Path, &entries); err == nil {
			if entry, ok := entries[key]; ok {
				ttl := c.TTL
				if ttl <= 0 {
					ttl = 30 * 24 * time.Hour
				}
				if now().Sub(entry.FetchedAt) < ttl {
					return entry.Coordinates, nil
				}
			}
		}
	}
	coords, err := c.Inner.Resolve(ctx, query)
	if err != nil {
		return location.Coordinates{}, err
	}
	if c.Path != "" {
		entries := map[string]cacheEntry{}
		_ = fsutil.ReadJSON(c.Path, &entries)
		entries[key] = cacheEntry{Coordinates: coords, FetchedAt: now()}
		_ = fsutil.WriteJSONAtomic(c.Path, entries, 0o600)
	}
	return coords, nil
}

// Clean validates and normalises a query.
func Clean(query string) string {
	return strings.Join(strings.Fields(query), " ")
}
