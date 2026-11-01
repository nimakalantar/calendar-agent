// Package retry runs operations with bounded, context-aware backoff.
package retry

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

// DefaultMaxServerDelay caps a server-advertised Retry-After so a large value
// cannot stall a request. Adapters may pass a smaller cap when they have one.
const DefaultMaxServerDelay = 30 * time.Second

// StatusError is an error carrying an HTTP status and response headers so
// adapters can use the shared retry classifiers instead of defining their own
// error types.
type StatusError struct {
	// Label prefixes the message, e.g. "OTP request failed".
	Label  string
	Status int
	Header http.Header
}

func (e *StatusError) Error() string {
	label := e.Label
	if label == "" {
		label = "request failed"
	}
	return fmt.Sprintf("%s: HTTP %d", label, e.Status)
}

// Classify reports whether err is transient: a StatusError with a retryable
// status, or any network error.
func Classify(err error) bool {
	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		return RetryableStatus(statusErr.Status)
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

// ServerDelayFromError extracts a server-advertised wait from err, capped at
// max. It returns zero when err carries no Retry-After header.
func ServerDelayFromError(err error, max time.Duration) time.Duration {
	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		return ServerDelay(statusErr.Header, max)
	}
	return 0
}

// Policy controls bounded retries.
type Policy struct {
	Attempts  int
	BaseDelay time.Duration
	MaxDelay  time.Duration
}

// Delay returns the wait before the next attempt, using the larger of the
// linear backoff and a server-advertised wait, capped at MaxDelay.
func (p Policy) Delay(attempt int, server time.Duration) time.Duration {
	delay := p.BaseDelay * time.Duration(attempt+1)
	if server > delay {
		delay = server
	}
	if p.MaxDelay > 0 && delay > p.MaxDelay {
		delay = p.MaxDelay
	}
	return delay
}

// Classifier decides whether an error is retryable and how long a server
// asked us to wait. Nil callbacks mean "not retryable" and "no server delay".
type Classifier struct {
	Retryable   func(error) bool
	ServerDelay func(error) time.Duration
}

// DefaultClassifier retries transient StatusError and network failures and
// honours a Retry-After header up to DefaultMaxServerDelay.
var DefaultClassifier = Classifier{
	Retryable: Classify,
	ServerDelay: func(err error) time.Duration {
		return ServerDelayFromError(err, DefaultMaxServerDelay)
	},
}

// DoValue runs fn, retrying while classify.Retryable reports true and attempts
// remain. classify.ServerDelay extracts a server-advertised wait from err (zero
// when absent). It returns ctx.Err() if the context is cancelled while waiting.
func DoValue[T any](ctx context.Context, p Policy, classify Classifier, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	attempts := max(p.Attempts, 1)
	for attempt := 0; ; attempt++ {
		value, err := fn(ctx)
		if err == nil {
			return value, nil
		}
		if attempt == attempts-1 || classify.Retryable == nil || !classify.Retryable(err) {
			return zero, err
		}
		var server time.Duration
		if classify.ServerDelay != nil {
			server = classify.ServerDelay(err)
		}
		if !Sleep(ctx, p.Delay(attempt, server)) {
			return zero, ctx.Err()
		}
	}
}

// Do runs a value-less operation with retries.
func Do(ctx context.Context, p Policy, classify Classifier, fn func(context.Context) error) error {
	_, err := DoValue(ctx, p, classify, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	})
	return err
}

// Sleep reports whether delay elapsed before ctx was cancelled. A non-positive
// delay returns immediately.
func Sleep(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// RetryableStatus reports whether an HTTP status warrants a retry.
func RetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// ServerDelay parses a Retry-After header, capped at max. It returns zero when
// the header is absent, malformed, or in an unsupported HTTP-date form.
func ServerDelay(header http.Header, max time.Duration) time.Duration {
	if header == nil {
		return 0
	}
	value := header.Get("Retry-After")
	if value == "" {
		return 0
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0
	}
	delay := time.Duration(seconds) * time.Second
	if max > 0 && delay > max {
		delay = max
	}
	return delay
}
