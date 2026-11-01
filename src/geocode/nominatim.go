package geocode

import (
	"context"
	"net/url"
	"time"

	"github.com/nimakalantar/calendar-agent/src/platform/retry"
)

// nominatimRetry bounds retries against the public Nominatim API.
var nominatimRetry = retry.Policy{Attempts: 3, BaseDelay: time.Second, MaxDelay: retry.DefaultMaxServerDelay}

// fetchNominatim performs a rate-limited Nominatim search, retrying transient
// failures (network errors, 429, 5xx) so a single hiccup does not drop a
// location. The 1 request/second policy is enforced before every attempt.
func (g *Geocoder) fetchNominatim(ctx context.Context, key string) ([]byte, error) {
	endpoint := g.nominatimURL + "?q=" + url.QueryEscape(key) + "&format=json&limit=1"
	return retry.DoValue(ctx, nominatimRetry, retry.DefaultClassifier,
		func(ctx context.Context) ([]byte, error) {
			if !g.waitForRateLimit(ctx) {
				return nil, ctx.Err()
			}
			return g.getBody(ctx, endpoint, "Nominatim request failed")
		})
}

// geocodeMinInterval is a small margin over Nominatim's 1 request/second
// policy, which avoids tripping its burst detection.
const geocodeMinInterval = 1100 * time.Millisecond

func (g *Geocoder) waitForRateLimit(ctx context.Context) bool {
	return g.limiter.Wait(ctx) == nil
}
