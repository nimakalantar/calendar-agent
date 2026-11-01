package gcalendar

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/nimakalantar/calendar-agent/src/calendaragent"
)

// apiRequestTimeout bounds one Google API request so a stalled connection
// cannot hold a sync job open indefinitely.
const apiRequestTimeout = 2 * time.Minute

// Metrics is the small metrics surface used by the calendar cache. It is
// optional; a nil Metrics disables metric recording.
type Metrics interface {
	Inc(name string, labels map[string]string)
	Set(name string, value float64, labels map[string]string)
}

// ClientOptions controls the optional durable event cache, ownership marker,
// and transport instrumentation. Incremental sync is opt-in; without it the
// client lists the requested horizon on every run.
type ClientOptions struct {
	CachePath        string
	IncrementalSync  bool
	FullSyncInterval time.Duration
	Now              func() time.Time
	Metrics          Metrics
	// MetricsPrefix is prepended to cache metric names. The default is
	// "calendar_agent".
	MetricsPrefix string
	// Ownership identifies the events this client manages. It is used by
	// managed-only listings and by the cache.
	Ownership calendaragent.Ownership
	// WrapTransport optionally instruments the OAuth HTTP transport (for
	// example with OpenTelemetry). Nil leaves the transport unchanged.
	WrapTransport func(http.RoundTripper) http.RoundTripper
}

// Client is an authenticated Google Calendar API client.
type Client struct {
	service *calendar.Service
	opts    ClientOptions
	cacheMu sync.Mutex
}

// NewClient builds a Calendar API client backed by a token source.
func NewClient(ctx context.Context, tokenSource oauth2.TokenSource) (*Client, error) {
	return NewClientWithEndpoint(ctx, tokenSource, "")
}

// NewClientWithEndpoint is NewClient with an optional API base URL, used by
// tests and by deployments that front the Calendar API with a proxy.
func NewClientWithEndpoint(ctx context.Context, tokenSource oauth2.TokenSource, endpoint string) (*Client, error) {
	return NewClientWithOptions(ctx, tokenSource, endpoint, ClientOptions{})
}

// NewClientWithOptions is NewClientWithEndpoint plus incremental-sync options.
func NewClientWithOptions(ctx context.Context, tokenSource oauth2.TokenSource, endpoint string, options ClientOptions) (*Client, error) {
	httpClient := oauth2.NewClient(ctx, tokenSource)
	if options.WrapTransport != nil {
		httpClient.Transport = options.WrapTransport(httpClient.Transport)
	}
	httpClient.Timeout = apiRequestTimeout
	clientOptions := []option.ClientOption{option.WithHTTPClient(httpClient)}
	if endpoint != "" {
		if !strings.HasSuffix(endpoint, "/") {
			endpoint += "/"
		}
		clientOptions = append(clientOptions, option.WithEndpoint(endpoint))
	}
	service, err := calendar.NewService(ctx, clientOptions...)
	if err != nil {
		return nil, err
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.FullSyncInterval <= 0 {
		options.FullSyncInterval = 24 * time.Hour
	}
	if options.MetricsPrefix == "" {
		options.MetricsPrefix = "calendar_agent"
	}
	if options.Ownership.Property == "" {
		options.Ownership.Property = DefaultOwnership().Property
	}
	if options.Ownership.Value == "" {
		options.Ownership.Value = DefaultOwnership().Value
	}
	return &Client{service: service, opts: options}, nil
}

// Ownership returns the marker used for managed events.
func (c *Client) Ownership() calendaragent.Ownership {
	return c.opts.Ownership
}

// IsTransientAPIError reports whether err is a Google API error worth
// retrying or classifying as a network failure.
func IsTransientAPIError(err error) bool {
	var apiErr *googleapi.Error
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Code == http.StatusTooManyRequests || apiErr.Code >= http.StatusInternalServerError
}

// IsGone reports whether err is a Google API 404 (not found) or 410 (deleted).
func IsGone(err error) bool {
	var apiErr *googleapi.Error
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Code == http.StatusNotFound || apiErr.Code == http.StatusGone
}

func isConflict(err error) bool {
	var apiErr *googleapi.Error
	return errors.As(err, &apiErr) && apiErr.Code == http.StatusConflict
}

func (c *Client) metricName(suffix string) string {
	return c.opts.MetricsPrefix + "_calendar_" + suffix
}
