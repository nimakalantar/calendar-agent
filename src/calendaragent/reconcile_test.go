package calendaragent

import (
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func at(loc *time.Location, year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, loc)
}

func testOptions(t *testing.T) Options {
	t.Helper()
	loc := mustLoc(t, "Europe/London")
	return Options{
		Window: NewWindow(at(loc, 2026, time.June, 1, 0, 0), 7),
		Now:    at(loc, 2026, time.June, 1, 0, 0),
	}
}

func testEvent(t *testing.T) Event {
	t.Helper()
	loc := mustLoc(t, "Europe/London")
	return Event{
		Key:         "arrival:evt-1",
		SourceKey:   "evt-1",
		Start:       at(loc, 2026, time.June, 2, 9, 0),
		End:         at(loc, 2026, time.June, 2, 9, 40),
		Summary:     "Event: Home -> Office",
		Description: "Take the train",
		Kind:        "outward",
		Link:        "https://otp/1",
	}
}

func managedFrom(t *testing.T, event Event) ManagedEvent {
	t.Helper()
	return ManagedEvent{
		ID:          "g1",
		Key:         event.Key,
		SourceKey:   event.SourceKey,
		Start:       event.Start,
		End:         event.End,
		Summary:     event.Summary,
		Description: event.Description,
		Kind:        event.Kind,
		Link:        event.Link,
		LinkHash:    Fingerprint(event.Link),
		Owned:       true,
		ContentHash: DefaultHash(event),
	}
}

func TestReconcileCreateNoOpUpdateDelete(t *testing.T) {
	opts := testOptions(t)
	event := testEvent(t)

	if got := Reconcile(opts, []Event{event}, nil); len(got) != 1 || got[0].Kind != Create {
		t.Fatalf("expected Create, got %+v", got)
	}
	existing := managedFrom(t, event)
	if got := Reconcile(opts, []Event{event}, []ManagedEvent{existing}); len(got) != 1 || got[0].Kind != NoOp {
		t.Fatalf("expected NoOp, got %+v", got)
	}

	changed := existing
	changed.LinkHash = Fingerprint("https://otp/2")
	if got := Reconcile(opts, []Event{event}, []ManagedEvent{changed}); len(got) != 1 || got[0].Kind != Update {
		t.Fatalf("expected Update, got %+v", got)
	}

	if got := Reconcile(opts, nil, []ManagedEvent{existing}); len(got) != 1 || got[0].Kind != Delete {
		t.Fatalf("expected Delete, got %+v", got)
	}
}

func TestReconcileNeverTouchesUnownedOrStarted(t *testing.T) {
	opts := testOptions(t)
	loc := mustLoc(t, "Europe/London")
	opts.Now = at(loc, 2026, time.June, 3, 12, 0)

	unowned := ManagedEvent{ID: "u1", Owned: false, Start: at(loc, 2026, time.June, 4, 9, 0), End: at(loc, 2026, time.June, 4, 10, 0)}
	started := ManagedEvent{ID: "s1", Owned: true, Key: "arrival:old", Start: at(loc, 2026, time.May, 3, 9, 0), End: at(loc, 2026, time.May, 3, 10, 0)}
	startedToday := ManagedEvent{ID: "s2", Owned: true, Key: "arrival:today", Start: at(loc, 2026, time.June, 2, 9, 0), End: at(loc, 2026, time.June, 2, 10, 0)}

	if got := Reconcile(opts, nil, []ManagedEvent{unowned, started, startedToday}); len(got) != 0 {
		t.Fatalf("expected no actions, got %+v", got)
	}
}

