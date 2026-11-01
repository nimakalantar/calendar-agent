package controlapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/nimakalantar/calendar-agent/src/health"
)

func (a *api) handleLive(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": a.opts.Build.Version,
	})
}

func (a *api) handleReady(w http.ResponseWriter, r *http.Request) {
	current := a.service.Status()
	if current.ShuttingDown {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "shutting_down"})
		return
	}
	if a.readiness != nil {
		report := a.readiness.Check(r.Context())
		payload := map[string]any{
			"status":    "ready",
			"checks":    report.Checks,
			"checkedAt": report.CheckedAt,
		}
		if report.Status == health.StatusUnavailable {
			payload["status"] = string(report.Status)
			writeJSON(w, http.StatusServiceUnavailable, payload)
			return
		}
		if report.Status == health.StatusDegraded {
			payload["status"] = string(report.Status)
		}
		writeJSON(w, http.StatusOK, payload)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

func (a *api) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.opts.Build.Info())
}

func (a *api) handleMetrics(w http.ResponseWriter, r *http.Request) {
	a.metrics.Handler().ServeHTTP(w, r)
}

func (a *api) handleHealth(w http.ResponseWriter, r *http.Request) {
	current := a.service.Status()
	status := "ok"
	switch {
	case current.ShuttingDown:
		status = "shutting_down"
	case current.Running:
		status = "working"
	case current.Queued:
		status = "queued"
	case current.LastJob != nil && current.LastJob.State == JobFailed:
		status = "degraded"
	}
	payload := map[string]any{
		"status":              status,
		"uptimeSeconds":       current.UptimeSeconds,
		"consecutiveFailures": current.ConsecutiveFailures,
		"version":             a.opts.Build.Version,
		"commit":              a.opts.Build.Commit,
		"buildDate":           a.opts.Build.BuildDate,
	}
	if current.LastJob != nil {
		last := map[string]any{
			"id":      current.LastJob.ID,
			"trigger": current.LastJob.Trigger,
			"state":   current.LastJob.State,
		}
		if current.LastJob.FinishedAt != nil {
			last["finishedAt"] = current.LastJob.FinishedAt
		}
		payload["lastJob"] = last
	}
	if current.LastSuccessAt != nil {
		payload["lastSuccessAt"] = current.LastSuccessAt
	}
	if a.readiness != nil {
		report := a.readiness.Check(r.Context())
		payload["ready"] = report.Status != health.StatusUnavailable
		payload["dependencies"] = report
	}
	writeJSON(w, http.StatusOK, payload)
}

var validIdempotencyKey = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func (a *api) handleSync(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key != "" && !validIdempotencyKey.MatchString(key) {
		writeError(w, http.StatusBadRequest, "Idempotency-Key must be 1-128 characters of A-Z, a-z, 0-9, dot, underscore, colon, or hyphen")
		return
	}
	id, reused, err := a.service.TriggerWithKey(r.Context(), "manual", key)
	if err != nil {
		switch {
		case errors.Is(err, ErrSyncRunning):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrServiceStopped), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			writeError(w, http.StatusServiceUnavailable, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	payload := map[string]any{"jobId": id}
	if reused {
		payload["deduplicated"] = true
	}
	writeJSON(w, http.StatusAccepted, payload)
}

func (a *api) handleJob(w http.ResponseWriter, r *http.Request) {
	job, ok := a.service.GetJob(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// handleJobs returns recent job records, newest first. It is bounded so a
// dashboard or script cannot ask for unbounded history.
func (a *api) handleJobs(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = min(parsed, 200)
		}
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			offset = parsed
		}
	}
	jobs := a.service.RecentJobs()
	total := len(jobs)
	if offset >= len(jobs) {
		jobs = nil
	} else {
		jobs = jobs[offset:]
		if len(jobs) > limit {
			jobs = jobs[:limit]
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs, "total": total})
}

// handleCancelJob requests cancellation of a queued or running job.
func (a *api) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	job, err := a.service.CancelJob(r.PathValue("id"))
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, job)
	case errors.Is(err, ErrJobNotFound):
		writeError(w, http.StatusNotFound, "job not found")
	case errors.Is(err, ErrJobFinished):
		writeError(w, http.StatusConflict, "job has already finished")
	default:
		writeError(w, http.StatusInternalServerError, "failed to cancel job")
	}
}
