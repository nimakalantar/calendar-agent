package gcalendar

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"google.golang.org/api/googleapi"
)

func TestWithRetryRetriesTransientStatus(t *testing.T) {
	attempts := 0
	got, err := withRetry(context.Background(), func(context.Context) (string, error) {
		attempts++
		if attempts < 2 {
			return "", &googleapi.Error{Code: http.StatusInternalServerError}
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("expected retry to succeed: %v", err)
	}
	if got != "ok" || attempts != 2 {
		t.Fatalf("got %q after %d attempts, want ok after 2", got, attempts)
	}
}

func TestWithRetryDoesNotRetryClientError(t *testing.T) {
	attempts := 0
	_, err := withRetry(context.Background(), func(context.Context) (string, error) {
		attempts++
		return "", &googleapi.Error{Code: http.StatusForbidden}
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestGoogleRetryableRateLimit(t *testing.T) {
	err := &googleapi.Error{
		Code:   http.StatusForbidden,
		Errors: []googleapi.ErrorItem{{Reason: "rateLimitExceeded"}},
	}
	if !googleRetryable(err) {
		t.Fatal("403 rateLimitExceeded should be retryable")
	}
	if googleRetryable(&googleapi.Error{Code: http.StatusForbidden}) {
		t.Fatal("plain 403 should not be retryable")
	}
}

func TestIsTransientAPIError(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		if !IsTransientAPIError(&googleapi.Error{Code: status}) {
			t.Fatalf("status %d should be transient", status)
		}
	}
	for _, status := range []int{http.StatusOK, http.StatusForbidden, http.StatusNotFound} {
		if IsTransientAPIError(&googleapi.Error{Code: status}) {
			t.Fatalf("status %d should not be transient", status)
		}
	}
	if IsTransientAPIError(errors.New("plain")) {
		t.Fatal("non-API error should not be transient")
	}
}

func TestIsGone(t *testing.T) {
	if !IsGone(&googleapi.Error{Code: http.StatusNotFound}) {
		t.Fatal("404 should be gone")
	}
	if !IsGone(&googleapi.Error{Code: http.StatusGone}) {
		t.Fatal("410 should be gone")
	}
	if IsGone(&googleapi.Error{Code: http.StatusForbidden}) {
		t.Fatal("403 should not be gone")
	}
	if IsGone(errors.New("plain")) {
		t.Fatal("non-API error should not be gone")
	}
}
