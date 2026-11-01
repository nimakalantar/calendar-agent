// Package geocode converts location strings into coordinates.
//
// Strategy "auto" uses "lat,lon" strings directly, tries UK postcodes through
// postcodes.io, and falls back to Nominatim (rate-limited to 1 request/second).
// "nominatim" skips the UK postcode service, and "coordinates" performs no
// network lookups at all.
package geocode

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Geocoding strategies.
const (
	StrategyAuto        = "auto"
	StrategyNominatim   = "nominatim"
	StrategyCoordinates = "coordinates"
)

// Coord is a latitude/longitude pair.
type Coord struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// MaxCacheEntries is the in-memory cache ceiling.
const MaxCacheEntries = 1000

const (
	defaultNominatimURL = "https://nominatim.openstreetmap.org/search"
	defaultPostcodesURL = "https://api.postcodes.io"
)

// Options configure a Geocoder. Empty endpoint values use the public services.
type Options struct {
	UserAgent    string
	CachePath    string
	Strategy     string
	NominatimURL string
	PostcodesURL string
	HTTPClient   *http.Client
	Limiter      *rate.Limiter
}

// Geocoder resolves location strings. It is safe for concurrent use.
type Geocoder struct {
	mu           sync.Mutex
	userAgent    string
	cachePath    string
	strategy     string
	httpClient   *http.Client
	limiter      *rate.Limiter
	cache        *coordCache
	failures     map[string]string
	failureOrder []string
	nominatimURL string
	postcodesURL string
}

// New creates a geocoder using the public Nominatim and postcodes.io services.
func New(userAgent, cachePath string) *Geocoder {
	return NewWithOptions(Options{UserAgent: userAgent, CachePath: cachePath})
}

// NewWithOptions creates a geocoder with optional endpoint overrides.
func NewWithOptions(opts Options) *Geocoder {
	opts.Strategy = normalizeStrategy(opts.Strategy)
	if opts.NominatimURL == "" {
		opts.NominatimURL = defaultNominatimURL
	}
	if opts.PostcodesURL == "" && opts.Strategy == StrategyAuto {
		opts.PostcodesURL = defaultPostcodesURL
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	limiter := opts.Limiter
	if limiter == nil {
		limiter = rate.NewLimiter(rate.Every(geocodeMinInterval), 1)
	}
	g := &Geocoder{
		userAgent:    opts.UserAgent,
		cachePath:    opts.CachePath,
		strategy:     opts.Strategy,
		httpClient:   httpClient,
		limiter:      limiter,
		cache:        newCoordCache(MaxCacheEntries),
		failures:     map[string]string{},
		nominatimURL: opts.NominatimURL,
		postcodesURL: opts.PostcodesURL,
	}
	if opts.CachePath != "" {
		if parsed, err := readCacheFile(opts.CachePath); err == nil {
			g.cache = newCoordCacheFromEntries(parsed.Entries, parsed.Order, MaxCacheEntries)
		} else if !os.IsNotExist(err) {
			slog.Warn("failed to load geocode cache", "path", opts.CachePath, "error", err)
		}
	}
	return g
}

// ParseCoordinates parses "lat,lon".
func ParseCoordinates(location string) (Coord, bool) {
	parts := strings.Split(location, ",")
	if len(parts) != 2 {
		return Coord{}, false
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lon, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil || !finiteCoordinate(lat, lon) {
		return Coord{}, false
	}

	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return Coord{}, false
	}
	return Coord{Lat: lat, Lon: lon}, true
}

// finiteCoordinate rejects NaN and infinities, which pass naive range checks.
func finiteCoordinate(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lon) && !math.IsInf(lat, 0) && !math.IsInf(lon, 0)
}

// StripVenueName removes a leading venue/building name from a comma-separated
// address: "Venue, 42 Sample Road, City" -> "42 Sample Road, City".
func StripVenueName(location string) string {
	parts := strings.Split(location, ",")
	if len(parts) < 2 {
		return location
	}
	return strings.TrimSpace(strings.Join(parts[1:], ","))
}

// normalizeStrategy maps empty or unknown values to the safe default. Config
// validation reports unknown values before a Geocoder is built.
func normalizeStrategy(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case StrategyNominatim:
		return StrategyNominatim
	case StrategyCoordinates:
		return StrategyCoordinates
	default:
		return StrategyAuto
	}
}

// Resolve turns a location string into coordinates. Coordinates and the cache
// are used directly. In "auto" mode UK postcodes are resolved with postcodes.io
// first, which is fast and reliable; Nominatim handles the remaining free-text
// addresses (and is retried, since shared egress IPs are frequently
// 429-limited).
func (g *Geocoder) Resolve(ctx context.Context, location string) (Coord, error) {
	key := strings.TrimSpace(location)
	if coord, ok := g.cacheGet(key); ok {
		return coord, nil
	}
	if g.strategy == StrategyCoordinates {
		if coord, ok := ParseCoordinates(key); ok {
			g.cachePut(key, coord)
			return coord, nil
		}
		return Coord{}, fmt.Errorf("could not geocode %q: coordinates strategy requires a lat,lon value", location)
	}
	if g.strategy == StrategyAuto {
		if postcode := ExtractUKPostcode(key); postcode != "" {
			if coord, ok := g.cacheGet(postcode); ok {
				g.cachePut(key, coord)
				return coord, nil
			}
			if coord, ok := g.geocodeViaPostcodesIO(ctx, postcode); ok {
				g.cachePut(key, coord)
				g.cachePut(postcode, coord)
				return coord, nil
			}
		}
	}
	var lastErr error
	for _, candidate := range candidateAddresses(key) {
		coord, err := g.geocodeOne(ctx, candidate)
		if err == nil {
			return coord, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no match")
	}
	return Coord{}, fmt.Errorf("could not geocode %q: %w", location, lastErr)
}

// candidateAddresses returns the address variants to try, most specific first.
func candidateAddresses(key string) []string {
	candidates := []string{key}
	if stripped := StripVenueName(key); stripped != key && stripped != "" {
		candidates = append(candidates, stripped)
	}
	return candidates
}
