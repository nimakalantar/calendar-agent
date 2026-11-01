package calendaragent

import (
	"context"
	"time"
)

// Backend is the boundary between the reconciler and a concrete calendar
// provider. Implementations own authentication, HTTP, payload shape, and the
// translation between Event/ManagedEvent and provider objects.
type Backend interface {
	// ListManaged returns owned and unowned events in the destination calendar
	// for the requested horizon. It must populate ID, Key, times, summary,
	// description, presentation, Owned, and ContentHash.
	ListManaged(ctx context.Context, horizonStart time.Time, days int) ([]ManagedEvent, error)

	Create(ctx context.Context, event Event) (string, error)
	Update(ctx context.Context, eventID string, event Event) (string, error)
	Delete(ctx context.Context, eventID string) error
}
