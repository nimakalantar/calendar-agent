package anchor

import (
	"testing"
	"time"
)

func at(hour, minute int) time.Time {
	return time.Date(2026, time.December, 21, hour, minute, 0, 0, time.UTC)
}

func TestBuildAnchorAndReturn(t *testing.T) {
	now := at(12, 0)
	result := Build([]Event{
		{Summary: "Flight #anchor", Location: "40.7,-74.0", End: at(10, 0)},
		{Summary: "Home #return", End: at(18, 0)},
	}, "London", 0, State{}, now)
	if got := Resolve(result.Timeline, at(11, 0)); got != "40.7,-74.0" {
		t.Fatalf("anchor = %q", got)
	}
	if got := Resolve(result.Timeline, at(19, 0)); got != "London" {
		t.Fatalf("return = %q", got)
	}
	if got := Resolve(result.Timeline, at(9, 0)); got != "London" {
		t.Fatalf("before = %q", got)
	}
}

func TestBuildCooldown(t *testing.T) {
	result := Build([]Event{
		{Summary: "#anchor", Location: "Paris", End: at(10, 0)},
	}, "London", 2*time.Hour, State{}, at(12, 0))
	if got := Resolve(result.Timeline, at(11, 0)); got != "Paris" {
		t.Fatalf("cooldown active = %q", got)
	}
	if got := Resolve(result.Timeline, at(12, 0)); got != "London" {
		t.Fatalf("cooldown expired = %q", got)
	}
}

func TestPersistedState(t *testing.T) {
	expiry := at(18, 0)
	result := Build(nil, "London", 0, State{Location: "Paris", ExpiresAtEpoch: expiry.Unix()}, at(12, 0))
	if got := Resolve(result.Timeline, at(12, 0)); got != "Paris" {
		t.Fatalf("persisted = %q", got)
	}
	if got := Resolve(result.Timeline, at(19, 0)); got != "London" {
		t.Fatalf("expired = %q", got)
	}
	if result.State.Location != "Paris" || result.State.ExpiresAtEpoch != expiry.Unix() {
		t.Fatalf("state = %+v", result.State)
	}
}

func TestExpiredStateIsIgnored(t *testing.T) {
	result := Build(nil, "London", 0, State{Location: "Paris", ExpiresAtEpoch: at(9, 0).Unix()}, at(12, 0))
	if got := Resolve(result.Timeline, at(12, 0)); got != "London" {
		t.Fatalf("expired state = %q", got)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected a warning")
	}
}

func TestAnchorWithoutLocationWarns(t *testing.T) {
	result := Build([]Event{{Summary: "#anchor", End: at(10, 0)}}, "London", 0, State{}, at(12, 0))
	if len(result.Warnings) == 0 {
		t.Fatal("expected a warning")
	}
	if got := Resolve(result.Timeline, at(12, 0)); got != "London" {
		t.Fatalf("anchor = %q", got)
	}
}

func TestExplicitTagFlags(t *testing.T) {
	result := Build([]Event{
		{Summary: "Flight", Location: "Paris", End: at(10, 0), IsAnchor: true},
		{Summary: "Back", End: at(18, 0), IsReturn: true},
	}, "London", 0, State{}, at(12, 0))
	if got := Resolve(result.Timeline, at(11, 0)); got != "Paris" {
		t.Fatalf("anchor = %q", got)
	}
	if got := Resolve(result.Timeline, at(19, 0)); got != "London" {
		t.Fatalf("return = %q", got)
	}
}
