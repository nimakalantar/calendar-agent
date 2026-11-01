package calendaragent

import "time"

// ManualEditPolicy controls what happens when the observed event no longer
// matches the content hash the agent last wrote. Calendar users often move a
// generated event deliberately; an agent can either respect that or restore the
// computed schedule.
type ManualEditPolicy string

const (
	// UpdateManualEdits rewrites the event to match the desired state.
	UpdateManualEdits ManualEditPolicy = "update"
	// PreserveManualEdits leaves a manually edited event untouched and reports
	// a NoOp with the reason recorded.
	PreserveManualEdits ManualEditPolicy = "preserve"
)

// Options configures one reconciliation pass.
type Options struct {
	// Window is the half-open horizon in which events are managed.
	Window Window
	// Now is the instant used to distinguish future events from calendar
	// history. A zero Now treats no event as started.
	Now time.Time

	// Hash returns the content hash written into generated events. It defaults
	// to DefaultHash.
	Hash func(Event) string
	// ObservedHash returns the hash of the fields currently observed on a
	// managed event. It is required to detect manual edits when the policy is
	// PreserveManualEdits.
	ObservedHash func(ManagedEvent) string
	// Matches reports whether a managed event already equals the desired
	// event. It defaults to DefaultMatches.
	Matches func(Event, ManagedEvent) bool

	ManualEditPolicy ManualEditPolicy
}

func (o Options) hash(event Event) string {
	if o.Hash != nil {
		return o.Hash(event)
	}
	return DefaultHash(event)
}

func (o Options) matches(desired Event, observed ManagedEvent) bool {
	if o.Matches != nil {
		return o.Matches(desired, observed)
	}
	return defaultMatches(desired, observed, o.hash)
}

func (o Options) manuallyEdited(observed ManagedEvent) bool {
	if observed.ContentHash == "" || o.ObservedHash == nil {
		return false
	}
	return o.ObservedHash(observed) != observed.ContentHash
}

// Reconcile diffs desired events against existing managed events.
//
// Matching consumes future events: every owned, unstarted event that is not
// claimed by a desired event is deleted. Started events remain as calendar
// history, even when they are still inside the reconciliation window.
func Reconcile(opts Options, desired []Event, existing []ManagedEvent) []Action {
	if opts.ManualEditPolicy == "" {
		opts.ManualEditPolicy = UpdateManualEdits
	}

	var actions []Action

	// Index owned existing events by identity so the diff is linear rather than
	// O(desired x existing).
	byKey := map[string][]int{}
	for i, event := range existing {
		if !event.Owned {
			continue
		}
		key := managedKey(event)
		byKey[key] = append(byKey[key], i)
	}

	matched := make([]bool, len(existing))

	for _, event := range desired {
		key := desiredKey(event)
		if startedAtOrBefore(event.Start, opts.Now) {
			// Past events are not recreated or rewritten. Claim a matching started
			// event only to report that its history is intentionally retained.
			for _, i := range byKey[key] {
				if !matched[i] && startedAtOrBefore(existing[i].Start, opts.Now) {
					matched[i] = true
					actions = append(actions, Action{
						Kind: NoOp, Event: event, ExistingEventID: existing[i].ID,
						Reason: "started event retained as calendar history",
					})
					break
				}
			}
			continue
		}

		matchingIndex := -1
		for _, i := range byKey[key] {
			if !matched[i] && !startedAtOrBefore(existing[i].Start, opts.Now) {
				matchingIndex = i
				break
			}
		}

		if matchingIndex < 0 {
			actions = append(actions, Action{
				Kind: Create, Event: event, Reason: "missing generated event",
			})
			continue
		}

		matched[matchingIndex] = true
		observed := existing[matchingIndex]
		switch {
		case opts.matches(event, observed):
			actions = append(actions, Action{
				Kind: NoOp, Event: event, ExistingEventID: observed.ID,
				Reason: "existing generated event already matches the desired state",
			})
		case opts.ManualEditPolicy == PreserveManualEdits && opts.manuallyEdited(observed):
			actions = append(actions, Action{
				Kind: NoOp, Event: event, ExistingEventID: observed.ID,
				Reason: "preserved manual edit",
			})
		default:
			actions = append(actions, Action{
				Kind: Update, Event: event, ExistingEventID: observed.ID,
				Reason: "generated event differs from the desired state",
			})
		}
	}

	return append(actions, unmatchedDeletions(opts, existing, matched)...)
}

func desiredKey(event Event) string {
	if event.Key != "" {
		return event.Key
	}
	return event.SourceKey
}

func managedKey(event ManagedEvent) string {
	if event.Key != "" {
		return event.Key
	}
	if event.SourceKey != "" {
		return event.SourceKey
	}
	return event.ID
}

func startedAtOrBefore(start, now time.Time) bool {
	return !now.IsZero() && !start.After(now)
}

// unmatchedDeletions removes future owned events that no desired event
// claimed. Started events are always retained as calendar history, even inside
// the window.
func unmatchedDeletions(opts Options, existing []ManagedEvent, matched []bool) []Action {
	var actions []Action
	for i, event := range existing {
		if !event.Owned || matched[i] || startedAtOrBefore(event.Start, opts.Now) {
			continue
		}
		if !opts.Window.Overlaps(event.Start, event.End) {
			if opts.Now.IsZero() || event.Start.After(opts.Now) {
				actions = append(actions, Action{
					Kind: Delete, ExistingEventID: event.ID,
					Reason: "generated event is outside the reconciliation window",
				})
			}
			continue
		}
		actions = append(actions, Action{
			Kind: Delete, ExistingEventID: event.ID,
			Reason: "duplicate or no-longer-required generated event",
		})
	}
	return actions
}
