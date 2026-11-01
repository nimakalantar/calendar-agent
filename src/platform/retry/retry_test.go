package retry

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestDoValueRetriesThenSucceeds(t *testing.T) {
	attempts := 0
	policy := Policy{Attempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Second}
	got, err := DoValue(context.Background(), policy, Classifier{Retryable: func(error) bool { return true }},
		func(context.Context) (int, error) {
			attempts++
			if attempts < 3 {
				return 0, errors.New("transient")
			}
			return 42, nil
		})
	if err != nil || got != 42 || attempts != 3 {
		t.Fatalf("got (%d, %v) after %d attempts", got, err, attempts)
	}
}

func TestDoValueStopsOnNonRetryable(t *testing.T) {
	attempts := 0
	policy := Policy{Attempts: 3, BaseDelay: time.Millisecond}
	sentinel := errors.New("permanent")
	_, err := DoValue(context.Background(), policy, Classifier{Retryable: func(error) bool { return false }},
		func(context.Context) (int, error) {
			attempts++
			return 0, sentinel
		})
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want sentinel", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestDoValueHonoursContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	policy := Policy{Attempts: 3, BaseDelay: time.Hour}
	_, err := DoValue(ctx, policy, Classifier{Retryable: func(error) bool { return true }},
		func(context.Context) (int, error) { return 0, errors.New("transient") })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestPolicyDelay(t *testing.T) {
	policy := Policy{Attempts: 3, BaseDelay: 500 * time.Millisecond, MaxDelay: 30 * time.Second}
	if got := policy.Delay(0, 0); got != 500*time.Millisecond {
		t.Fatalf("delay = %v", got)
	}
	if got := policy.Delay(1, 0); got != time.Second {
		t.Fatalf("delay = %v", got)
	}
	if got := policy.Delay(0, 5*time.Second); got != 5*time.Second {
		t.Fatalf("server delay should win, got %v", got)
	}
	if got := policy.Delay(0, 10*time.Minute); got != 30*time.Second {
		t.Fatalf("delay should cap, got %v", got)
	}
}

func TestServerDelay(t *testing.T) {
	if got := ServerDelay(http.Header{"Retry-After": []string{"5"}}, 30*time.Second); got != 5*time.Second {
		t.Fatalf("delay = %v", got)
	}
	if got := ServerDelay(http.Header{"Retry-After": []string{"nonsense"}}, 30*time.Second); got != 0 {
		t.Fatalf("delay = %v, want 0", got)
	}
	if got := ServerDelay(nil, time.Second); got != 0 {
		t.Fatalf("nil header delay = %v", got)
	}
}

func TestRetryableStatus(t *testing.T) {
	for _, status := range []int{429, 500, 503} {
		if !RetryableStatus(status) {
			t.Fatalf("status %d should be retryable", status)
		}
	}
	for _, status := range []int{200, 400, 403, 404} {
		if RetryableStatus(status) {
			t.Fatalf("status %d should not be retryable", status)
		}
	}
}

func TestSleepReturnsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if Sleep(ctx, time.Hour) {
		t.Fatal("sleep should report cancellation")
	}
	if !Sleep(ctx, 0) {
		t.Fatal("zero sleep should return immediately")
	}
}

func TestDefaultClassifierHonoursRetryAfter(t *testing.T) {
	err := &StatusError{Label: "test", Status: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"7"}}}
	if !DefaultClassifier.Retryable(err) {
		t.Fatal("429 should be retryable")
	}
	if got := DefaultClassifier.ServerDelay(err); got != 7*time.Second {
		t.Fatalf("server delay = %v, want 7s", got)
	}
}
