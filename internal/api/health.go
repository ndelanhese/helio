package api

import (
	"net/http"

	"github.com/ndelanhese/helio/internal/collector"
)

type ComponentStatus struct {
	Database           string `json:"database"`
	Logger             string `json:"logger"`
	Collector          string `json:"collector"`
	Jobs               string `json:"jobs,omitempty"`
	Weather            string `json:"weather"`
	LastSuccess        string `json:"lastSuccess,omitempty"`
	LoggerUpdatedAt    string `json:"loggerUpdatedAt,omitempty"`
	CollectorUpdatedAt string `json:"collectorUpdatedAt,omitempty"`
	JobsUpdatedAt      string `json:"jobsUpdatedAt,omitempty"`
	CollectorError     string `json:"collectorErrorClass,omitempty"`
	JobsError          string `json:"jobsErrorClass,omitempty"`
}

func (a *API) componentHealth(w http.ResponseWriter, r *http.Request) {
	status := ComponentStatus{Database: "ok", Logger: "unknown", Collector: "idle", Weather: "unconfigured"}
	if a.dependencies.Components != nil {
		status = a.dependencies.Components(r.Context())
	} else {
		state := a.dependencies.Latest()
		status = componentStatusFromState(status, state)
	}
	if status.Weather == "" {
		status.Weather = "unconfigured"
	}
	writeJSON(w, http.StatusOK, status)
}

func componentStatusFromState(status ComponentStatus, state collector.State) ComponentStatus {
	if state.Snapshot != nil {
		status.Collector = "running"
		status.Logger = "online"
	}
	if state.Stale || state.LastError != "" {
		status.Logger = "offline"
	}
	if !state.LastSuccess.IsZero() {
		status.LastSuccess = state.LastSuccess.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return status
}
