package health

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckAggregatesStatuses(t *testing.T) {
	checker := New([]Probe{
		{Name: "otp", Critical: true, Run: func(context.Context) error { return nil }},
		{Name: "disk", Critical: true, Run: func(context.Context) error { return errors.New("full") }},
	})
	report := checker.Check(context.Background())
	if report.Status != StatusUnavailable {
		t.Fatalf("status = %s", report.Status)
	}
	if len(report.Checks) != 2 || report.Checks[1].Status != StatusUnavailable {
		t.Fatalf("checks = %+v", report.Checks)
	}
}

func TestOptionalProbeDegrades(t *testing.T) {
	checker := New([]Probe{
		{Name: "otp", Critical: true, Run: func(context.Context) error { return nil }},
		{Name: "geocode", Critical: false, Run: func(context.Context) error { return errors.New("down") }},
	})
	report := checker.Check(context.Background())
	if report.Status != StatusDegraded {
		t.Fatalf("status = %s", report.Status)
	}
}

func TestReportIsCached(t *testing.T) {
	var calls atomic.Int32
	now := time.Unix(1000, 0)
	checker := New(
		[]Probe{{Name: "p", Critical: true, Run: func(context.Context) error { calls.Add(1); return nil }}},
		WithCacheTTL(time.Minute),
		WithNow(func() time.Time { return now }),
	)
	checker.Check(context.Background())
	checker.Check(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("probe calls = %d", calls.Load())
	}
	now = now.Add(2 * time.Minute)
	checker.Check(context.Background())
	if calls.Load() != 2 {
		t.Fatalf("probe calls after ttl = %d", calls.Load())
	}
}

func TestConcurrentChecksShareOneRun(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	checker := New([]Probe{{Name: "p", Critical: true, Run: func(context.Context) error {
		calls.Add(1)
		<-release
		return nil
	}}}, WithCacheTTL(time.Minute))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			checker.Check(context.Background())
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("probe calls = %d", calls.Load())
	}
}

func TestObserverSeesReport(t *testing.T) {
	var got Report
	checker := New(
		[]Probe{{Name: "p", Critical: true, Run: func(context.Context) error { return nil }}},
		WithObserver(func(report Report) { got = report }),
	)
	checker.Check(context.Background())
	if got.Status != StatusOK || len(got.Checks) != 1 {
		t.Fatalf("observer report = %+v", got)
	}
}
