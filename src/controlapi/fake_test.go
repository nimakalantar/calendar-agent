package controlapi

import (
	"context"
	"time"
)

// FakeService is a minimal Service implementation for tests.
type FakeService struct {
	Token     string
	Jobs      []Job
	StartedAt time.Time
	State     Status
}

func (f FakeService) Trigger(context.Context, string) (string, error) { return "", nil }
func (f FakeService) TriggerWithKey(context.Context, string, string) (string, bool, error) {
	return "", false, nil
}
func (f FakeService) GetJob(string) (Job, bool)     { return Job{}, false }
func (f FakeService) CancelJob(string) (Job, error) { return Job{}, nil }
func (f FakeService) RecentJobs() []Job             { return f.Jobs }
func (f FakeService) Status() Status                { return f.State }
func (f FakeService) Started() time.Time            { return f.StartedAt }
func (f FakeService) APIToken() string              { return f.Token }
func (f FakeService) APITokens() []string {
	if f.Token == "" {
		return nil
	}
	return []string{f.Token}
}
