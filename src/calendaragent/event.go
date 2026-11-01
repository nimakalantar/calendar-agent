package calendaragent

import "time"

// Transparency controls whether a managed event appears free or busy.
type Transparency string

const (
	TransparencyOpaque      Transparency = "opaque"
	TransparencyBusy        Transparency = TransparencyOpaque
	TransparencyTransparent Transparency = "transparent"
	TransparencyFree        Transparency = TransparencyTransparent
)

// Reminder is a reminder override managed on a generated event.
type Reminder struct {
	Method  string
	Minutes int
}

// Presentation contains provider-independent calendar settings that the agent
// manages on a generated event. Calendar adapters translate it to their own
// payload.
type Presentation struct {
	Transparency        Transparency
	ColorID             string
	StartTimeZone       string
	EndTimeZone         string
	UseDefaultReminders bool
	Reminders           []Reminder
}

// Event is a desired calendar event produced by an application planner.
//
// Key is the stable reconciliation identity (for example "event:2026-01-02:1").
// SourceKey links the event to the source object that caused it, when one
// exists. Kind and Link are opaque application metadata that participate in
// change detection.
type Event struct {
	Key          string
	SourceKey    string
	Start        time.Time
	End          time.Time
	Summary      string
	Description  string
	Location     string
	Kind         string
	Link         string
	Presentation Presentation
	ContentHash  string

	// Data carries application-specific values for the hooks in Options. The
	// library never inspects it.
	Data any
}

// ManagedEvent is an event already persisted in the destination calendar.
// Provider-specific fields stay inside the adapter.
type ManagedEvent struct {
	ID           string
	Key          string
	SourceKey    string
	Summary      string
	Description  string
	Kind         string
	Start        time.Time
	End          time.Time
	Location     string
	AllDay       bool
	Status       string
	SelfResponse string
	Link         string
	LinkHash     string
	Presentation Presentation
	Owned        bool
	ContentHash  string

	// Data carries application-specific values for the hooks in Options.
	Data any
}

// ActionKind enumerates the mutations a sync run can perform.
type ActionKind string

const (
	Create ActionKind = "Create"
	Update ActionKind = "Update"
	Delete ActionKind = "Delete"
	NoOp   ActionKind = "NoOp"
)

// Action is a single mutation to apply to the destination calendar. Event is
// zero for Delete actions; ExistingEventID is empty for Create actions.
type Action struct {
	Kind            ActionKind
	Event           Event
	ExistingEventID string
	Reason          string
}
