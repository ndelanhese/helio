package api

import (
	"net/http"
	"time"

	"github.com/ndelanhese/helio/internal/collector"
)

type ComponentStatus struct {
	Database           string   `json:"database"`
	Logger             string   `json:"logger"`
	Collector          string   `json:"collector"`
	Jobs               string   `json:"jobs,omitempty"`
	Weather            string   `json:"weather"`
	Alerts             string   `json:"alerts,omitempty"`
	Analysis           string   `json:"analysis,omitempty"`
	LastSuccess        string   `json:"lastSuccess,omitempty"`
	LoggerUpdatedAt    string   `json:"loggerUpdatedAt,omitempty"`
	CollectorUpdatedAt string   `json:"collectorUpdatedAt,omitempty"`
	JobsUpdatedAt      string   `json:"jobsUpdatedAt,omitempty"`
	DatabaseUpdatedAt  string   `json:"databaseUpdatedAt,omitempty"`
	WeatherUpdatedAt   string   `json:"weatherUpdatedAt,omitempty"`
	WeatherFetchedAt   string   `json:"weatherFetchedAt,omitempty"`
	TemperatureC       *float64 `json:"temperatureC,omitempty"`
	PrecipitationMM    *float64 `json:"precipitationMM,omitempty"`
	WeatherCode        *int     `json:"weatherCode,omitempty"`
	CloudCoverPct      *float64 `json:"cloudCoverPct,omitempty"`
	WindSpeedKMH       *float64 `json:"windSpeedKMH,omitempty"`
	IrradianceWM2      *float64 `json:"irradianceWM2,omitempty"`
	AlertsUpdatedAt    string   `json:"alertsUpdatedAt,omitempty"`
	AnalysisUpdatedAt  string   `json:"analysisUpdatedAt,omitempty"`
	DatabaseError      string   `json:"databaseErrorClass,omitempty"`
	LoggerError        string   `json:"loggerErrorClass,omitempty"`
	WeatherError       string   `json:"weatherErrorClass,omitempty"`
	CollectorError     string   `json:"collectorErrorClass,omitempty"`
	JobsError          string   `json:"jobsErrorClass,omitempty"`
	AlertsError        string   `json:"alertsErrorClass,omitempty"`
	AnalysisError      string   `json:"analysisErrorClass,omitempty"`
}

func (a *API) componentHealth(w http.ResponseWriter, r *http.Request) {
	status := ComponentStatus{Database: "ok", Logger: "unknown", Collector: "idle", Weather: "unavailable"}
	if a.dependencies.Components != nil {
		status = a.dependencies.Components(r.Context())
	} else {
		state := a.dependencies.Latest()
		status = componentStatusFromState(status, state)
	}
	if status.Weather == "" {
		status.Weather = "unavailable"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if status.DatabaseUpdatedAt == "" {
		status.DatabaseUpdatedAt = now
	}
	if status.WeatherUpdatedAt == "" {
		status.WeatherUpdatedAt = now
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
		status.LoggerError = state.ErrorClass
		if status.LoggerError == "" {
			status.LoggerError = "communication"
		}
		if !state.LastErrorAt.IsZero() {
			status.LoggerUpdatedAt = state.LastErrorAt.UTC().Format(time.RFC3339)
		}
	}
	if !state.LastSuccess.IsZero() {
		status.LastSuccess = state.LastSuccess.UTC().Format("2006-01-02T15:04:05Z07:00")
		if status.LoggerUpdatedAt == "" {
			status.LoggerUpdatedAt = status.LastSuccess
		}
	}
	return status
}
