package controlapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nimakalantar/calendar-agent/src/controlapi"
	"github.com/nimakalantar/calendar-agent/src/health"
)

func TestReadyReflectsDependencyChecks(t *testing.T) {
	failing := true
	checker := health.New([]health.Probe{{
		Name:     "otp",
		Critical: true,
		Run: func(context.Context) error {
			if failing {
				return errors.New("down")
			}
			return nil
		},
	}}, health.WithCacheTTL(time.Millisecond), health.WithTimeout(time.Second))

	svc := controlapi.FakeService{}
	server := httptest.NewServer(controlapi.HandlerWithOptions(svc, controlapi.Options{Readiness: checker}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/ready")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/ready = %d", resp.StatusCode)
	}
	var report health.Report
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if report.Status != health.StatusUnavailable {
		t.Fatalf("report = %+v", report)
	}

	failing = false
	time.Sleep(5 * time.Millisecond)
	resp, err = http.Get(server.URL + "/ready")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/ready after recovery = %d", resp.StatusCode)
	}

	healthResp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer healthResp.Body.Close()
	var payload struct {
		Ready        bool          `json:"ready"`
		Dependencies health.Report `json:"dependencies"`
	}
	if err := json.NewDecoder(healthResp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Ready || payload.Dependencies.Status != health.StatusOK {
		t.Fatalf("health payload = %+v", payload)
	}
}

func TestPprofRequiresToken(t *testing.T) {
	svc := controlapi.FakeService{Token: "secret"}
	server := httptest.NewServer(controlapi.HandlerWithOptions(svc, controlapi.Options{EnablePprof: true}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/debug/pprof/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("pprof without auth = %d", resp.StatusCode)
	}
}

func TestReadyStaysUpWhenOptionalProbeDegrades(t *testing.T) {
	checker := health.New([]health.Probe{{
		Name:     "disk",
		Critical: false,
		Run:      func(context.Context) error { return errors.New("low disk") },
	}}, health.WithCacheTTL(time.Minute))
	svc := controlapi.FakeService{}
	server := httptest.NewServer(controlapi.HandlerWithOptions(svc, controlapi.Options{Readiness: checker}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/ready")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/ready = %d", resp.StatusCode)
	}
	var report health.Report
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		t.Fatal(err)
	}
	if report.Status != health.StatusDegraded {
		t.Fatalf("report = %+v", report)
	}
}