func TestReconcilePreservesHistoryAndDoesNotRecreatePastEvents(t *testing.T) {
	opts := testOptions(t)
	loc := mustLoc(t, "Europe/London")
	opts.Now = at(loc, 2026, time.June, 2, 10, 0)

	started := ManagedEvent{
		ID: "history", Owned: true, Key: "arrival:evt-1", SourceKey: "evt-1",
		Summary: "old title", Start: at(loc, 2026, time.June, 2, 9, 0), End: at(loc, 2026, time.June, 2, 9, 30),
	}
	past := Event{
		Key: "arrival:evt-1", SourceKey: "evt-1", Summary: "new title",
		Start: at(loc, 2026, time.June, 2, 9, 0), End: at(loc, 2026, time.June, 2, 9, 30),
	}

	actions := Reconcile(opts, []Event{past}, []ManagedEvent{started})
	if len(actions) != 1 || actions[0].Kind != NoOp || actions[0].ExistingEventID != "history" {
		t.Fatalf("started history was changed instead of kept: %+v", actions)
	}
	if actions := Reconcile(opts, []Event{past}, nil); len(actions) != 0 {
		t.Fatalf("past event should not be recreated: %+v", actions)
	}
}

func TestMovedEventCreatesFutureEventWithoutChangingHistory(t *testing.T) {
	opts := testOptions(t)
	loc := mustLoc(t, "Europe/London")
	opts.Now = at(loc, 2026, time.June, 2, 10, 0)

	started := ManagedEvent{
		ID: "history", Owned: true, Key: "arrival:evt-1", SourceKey: "evt-1",
		Start: at(loc, 2026, time.June, 2, 9, 0), End: at(loc, 2026, time.June, 2, 9, 30),
	}
	future := Event{
		Key: "arrival:evt-1", SourceKey: "evt-1",
		Start: at(loc, 2026, time.June, 2, 11, 0), End: at(loc, 2026, time.June, 2, 11, 30),
	}

	actions := Reconcile(opts, []Event{future}, []ManagedEvent{started})
	if len(actions) != 1 || actions[0].Kind != Create {
		t.Fatalf("future event should be created without editing history: %+v", actions)
	}
}

func TestContentHashCannotHideEditedFields(t *testing.T) {
	opts := testOptions(t)
	event := testEvent(t)
	existing := managedFrom(t, event)
	existing.Summary = "manually edited"

	actions := Reconcile(opts, []Event{event}, []ManagedEvent{existing})
	if len(actions) != 1 || actions[0].Kind != Update {
		t.Fatalf("stale content hash hid an event edit: %+v", actions)
	}
}

func TestPreserveManualEdit(t *testing.T) {
	opts := testOptions(t)
	opts.ManualEditPolicy = PreserveManualEdits
	opts.ObservedHash = func(event ManagedEvent) string {
		// Simulate a backend recomputing the hash from the observed payload.
		return DefaultHash(Event{
			Summary: event.Summary, Description: event.Description, Kind: event.Kind,
			Start: event.Start, End: event.End, Link: event.Link,
		})
	}
	event := testEvent(t)
	existing := managedFrom(t, event)
	existing.Start = existing.Start.Add(30 * time.Minute)
	existing.End = existing.End.Add(30 * time.Minute)

	actions := Reconcile(opts, []Event{event}, []ManagedEvent{existing})
	if len(actions) != 1 || actions[0].Kind != NoOp || actions[0].Reason != "preserved manual edit" {
		t.Fatalf("manual edit was not preserved: %+v", actions)
	}
}

func TestReconcileDeletesDuplicateOwnedEvents(t *testing.T) {
	opts := testOptions(t)
	event := testEvent(t)
	original := managedFrom(t, event)
	duplicate := original
	duplicate.ID = "g2"

	got := Reconcile(opts, []Event{event}, []ManagedEvent{original, duplicate})
	var noops, deletes int
	for _, action := range got {
		switch action.Kind {
		case NoOp:
			noops++
		case Delete:
			deletes++
		}
	}
	if noops != 1 || deletes != 1 {
		t.Fatalf("expected one NoOp and one Delete, got %+v", got)
	}
}

func TestReconcileIsIdempotent(t *testing.T) {
	opts := testOptions(t)
	event := testEvent(t)

	first := Reconcile(opts, []Event{event}, nil)
	if first[0].Kind != Create {
		t.Fatalf("expected Create, got %+v", first)
	}
	created := managedFrom(t, event)
	second := Reconcile(opts, []Event{event}, []ManagedEvent{created})
	if second[0].Kind != NoOp {
		t.Fatalf("expected NoOp on second pass, got %+v", second)
	}
}

