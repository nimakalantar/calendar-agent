package calendaragent

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// Fingerprint returns a stable SHA-256 hex digest. It returns "" for an empty
// value so absence remains distinguishable from a hash.
func Fingerprint(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// SameInstant reports whether two instants are equal, ignoring location and any
// monotonic clock component.
func SameInstant(a, b time.Time) bool {
	return a.UTC().Equal(b.UTC())
}

// DefaultHash covers everything the generic reconciler treats as user-visible
// plus the times and the opaque kind/link metadata.
func DefaultHash(event Event) string {
	return Fingerprint(strings.Join([]string{
		event.Summary,
		event.Description,
		event.Location,
		event.Kind,
		event.Start.UTC().Format(time.RFC3339),
		event.End.UTC().Format(time.RFC3339),
		event.Link,
	}, "\x00"))
}

// DefaultMatches compares the desired event with the observed event using
// DefaultHash.
func DefaultMatches(desired Event, observed ManagedEvent) bool {
	return defaultMatches(desired, observed, DefaultHash)
}

func defaultMatches(desired Event, observed ManagedEvent, hash func(Event) string) bool {
	if desired.SourceKey != observed.SourceKey ||
		desired.Summary != observed.Summary ||
		desired.Description != observed.Description ||
		desired.Location != observed.Location ||
		desired.Kind != observed.Kind ||
		!SameInstant(desired.Start, observed.Start) ||
		!SameInstant(desired.End, observed.End) ||
		!linkMatches(desired, observed) ||
		!PresentationEqual(desired.Presentation, observed.Presentation) {
		return false
	}
	if observed.ContentHash == "" {
		return true
	}
	return observed.ContentHash == hash(desired)
}

func linkMatches(desired Event, observed ManagedEvent) bool {
	if observed.LinkHash != "" {
		return Fingerprint(desired.Link) == observed.LinkHash
	}
	return desired.Link == observed.Link
}

// PresentationEqual reports whether two presentations are identical. Nil and
// empty reminder slices are treated as equal.
func PresentationEqual(a, b Presentation) bool {
	if a.Transparency != b.Transparency ||
		a.ColorID != b.ColorID ||
		a.StartTimeZone != b.StartTimeZone ||
		a.EndTimeZone != b.EndTimeZone ||
		a.UseDefaultReminders != b.UseDefaultReminders ||
		len(a.Reminders) != len(b.Reminders) {
		return false
	}
	for i := range a.Reminders {
		if a.Reminders[i] != b.Reminders[i] {
			return false
		}
	}
	return true
}
