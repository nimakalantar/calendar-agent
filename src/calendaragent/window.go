package calendaragent

import "time"

// Window is a half-open reconciliation horizon [Start, Finish).
type Window struct {
	Start  time.Time
	Finish time.Time
}

// NewWindow builds a window starting at start lasting durationDays days.
func NewWindow(start time.Time, durationDays int) Window {
	return Window{Start: start, Finish: start.AddDate(0, 0, durationDays)}
}

// Contains reports whether value is inside the window.
func (w Window) Contains(value time.Time) bool {
	return !value.Before(w.Start) && value.Before(w.Finish)
}

// Overlaps reports whether [start, end) intersects the window.
func (w Window) Overlaps(start, end time.Time) bool {
	return end.After(w.Start) && start.Before(w.Finish)
}