func TestCustomHashIsUsedByDefaultMatching(t *testing.T) {
	opts := testOptions(t)
	opts.Hash = func(Event) string { return "custom-hash" }
	event := testEvent(t)
	existing := managedFrom(t, event)
	existing.ContentHash = "custom-hash"

	if got := Reconcile(opts, []Event{event}, []ManagedEvent{existing}); len(got) != 1 || got[0].Kind != NoOp {
		t.Fatalf("custom hash was not used: %+v", got)
	}
}

func TestDefaultPresentationMatching(t *testing.T) {
	a := Presentation{
		Transparency: TransparencyOpaque,
		ColorID:      "blue",
		Reminders:    []Reminder{{Method: "popup", Minutes: 5}},
	}
	b := a
	if !PresentationEqual(a, b) {
		t.Fatal("identical presentations should match")
	}
	b.Reminders = []Reminder{{Method: "popup", Minutes: 10}}
	if PresentationEqual(a, b) {
		t.Fatal("different reminders should not match")
	}
}

func TestOwnershipAndWindowHelpers(t *testing.T) {
	ownership := Ownership{Property: "managedBy", Value: "my-agent"}
	if got := ownership.Filter(); got != "managedBy=my-agent" {
		t.Fatalf("filter = %q", got)
	}
	if !ownership.Matches(map[string]string{"managedBy": "my-agent"}) {
		t.Fatal("ownership should match")
	}
	if ownership.Matches(map[string]string{"managedBy": "calendar-agent"}) {
		t.Fatal("ownership should not match another agent")
	}
	if (Ownership{}).Filter() != "" {
		t.Fatal("empty ownership should have an empty filter")
	}

	opts := testOptions(t)
	if !opts.Window.Contains(opts.Window.Start) {
		t.Fatal("window should contain its start")
	}
	if opts.Window.Contains(opts.Window.Finish) {
		t.Fatal("window is half-open and should not contain its finish")
	}
}

func TestDefaultMatchesAndEmptyHash(t *testing.T) {
	event := testEvent(t)
	observed := managedFrom(t, event)
	if !DefaultMatches(event, observed) {
		t.Fatal("identical event should match")
	}
	if Fingerprint("") != "" {
		t.Fatal("empty fingerprint should be empty")
	}
	observed.LinkHash = ""
	observed.Link = event.Link
	if !DefaultMatches(event, observed) {
		t.Fatal("plain link should match when no hash is stored")
	}
}

func TestReconcileDeletesFutureEventOutsideWindow(t *testing.T) {
	opts := testOptions(t)
	opts.Window = NewWindow(opts.Window.Start, 1)
	loc := mustLoc(t, "Europe/London")
	outside := ManagedEvent{
		ID:    "g1",
		Key:   "arrival:other",
		Owned: true,
		Start: at(loc, 2026, time.June, 10, 9, 0),
		End:   at(loc, 2026, time.June, 10, 10, 0),
	}
	got := Reconcile(opts, nil, []ManagedEvent{outside})
	if len(got) != 1 || got[0].Kind != Delete || got[0].Reason != "generated event is outside the reconciliation window" {
		t.Fatalf("expected outside-window delete, got %+v", got)
	}
}

func TestPreservePolicyWithoutObservedHashCannotDetectEdit(t *testing.T) {
	opts := testOptions(t)
	opts.ManualEditPolicy = PreserveManualEdits
	event := testEvent(t)
	observed := managedFrom(t, event)
	observed.Summary = "manually edited"

	got := Reconcile(opts, []Event{event}, []ManagedEvent{observed})
	if len(got) != 1 || got[0].Kind != Update {
		t.Fatalf("without an observed hash the event should be updated, got %+v", got)
	}
}

func TestKeyFallsBackToSourceKey(t *testing.T) {
	opts := testOptions(t)
	event := testEvent(t)
	event.Key = ""
	observed := managedFrom(t, event)
	observed.Key = ""

	got := Reconcile(opts, []Event{event}, []ManagedEvent{observed})
	if len(got) != 1 || got[0].Kind != NoOp {
		t.Fatalf("source key fallback failed: %+v", got)
	}
}
