// Package health aggregates dependency probes for readiness reporting.
//
// A report is cached briefly so kubelet, load balancers, and the container
// healthcheck can poll cheaply without hammering OTP, Google, or the
// filesystem. Critical probe failures make the service unavailable; optional
// probe failures only degrade it.
package health

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// Status is the aggregate state of a report or a single check.
type Status string

const (
	// StatusOK means every probe succeeded.
	StatusOK Status = "ok"
	// StatusDegraded means a non-critical probe failed.
	StatusDegraded Status = "degraded"
	// StatusUnavailable means a critical probe failed.
	StatusUnavailable Status = "unavailable"
)

// Check is one probe result. Error text is intentionally omitted from JSON:
// provider errors can contain internal URLs and identifiers.
type Check struct {
	Name       string `json:"name"`
	Status     Status `json:"status"`
	Critical   bool   `json:"critical"`
	DurationMS int64  `json:"durationMs"`
}

// Report is an aggregate readiness report.
type Report struct {
	Status    Status    `json:"status"`
	CheckedAt time.Time `json:"checkedAt"`
	Checks    []Check   `json:"checks"`
}

// Probe is one dependency check. Critical probes gate readiness.
type Probe struct {
	Name     string
	Critical bool
	Run      func(context.Context) error
}

// Observer receives every fresh report, for example to update metrics.
type Observer func(Report)

// Option configures a Checker.
type Option func(*Checker)

// WithTimeout bounds the whole probe run. Zero uses 5 seconds.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Checker) {
		if timeout > 0 {
			c.timeout = timeout
		}
	}
}

// WithCacheTTL controls how long a report is reused. Zero uses 30 seconds.
func WithCacheTTL(ttl time.Duration) Option {
	return func(c *Checker) {
		if ttl > 0 {
			c.ttl = ttl
		}
	}
}

// WithNow replaces the clock, for tests.
func WithNow(now func() time.Time) Option {
	return func(c *Checker) {
		if now != nil {
			c.now = now
		}
	}
}

// WithObserver registers a report observer.
func WithObserver(observer Observer) Option {
	return func(c *Checker) {
		c.observer = observer
	}
}

// Checker runs probes and caches the aggregate result.
type Checker struct {
	probes   []Probe
	timeout  time.Duration
	ttl      time.Duration
	now      func() time.Time
	observer Observer

	mu       sync.Mutex
	cached   Report
	cachedAt time.Time
	inflight chan struct{}
	last     Report
}

// New builds a Checker.
func New(probes []Probe, opts ...Option) *Checker {
	c := &Checker{
		probes:  append([]Probe(nil), probes...),
		timeout: 5 * time.Second,
		ttl:     30 * time.Second,
		now:     time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Probes returns the configured probe names, for startup logging.
func (c *Checker) Probes() []string {
	if c == nil {
		return nil
	}
	names := make([]string, 0, len(c.probes))
	for _, probe := range c.probes {
		names = append(names, probe.Name)
	}
	return names
}

// Check returns a cached report when fresh, otherwise runs the probes. Only one
// caller runs probes at a time; concurrent callers wait for that result.
func (c *Checker) Check(ctx context.Context) Report {
	if c == nil {
		return Report{Status: StatusOK, CheckedAt: time.Now()}
	}

	for {
		c.mu.Lock()
		if !c.cachedAt.IsZero() && c.now().Sub(c.cachedAt) < c.ttl {
			report := cloneReport(c.cached)
			c.mu.Unlock()
			return report
		}
		if c.inflight == nil {
			c.inflight = make(chan struct{})
			c.mu.Unlock()
			break
		}
		wait := c.inflight
		c.mu.Unlock()
		select {
		case <-wait:
			continue
		case <-ctx.Done():
			return Report{Status: StatusUnavailable, CheckedAt: c.now()}
		}
	}

	report := c.run(ctx)

	c.mu.Lock()
	c.cached = report
	c.cachedAt = c.now()
	c.last = report
	close(c.inflight)
	c.inflight = nil
	c.mu.Unlock()

	if c.observer != nil {
		c.observer(cloneReport(report))
	}
	return cloneReport(report)
}

// Last returns the most recent report without running probes.
func (c *Checker) Last() Report {
	if c == nil {
		return Report{Status: StatusOK}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneReport(c.last)
}

func (c *Checker) run(ctx context.Context) Report {
	checkCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	report := Report{Status: StatusOK, CheckedAt: c.now(), Checks: make([]Check, 0, len(c.probes))}
	for _, probe := range c.probes {
		start := c.now()
		err := probe.Run(checkCtx)
		check := Check{
			Name:       probe.Name,
			Status:     StatusOK,
			Critical:   probe.Critical,
			DurationMS: c.now().Sub(start).Milliseconds(),
		}
		if err != nil {
			check.Status = StatusDegraded
			if probe.Critical {
				check.Status = StatusUnavailable
				report.Status = StatusUnavailable
			} else if report.Status == StatusOK {
				report.Status = StatusDegraded
			}
			slog.Warn("readiness probe failed",
				"probe", probe.Name,
				"critical", probe.Critical,
				"duration_ms", check.DurationMS,
				"error", err,
			)
		}
		report.Checks = append(report.Checks, check)
	}
	return report
}

func cloneReport(report Report) Report {
	report.Checks = append([]Check(nil), report.Checks...)
	return report
}

// ErrUnavailable is returned by Probes when a report is not ready.
var ErrUnavailable = errors.New("service is not ready")
