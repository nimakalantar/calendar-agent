package controlapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

type requestIDContextKey struct{}

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// statusRecorder captures the response status for logs and metrics.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.status = status
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(data)
	r.bytes += n
	return n, err
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func requestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDContextKey{}).(string)
	return value
}

func newRequestID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buffer)
}

func sanitizeRequestID(value string) string {
	value = strings.TrimSpace(value)
	if !validRequestID.MatchString(value) {
		return ""
	}
	return value
}

func (a *api) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := sanitizeRequestID(r.Header.Get("X-Request-ID"))
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, id)))
	})
}

func (a *api) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		header.Set("Cross-Origin-Opener-Policy", "same-origin")
		header.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if a.isHTTPS(r) {
			header.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (a *api) bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > a.opts.MaxBodyBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, a.opts.MaxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

func (a *api) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder, ok := w.(*statusRecorder)
		if !ok {
			recorder = &statusRecorder{ResponseWriter: w}
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("panic serving HTTP request",
					"panic", fmt.Sprint(recovered),
					"stack", string(debug.Stack()),
					"method", r.Method,
					"path", r.URL.Path,
					"request_id", requestIDFromContext(r.Context()),
				)
				if !recorder.wroteHeader {
					writeError(recorder, http.StatusInternalServerError, "internal server error")
				}
			}
		}()
		next.ServeHTTP(recorder, r)
	})
}

func (a *api) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder, ok := w.(*statusRecorder)
		if !ok {
			recorder = &statusRecorder{ResponseWriter: w}
		}
		start := time.Now()
		next.ServeHTTP(recorder, r)
		if !a.opts.AccessLog || quietPath(r.URL.Path) {
			return
		}
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		attributes := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", a.clientIP(r),
			"request_id", requestIDFromContext(r.Context()),
		}
		switch {
		case status >= 500:
			slog.Error("http request", attributes...)
		case status >= 400:
			slog.Warn("http request", attributes...)
		default:
			slog.Info("http request", attributes...)
		}
	})
}

func (a *api) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder, ok := w.(*statusRecorder)
		if !ok {
			recorder = &statusRecorder{ResponseWriter: w}
		}
		start := time.Now()
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		route := r.Pattern
		if index := strings.IndexByte(route, ' '); index >= 0 {
			route = route[index+1:]
		}
		if route == "" {
			route = "unmatched"
		}
		a.metrics.Inc(a.metricName("http_requests_total"), map[string]string{
			"method": r.Method,
			"route":  route,
			"status": strconv.Itoa(status),
		})
		a.metrics.Observe(a.metricName("http_request_duration_seconds"), time.Since(start).Seconds(), map[string]string{
			"method": r.Method,
			"route":  route,
		})
	})
}

func (a *api) rateLimit(limiter *keyedLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow(a.clientIP(r)) {
			a.metrics.Inc(a.metricName("rate_limited_total"), nil)
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next(w, r)
	}
}

func (a *api) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokens := a.service.APITokens()
		if len(tokens) == 0 {
			writeError(w, http.StatusServiceUnavailable, "control endpoint is disabled")
			return
		}
		ip := a.clientIP(r)
		if a.failures.Blocked(ip) {
			a.metrics.Inc(a.metricName("auth_blocked_total"), nil)
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "too many authorization failures")
			return
		}
		if !headerMatchesBearer(r.Header.Get("Authorization"), tokens) {
			a.failures.Record(ip)
			a.authFailure(r, "bearer")
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "authorization required")
			return
		}
		a.failures.Reset(ip)
		next(w, r)
	}
}

// requireViewAuth protects the HTML dashboard when an API token is configured.
// Browsers use HTTP Basic (the token is the password); API clients can use the
// same Bearer header as the JSON endpoints. With no token the dashboard remains
// available on the local-only bind, matching the disabled control endpoints.
func (a *api) requireViewAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokens := a.service.APITokens()
		if len(tokens) == 0 {
			next(w, r)
			return
		}
		ip := a.clientIP(r)
		if a.failures.Blocked(ip) {
			a.metrics.Inc(a.metricName("auth_blocked_total"), nil)
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "too many authorization failures")
			return
		}
		if headerMatchesBearer(r.Header.Get("Authorization"), tokens) {
			a.failures.Reset(ip)
			next(w, r)
			return
		}
		if _, password, ok := r.BasicAuth(); ok && passwordMatches(password, tokens) {
			a.failures.Reset(ip)
			next(w, r)
			return
		}
		a.failures.Record(ip)
		a.authFailure(r, "view")
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s", charset="UTF-8"`, a.opts.Title))
		writeError(w, http.StatusUnauthorized, "authorization required")
	}
}

// headerMatchesBearer checks every active token and always visits all of them
// so response timing does not reveal which token matched.
func headerMatchesBearer(header string, tokens []string) bool {
	matched := false
	for _, token := range tokens {
		if secureEquals(header, "Bearer "+token) {
			matched = true
		}
	}
	return matched
}

func passwordMatches(password string, tokens []string) bool {
	matched := false
	for _, token := range tokens {
		if secureEquals(password, token) {
			matched = true
		}
	}
	return matched
}

func (a *api) authFailure(r *http.Request, scheme string) {
	a.metrics.Inc(a.metricName("auth_failures_total"), map[string]string{"scheme": scheme})
	slog.Warn("control API authorization failed",
		"scheme", scheme,
		"remote", a.clientIP(r),
		"method", r.Method,
		"path", r.URL.Path,
		"request_id", requestIDFromContext(r.Context()),
	)
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func quietPath(path string) bool {
	switch path {
	case "/live", "/ready", "/health", "/metrics":
		return true
	default:
		return false
	}
}
