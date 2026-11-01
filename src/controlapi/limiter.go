package controlapi

import (
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// keyedLimiter applies a token bucket per client key. Entries are evicted when
// they have been idle for a while so a spoofed-key flood cannot grow memory
// without bound.
type keyedLimiter struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	rate    rate.Limit
	burst   int
	max     int
	now     func() time.Time
}

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newKeyedLimiter(perMinute, maxKeys int) *keyedLimiter {
	if perMinute <= 0 {
		return nil
	}
	burst := perMinute / 6
	if burst < 1 {
		burst = 1
	}
	if maxKeys <= 0 {
		maxKeys = 10000
	}
	return &keyedLimiter{
		entries: map[string]*limiterEntry{},
		rate:    rate.Every(time.Minute / time.Duration(perMinute)),
		burst:   burst,
		max:     maxKeys,
		now:     time.Now,
	}
}

func (l *keyedLimiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	now := l.now()
	l.mu.Lock()
	entry := l.entries[key]
	if entry == nil {
		if len(l.entries) >= l.max {
			l.evictLocked(now)
		}
		entry = &limiterEntry{limiter: rate.NewLimiter(l.rate, l.burst)}
		l.entries[key] = entry
	}
	entry.lastSeen = now
	l.mu.Unlock()
	return entry.limiter.Allow()
}

// evictLocked removes entries unused for ten minutes. If the map is still at
// capacity, it drops the least recently seen quarter.
func (l *keyedLimiter) evictLocked(now time.Time) {
	cutoff := now.Add(-10 * time.Minute)
	for key, entry := range l.entries {
		if entry.lastSeen.Before(cutoff) {
			delete(l.entries, key)
		}
	}
	if len(l.entries) < l.max {
		return
	}
	target := l.max - l.max/4
	for len(l.entries) > target {
		var oldestKey string
		var oldest time.Time
		for key, entry := range l.entries {
			if oldestKey == "" || entry.lastSeen.Before(oldest) {
				oldestKey = key
				oldest = entry.lastSeen
			}
		}
		delete(l.entries, oldestKey)
	}
}

// failureTracker blocks a client for a minute after repeated authorization
// failures. It complements the request limiter by making credential guessing
// progressively expensive.
type failureTracker struct {
	mu       sync.Mutex
	entries  map[string]*failureEntry
	limit    int
	window   time.Duration
	blockFor time.Duration
	now      func() time.Time
	max      int
}

type failureEntry struct {
	count        int
	windowStart  time.Time
	blockedUntil time.Time
}

func newFailureTracker(limit int) *failureTracker {
	if limit <= 0 {
		return nil
	}
	return &failureTracker{
		entries:  map[string]*failureEntry{},
		limit:    limit,
		window:   time.Minute,
		blockFor: time.Minute,
		now:      time.Now,
		max:      10000,
	}
}

func (f *failureTracker) Blocked(key string) bool {
	if f == nil {
		return false
	}
	now := f.now()
	f.mu.Lock()
	defer f.mu.Unlock()
	entry := f.entries[key]
	if entry == nil {
		return false
	}
	return now.Before(entry.blockedUntil)
}

func (f *failureTracker) Record(key string) {
	if f == nil {
		return
	}
	now := f.now()
	f.mu.Lock()
	defer f.mu.Unlock()
	entry := f.entries[key]
	if entry == nil {
		if len(f.entries) >= f.max {
			f.evictLocked(now)
		}
		entry = &failureEntry{windowStart: now}
		f.entries[key] = entry
	}
	if now.Sub(entry.windowStart) > f.window {
		entry.count = 0
		entry.windowStart = now
	}
	entry.count++
	if entry.count >= f.limit {
		entry.blockedUntil = now.Add(f.blockFor)
		entry.count = 0
		entry.windowStart = now
	}
}

func (f *failureTracker) Reset(key string) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.entries, key)
}

func (f *failureTracker) evictLocked(now time.Time) {
	for key, entry := range f.entries {
		if now.After(entry.blockedUntil) && now.Sub(entry.windowStart) > f.window {
			delete(f.entries, key)
		}
	}
	for len(f.entries) >= f.max {
		for key := range f.entries {
			delete(f.entries, key)
			break
		}
	}
}

// clientIP returns the address used for limiting and logging. Only proxies in
// the trusted set may supply X-Forwarded-For; otherwise the header is ignored
// so a client cannot choose its own limiter bucket.
func clientIP(r *http.Request, trusted []netip.Prefix) string {
	remote := remoteIP(r)
	addr, err := netip.ParseAddr(remote)
	if err != nil || !trustedAddr(addr, trusted) {
		return remote
	}
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded == "" {
		if real := r.Header.Get("X-Real-IP"); real != "" {
			forwarded = real
		}
	}
	if forwarded == "" {
		return remote
	}
	parts := strings.Split(forwarded, ",")
	// Walk from the right: the nearest untrusted hop is the real client.
	for index := len(parts) - 1; index >= 0; index-- {
		candidate, err := netip.ParseAddr(strings.TrimSpace(parts[index]))
		if err != nil {
			continue
		}
		if trustedAddr(candidate, trusted) {
			continue
		}
		return candidate.String()
	}
	return remote
}

func trustedAddr(addr netip.Addr, trusted []netip.Prefix) bool {
	for _, prefix := range trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func (a *api) clientIP(r *http.Request) string {
	return clientIP(r, a.trustedProxies)
}

func (a *api) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if !a.opts.TrustTLSTermination {
		return false
	}
	remote, err := netip.ParseAddr(remoteIP(r))
	if err != nil || !trustedAddr(remote, a.trustedProxies) {
		return false
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
