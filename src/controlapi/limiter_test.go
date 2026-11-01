package controlapi

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func mustProxies(t *testing.T, values ...string) []netip.Prefix {
	t.Helper()
	prefixes, err := ParseTrustedProxies(values)
	if err != nil {
		t.Fatal(err)
	}
	return prefixes
}

func TestParseTrustedProxies(t *testing.T) {
	prefixes := mustProxies(t, "127.0.0.1", "10.0.0.0/8")
	if len(prefixes) != 2 {
		t.Fatalf("prefixes = %v", prefixes)
	}
	if !prefixes[0].Contains(netip.MustParseAddr("127.0.0.1")) {
		t.Fatalf("host prefix = %v", prefixes[0])
	}
	if !prefixes[1].Contains(netip.MustParseAddr("10.2.3.4")) {
		t.Fatalf("range prefix = %v", prefixes[1])
	}
	if _, err := ParseTrustedProxies([]string{"not-an-ip"}); err == nil {
		t.Fatal("expected invalid proxy error")
	}
}

func TestClientIPIgnoresForwardedForFromUntrustedPeer(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "203.0.113.9:1234"
	request.Header.Set("X-Forwarded-For", "198.51.100.7")
	if got := clientIP(request, mustProxies(t, "10.0.0.0/8")); got != "203.0.113.9" {
		t.Fatalf("client IP = %q", got)
	}
}

func TestClientIPUsesForwardedForFromTrustedProxy(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.5:1234"
	request.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.6")
	if got := clientIP(request, mustProxies(t, "10.0.0.0/8")); got != "198.51.100.7" {
		t.Fatalf("client IP = %q", got)
	}
}

func TestKeyedLimiterIsolatesClients(t *testing.T) {
	limiter := newKeyedLimiter(1, 100)
	if !limiter.Allow("a") {
		t.Fatal("first request for a should pass")
	}
	if limiter.Allow("a") {
		t.Fatal("second request for a should be limited")
	}
	if !limiter.Allow("b") {
		t.Fatal("a different client should have its own bucket")
	}
}

func TestFailureTrackerBlocksAndResets(t *testing.T) {
	tracker := newFailureTracker(2)
	tracker.Record("a")
	if tracker.Blocked("a") {
		t.Fatal("blocked before the limit")
	}
	tracker.Record("a")
	if !tracker.Blocked("a") {
		t.Fatal("expected block after the limit")
	}
	tracker.Reset("a")
	if tracker.Blocked("a") {
		t.Fatal("reset did not clear the block")
	}
}

func TestFailureTrackerWindowExpires(t *testing.T) {
	tracker := newFailureTracker(2)
	now := time.Unix(1000, 0)
	tracker.now = func() time.Time { return now }
	tracker.Record("a")
	now = now.Add(2 * time.Minute)
	tracker.Record("a")
	if tracker.Blocked("a") {
		t.Fatal("failures outside the window should not accumulate")
	}
}

func TestHSTSBehindTrustedTLSProxy(t *testing.T) {
	svc := FakeService{}
	handler := HandlerWithOptions(svc, Options{
		TrustedProxies:      mustProxies(t, "127.0.0.1"),
		TrustTLSTermination: true,
	})
	request := httptest.NewRequest(http.MethodGet, "/live", nil)
	request.RemoteAddr = "127.0.0.1:5555"
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if got := recorder.Header().Get("Strict-Transport-Security"); got == "" {
		t.Fatal("missing HSTS behind trusted TLS proxy")
	}
}

func TestHSTSNotSentForUntrustedProxy(t *testing.T) {
	svc := FakeService{}
	handler := HandlerWithOptions(svc, Options{
		TrustedProxies:      mustProxies(t, "10.0.0.0/8"),
		TrustTLSTermination: true,
	})
	request := httptest.NewRequest(http.MethodGet, "/live", nil)
	request.RemoteAddr = "203.0.113.9:5555"
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if got := recorder.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("unexpected HSTS = %q", got)
	}
}
