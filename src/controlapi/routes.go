package controlapi

import (
	"net/http"
	"net/http/pprof"
	"net/netip"
	"strings"

	"github.com/nimakalantar/calendar-agent/src/health"
)

// Metrics is the small metrics surface used by the HTTP layer.
type Metrics interface {
	Inc(name string, labels map[string]string)
	Observe(name string, value float64, labels map[string]string)
	Handler() http.Handler
}

type noopMetrics struct{}

func (noopMetrics) Inc(string, map[string]string)              {}
func (noopMetrics) Observe(string, float64, map[string]string) {}
func (noopMetrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	})
}

// Options controls HTTP hardening and observability.
type Options struct {
	// Metrics receives request and auth-failure counters. Nil serves empty
	// metrics; applications normally pass their registry.
	Metrics Metrics
	// MetricsPrefix is prepended to HTTP metric names. Default "calendar_agent".
	MetricsPrefix string
	// Build is exposed by /version and /health.
	Build BuildInfo
	// Title is shown in the operator dashboard. Default "calendar-agent".
	Title string
	// MaxBodyBytes bounds request bodies. Zero uses 1 MiB.
	MaxBodyBytes int64
	// RateLimitPerMinute limits authenticated control and dashboard requests
	// per client. Zero disables the limiter (useful in tests).
	RateLimitPerMinute int
	// PublicRateLimitPerMinute limits unauthenticated health and metrics
	// requests per client. Zero uses 600; a negative value disables the limiter.
	PublicRateLimitPerMinute int
	// AuthFailureLimitPerMinute blocks a client for one minute after this many
	// authorization failures. Zero disables the tracker.
	AuthFailureLimitPerMinute int
	// TrustedProxies are the only peers allowed to supply X-Forwarded-For and
	// X-Forwarded-Proto.
	TrustedProxies []netip.Prefix
	// TrustTLSTermination enables HSTS when a trusted proxy reports HTTPS.
	TrustTLSTermination bool
	// AccessLog enables one structured log line per non-noise request.
	AccessLog bool
	// Readiness gates /ready on dependency probes. Nil keeps the legacy
	// behaviour (ready unless shutting down), which is useful in tests.
	Readiness *health.Checker
	// EnablePprof exposes /debug/pprof behind the control-API token.
	EnablePprof bool
}

type api struct {
	service        Service
	opts           Options
	metrics        Metrics
	controlLimiter *keyedLimiter
	publicLimiter  *keyedLimiter
	failures       *failureTracker
	trustedProxies []netip.Prefix
	readiness      *health.Checker
}

// Handler builds the HTTP control API for svc with default options.
func Handler(svc Service) http.Handler {
	return HandlerWithOptions(svc, Options{})
}

// HandlerWithOptions builds the hardened HTTP control API for svc.
func HandlerWithOptions(svc Service, opts Options) http.Handler {
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = 1 << 20
	}
	if opts.Metrics == nil {
		opts.Metrics = noopMetrics{}
	}
	if opts.MetricsPrefix == "" {
		opts.MetricsPrefix = "calendar_agent"
	}
	if opts.Title == "" {
		opts.Title = "calendar-agent"
	}
	publicRate := opts.PublicRateLimitPerMinute
	if publicRate == 0 {
		publicRate = 600
	}
	a := &api{
		service:        svc,
		opts:           opts,
		metrics:        opts.Metrics,
		readiness:      opts.Readiness,
		controlLimiter: newKeyedLimiter(opts.RateLimitPerMinute, 10000),
		publicLimiter:  newKeyedLimiter(publicRate, 10000),
		failures:       newFailureTracker(opts.AuthFailureLimitPerMinute),
		trustedProxies: append([]netip.Prefix(nil), opts.TrustedProxies...),
	}

	handler := a.routes()
	handler = a.recoverPanic(handler)
	handler = a.bodyLimit(handler)
	handler = a.metricsMiddleware(handler)
	handler = a.accessLog(handler)
	handler = a.securityHeaders(handler)
	handler = a.requestID(handler)
	return handler
}

func (a *api) metricName(suffix string) string {
	return a.opts.MetricsPrefix + "_" + suffix
}

func jobsPathID(path string) string {
	for _, prefix := range []string{"/jobs/", "/v1/jobs/"} {
		if strings.HasPrefix(path, prefix) {
			return strings.TrimPrefix(path, prefix)
		}
	}
	return ""
}

type route struct {
	method   string
	pattern  string
	handler  http.HandlerFunc
	auth     bool
	viewAuth bool
}

func (a *api) routeTable() []route {
	routes := []route{
		{http.MethodGet, "/live", a.handleLive, false, false},
		{http.MethodGet, "/ready", a.handleReady, false, false},
		{http.MethodGet, "/health", a.handleHealth, false, false},
		{http.MethodGet, "/version", a.handleVersion, false, false},
		{http.MethodGet, "/metrics", a.handleMetrics, false, false},
		{http.MethodPost, "/sync", a.handleSync, true, false},
		{http.MethodGet, "/jobs", a.handleJobs, true, false},
		{http.MethodGet, "/jobs/{id}", a.handleJob, true, false},
		{http.MethodPost, "/jobs/{id}/cancel", a.handleCancelJob, true, false},
		{http.MethodGet, "/{$}", a.handleDashboard, false, true},
		{http.MethodGet, "/dashboard", a.handleDashboard, false, true},
		{http.MethodGet, "/ui", a.handleDashboard, false, true},
	}
	// Versioned aliases keep existing clients working while giving new
	// integrations a stable /v1 prefix. The HTML dashboard stays unversioned.
	versioned := make([]route, 0, len(routes))
	for _, item := range routes {
		if item.viewAuth || item.pattern == "/{$}" {
			continue
		}
		item.pattern = "/v1" + item.pattern
		versioned = append(versioned, item)
	}
	return append(routes, versioned...)
}

func (a *api) routes() http.Handler {
	mux := http.NewServeMux()
	methodByPath := map[string]string{}
	for _, route := range a.routeTable() {
		handler := route.handler
		if route.auth {
			handler = a.requireAuth(handler)
		}
		if route.viewAuth {
			handler = a.requireViewAuth(handler)
		}
		if route.auth || route.viewAuth {
			handler = a.rateLimit(a.controlLimiter, handler)
		} else {
			handler = a.rateLimit(a.publicLimiter, handler)
		}
		mux.HandleFunc(route.method+" "+route.pattern, handler)
		switch {
		case route.pattern == "/{$}":
			methodByPath["/"] = route.method
		case !strings.Contains(route.pattern, "{"):
			methodByPath[route.pattern] = route.method
		}
	}
	if a.opts.EnablePprof {
		mux.HandleFunc("GET /debug/pprof/", a.requireAuth(pprof.Index))
		mux.HandleFunc("GET /debug/pprof/cmdline", a.requireAuth(pprof.Cmdline))
		mux.HandleFunc("GET /debug/pprof/profile", a.requireAuth(pprof.Profile))
		mux.HandleFunc("GET /debug/pprof/symbol", a.requireAuth(pprof.Symbol))
		mux.HandleFunc("GET /debug/pprof/trace", a.requireAuth(pprof.Trace))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); pattern == "" {
			if method, ok := methodByPath[r.URL.Path]; ok {
				w.Header().Set("Allow", method)
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			if jobsPathID(r.URL.Path) != "" {
				allow := http.MethodGet
				if strings.HasSuffix(r.URL.Path, "/cancel") {
					allow = http.MethodPost
				}
				w.Header().Set("Allow", allow)
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		mux.ServeHTTP(w, r)
	})
}
