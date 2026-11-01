package gcalendar

import (
	"time"

	"github.com/nimakalantar/calendar-agent/src/calendaragent"
)

// Schema names the private extended properties used to persist agent metadata
// on a Google Calendar event. Applications with existing events keep their
// historical property names by setting these fields explicitly.
type Schema struct {
	ManagedBy   string
	Key         string
	SourceKey   string
	Kind        string
	Link        string
	LinkHash    string
	ContentHash string
}

// DefaultSchema is a sensible property set for a new agent.
func DefaultSchema() Schema {
	return Schema{
		ManagedBy:   "managedBy",
		Key:         "planKey",
		SourceKey:   "sourceEventID",
		Kind:        "kind",
		Link:        "link",
		LinkHash:    "linkHash",
		ContentHash: "contentHash",
	}
}

func (s Schema) withDefaults() Schema {
	def := DefaultSchema()
	if s.ManagedBy == "" {
		s.ManagedBy = def.ManagedBy
	}
	if s.Key == "" {
		s.Key = def.Key
	}
	if s.SourceKey == "" {
		s.SourceKey = def.SourceKey
	}
	if s.Kind == "" {
		s.Kind = def.Kind
	}
	if s.Link == "" {
		s.Link = def.Link
	}
	if s.LinkHash == "" {
		s.LinkHash = def.LinkHash
	}
	if s.ContentHash == "" {
		s.ContentHash = def.ContentHash
	}
	return s
}

// DefaultOwnership marks events written by this package when the application
// does not configure its own marker.
func DefaultOwnership() calendaragent.Ownership {
	return calendaragent.Ownership{Property: "managedBy", Value: "calendar-agent"}
}

// Config describes the destination calendar and how events are mapped to it.
type Config struct {
	CalendarID string
	Timezone   string
	Location   *time.Location

	Ownership calendaragent.Ownership
	Schema    Schema

	// DefaultPresentation is used when a desired Event carries no presentation
	// settings. Calendar adapters that manage colours/reminders centrally can
	// set it once instead of on every event.
	DefaultPresentation calendaragent.Presentation

	// EventID overrides the deterministic event ID. The default is a fingerprint
	// of "<ownership value>:<event key>", which makes creates idempotent.
	EventID func(calendaragent.Event) string
	// ContentHash overrides the hash written to the event. The default uses
	// Event.ContentHash, then DefaultHash.
	ContentHash func(calendaragent.Event) string
}

func (c Config) normalized() Config {
	if c.Ownership.Property == "" {
		c.Ownership.Property = DefaultOwnership().Property
	}
	if c.Ownership.Value == "" {
		c.Ownership.Value = DefaultOwnership().Value
	}
	c.Schema = c.Schema.withDefaults()
	if c.Location == nil {
		if c.Timezone != "" {
			if loc, err := time.LoadLocation(c.Timezone); err == nil {
				c.Location = loc
			}
		}
		if c.Location == nil {
			c.Location = time.UTC
		}
	}
	if c.Timezone == "" {
		c.Timezone = c.Location.String()
	}
	return c
}

// Presentation returns the desired presentation, falling back to the config
// default when the event does not specify one.
func (c Config) Presentation(event calendaragent.Event) calendaragent.Presentation {
	if isZeroPresentation(event.Presentation) {
		return c.DefaultPresentation
	}
	return event.Presentation
}

// Hash returns the content hash to write for an event.
func (c Config) Hash(event calendaragent.Event) string {
	if event.ContentHash != "" {
		return event.ContentHash
	}
	if c.ContentHash != nil {
		return c.ContentHash(event)
	}
	return calendaragent.DefaultHash(event)
}

// ID returns the deterministic Google event ID for a desired event.
func (c Config) ID(event calendaragent.Event) string {
	if c.EventID != nil {
		return c.EventID(event)
	}
	identity := event.Key
	if identity == "" {
		identity = event.SourceKey
	}
	return calendaragent.Fingerprint(c.Ownership.Value + ":" + identity)
}

func isZeroPresentation(p calendaragent.Presentation) bool {
	return p.Transparency == "" &&
		p.ColorID == "" &&
		p.StartTimeZone == "" &&
		p.EndTimeZone == "" &&
		!p.UseDefaultReminders &&
		len(p.Reminders) == 0
}
