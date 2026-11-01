package gcalendar

import (
	"context"
	"errors"
	"time"

	"google.golang.org/api/googleapi"

	"github.com/nimakalantar/calendar-agent/src/platform/retry"
)

// googleRetry bounds retries for transient Google API failures.
var googleRetry = retry.Policy{Attempts: 3, BaseDelay: 500 * time.Millisecond, MaxDelay: retry.DefaultMaxServerDelay}

var googleClassifier = retry.Classifier{
	Retryable:   googleRetryable,
	ServerDelay: googleServerDelay,
}

// withRetry runs fn with the Google API retry policy. fn is re-run from
// scratch on each attempt so paginated results are never duplicated.
func withRetry[T any](ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	return retry.DoValue(ctx, googleRetry, googleClassifier, fn)
}

func retryVoid(ctx context.Context, fn func(context.Context) error) error {
	return retry.Do(ctx, googleRetry, googleClassifier, fn)
}

func googleRetryable(err error) bool {
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		return retry.RetryableStatus(apiErr.Code)
	}
	return retry.Classify(err)
}

func googleServerDelay(err error) time.Duration {
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		return retry.ServerDelay(apiErr.Header, retry.DefaultMaxServerDelay)
	}
	return 0
}
