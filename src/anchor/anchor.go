// Package anchor derives a location timeline from calendar events tagged with
// #anchor, #return, or #home.
//
// An #anchor event moves the active location to the event's location after the
// event ends. A #return or #home event reverts to the home location after it
// ends. A persisted state keeps an override alive across runs, and an optional
// cooldown reverts to home after the last relevant event.
package anchor

import (
	"slices"
	"time"
)

// Event is the subset of a calendar event that anchor handling needs.
type Event struct {
	Summary     string
	Description string
	Location    string
	End         time.Time
}

// Entry is one point in the location timeline.
type Entry struct {
	EffectiveFrom time.Time `json:"effectiveFrom"`
	Location      string    `json:"location"`
}

// Timeline is a chronological list of location changes.
type Timeline []Entry

// State is the persisted "current location" override.
type State struct {
	Location       string `json:"location,omitempty"`
	ExpiresAtEpoch int64  `json:"expiresAtEpoch,omitempty"`
}

// Expired reports whether the override has passed its expiry.
func (s State) Expired(now time.Time) bool {
	return s.ExpiresAtEpoch > 0 && now.Unix() >= s.ExpiresAtEpoch
}

var pastSentinel = time.Date(1970, time.January, 2, 0, 0, 0, 0, time.UTC)

// Result is the output of Build.
type Result struct {
	Timeline Timeline
	State    State
	Warnings []string
}

// Build derives the timeline and the state to persist.
//
// Rules, in priority order:
//  1. Start from home.
//  2. A live persisted override replaces the baseline.
//  3. An #anchor event moves the location to its Location after the event ends.
//  4. A #return or #home event reverts to home after the event ends.
//  5. If an override remains active after the final event and cooldown is
//     positive, revert to home at the final event's end plus the cooldown.
func Build(events []Event, home string, cooldown time.Duration, persisted State, now time.Time) Result {
	var warnings []string
	timeline := Timeline{{EffectiveFrom: pastSentinel, Location: home}}

	if persisted.Location != "" {
		if persisted.Expired(now) {
			warnings = append(warnings, "persisted anchor override has expired")
		} else {
			timeline[0].Location = persisted.Location
			if persisted.ExpiresAtEpoch > 0 {
				timeline = append(timeline, Entry{
					EffectiveFrom: time.Unix(persisted.ExpiresAtEpoch, 0).UTC(),
					Location:      home,
				})
			}
		}
	}

	sorted := slices.Clone(events)
	slices.SortStableFunc(sorted, func(a, b Event) int {
		return a.End.Compare(b.End)
	})

	var last Event
	hasLast := false
	for _, event := range sorted {
		if event.End.IsZero() {
			continue
		}
		last = event
		hasLast = true
		isAnchor, isReturn := tags(event.Summary + " " + event.Description)
		switch {
		case isAnchor:
			if event.Location == "" {
				warnings = append(warnings, "anchor event has no location")
				continue
			}
			timeline = append(timeline, Entry{EffectiveFrom: event.End, Location: event.Location})
		case isReturn:
			timeline = append(timeline, Entry{EffectiveFrom: event.End, Location: home})
		}
	}

	sortTimeline(timeline)
	if hasLast && cooldown > 0 && Resolve(timeline, last.End) != home {
		timeline = append(timeline, Entry{EffectiveFrom: last.End.Add(cooldown), Location: home})
		sortTimeline(timeline)
	}

	return Result{
		Timeline: timeline,
		State:    CurrentState(timeline, home, now),
		Warnings: warnings,
	}
}

// Resolve returns the active location at an instant.
func Resolve(timeline Timeline, at time.Time) string {
	if len(timeline) == 0 {
		return ""
	}
	result := timeline[0].Location
	for _, entry := range timeline {
		if entry.EffectiveFrom.After(at) {
			break
		}
		result = entry.Location
	}
	return result
}

// CurrentState derives the override to persist. Its expiry is the next
// transition back to home, so repeated runs cannot slide the cooldown.
func CurrentState(timeline Timeline, home string, now time.Time) State {
	active := Resolve(timeline, now)
	if active == "" || active == home {
		return State{}
	}
	state := State{Location: active}
	for _, entry := range timeline {
		if entry.EffectiveFrom.After(now) && entry.Location == home {
			state.ExpiresAtEpoch = entry.EffectiveFrom.Unix()
			break
		}
	}
	return state
}

func sortTimeline(timeline Timeline) {
	slices.SortStableFunc(timeline, func(a, b Entry) int {
		return a.EffectiveFrom.Compare(b.EffectiveFrom)
	})
}

func tags(text string) (isAnchor, isReturn bool) {
	for _, word := range fields(text) {
		switch word {
		case "#anchor":
			isAnchor = true
		case "#return", "#home":
			isReturn = true
		}
	}
	return isAnchor, isReturn
}

func fields(text string) []string {
	var result []string
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		result = append(result, string(current))
		current = current[:0]
	}
	for _, r := range text {
		switch r {
		case ' ', '\t', '\n', '\r', ',', '.', '!', '?', ';', ':', ')', '(', '"', '\'':
			flush()
		default:
			current = append(current, r)
		}
	}
	flush()
	return result
}
