package controlapi

import (
	"bytes"
	_ "embed"
	"html/template"
	"net/http"
	"time"
)

type jobCountsView struct {
	Created   int
	Updated   int
	Deleted   int
	Unchanged int
}

type jobView struct {
	ID          string
	Trigger     string
	State       string
	DisplayTime time.Time
	Counts      jobCountsView
}

// dashboardData is the view model for the operator dashboard.
type dashboardData struct {
	Title   string
	Started string
	Jobs    []jobView
}

//go:embed dashboard.html
var dashboardHTML string

var dashboardTemplate = template.Must(template.New("dashboard").Parse(dashboardHTML))

func (a *api) handleDashboard(w http.ResponseWriter, _ *http.Request) {
	jobs := a.service.RecentJobs()
	views := make([]jobView, 0, len(jobs))
	for _, job := range jobs {
		views = append(views, jobView{
			ID:          job.ID,
			Trigger:     job.Trigger,
			State:       string(job.State),
			DisplayTime: job.DisplayTime(),
			Counts: jobCountsView{
				Created:   job.Counts.Created,
				Updated:   job.Counts.Updated,
				Deleted:   job.Counts.Deleted,
				Unchanged: job.Counts.Unchanged,
			},
		})
	}
	var body bytes.Buffer
	data := dashboardData{Title: a.opts.Title, Started: a.service.Started().Format("2006-01-02T15:04:05Z07:00"), Jobs: views}
	if err := dashboardTemplate.Execute(&body, data); err != nil {
		http.Error(w, "failed to render dashboard", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body.Bytes())
}
