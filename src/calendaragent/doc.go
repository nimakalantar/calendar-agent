// Package calendaragent is the provider-independent part of a calendar
// reconciliation agent.
//
// It owns the desired-state diff (Create / Update / Delete / NoOp), event
// identity, ownership, content hashing, and the reconciliation window. It does
// not know about domain-specific planning or any calendar provider.
//
// An application supplies:
//
//   - a pure planner that produces []Event;
//   - a Backend that lists, creates, updates, and deletes owned events;
//   - optional hooks for rendering, hashing, and field comparison.
//
// The package depends only on the standard library so it can be moved into its
// own Go module without code changes.
package calendaragent
