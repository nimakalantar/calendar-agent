// Package controlapi provides a reusable HTTP control plane for calendar
// agents: job endpoints, readiness, metrics, an operator dashboard, and the
// auth/limiting/security middleware around them.
package controlapi

import (
	"context"
	"errors"
	"runtime"
	"time"
)

// JobState is the lifecycle state of an agent job.
type JobState string

const (
	JobQueued    JobState = "queued"
	JobRunning   JobState = "running"
	JobFinished  JobState = "finished"
	JobFailed    JobState = "failed"
	JobCancelled JobState = "cancelled"
)

// Counts summarises the calendar actions of a job.
type Counts struct {
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Deleted   int `json:"deleted"`
	Unchanged int `json:"unchanged"`
}

// Job is a record of one agent run.
type Job struct {
	ID         string     `json:"id"`
	Trigger    string     `json:"trigger"`
	State      JobState   `json:"state"`
	QueuedAt   time.Time  `json:"queuedAt"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Counts     Counts     `json:"counts"`
	Warnings   []string   `json:"warnings,omitempty"`
	Error      string     `json:"error,omitempty"`
	DryRun     bool       `json:"dryRun"`

	CancelRequested bool   `json:"cancelRequested,omitempty"`
	IdempotencyKey  string `json:"idempotencyKey,omitempty"`
}

// DisplayTime is the best timestamp to show for a job.
func (j Job) DisplayTime() time.Time {
	if !j.StartedAt.IsZero() {
		return j.StartedAt
	}
	return j.QueuedAt
}

// Status summarises the current service and job state.
type Status struct {
	Running             bool
	Queued              bool
	ShuttingDown        bool
	UptimeSeconds       int64
	ConsecutiveFailures int
	LastJob             *Job
	LastSuccessAt       *time.Time
}

// BuildInfo is injected build metadata for the API responses.
type BuildInfo struct {
	Version   string
	Commit    string
	BuildDate string
}

// Info returns the build metadata map used by /version and /health.
func (b BuildInfo) Info() map[string]string {
	return map[string]string{
		"version":   b.Version,
		"commit":    b.Commit,
		"buildDate": b.BuildDate,
		"goVersion": runtime.Version(),
		"platform":  runtime.GOOS + "/" + runtime.GOARCH,
	}
}

// Service is the control surface exposed over HTTP.
type Service interface {
	Trigger(ctx context.Context, trigger string) (string, error)
	TriggerWithKey(ctx context.Context, trigger, key string) (string, bool, error)
	GetJob(id string) (Job, bool)
	CancelJob(id string) (Job, error)
	RecentJobs() []Job
	Status() Status
	Started() time.Time
	APIToken() string
	APITokens() []string
}

// Errors returned by the Service.
var (
	ErrSyncRunning    = errors.New("a sync is already running")
	ErrServiceStopped = errors.New("service is shutting down")
	ErrJobNotFound    = errors.New("job not found")
	ErrJobFinished    = errors.New("job has already finished")
	ErrJobCancelled   = errors.New("sync job cancelled")
)
