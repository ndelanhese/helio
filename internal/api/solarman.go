package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ndelanhese/helio/internal/auth"
	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/solarmancloud"
)

type SolarmanStore interface {
	Put(context.Context, string, any) error
	Get(context.Context, string, any) (bool, error)
}

type solarmanCredentialsDTO struct {
	AppID     string `json:"appId"`
	AppSecret string `json:"appSecret"`
	Account   string `json:"account"`
	Password  string `json:"password"`
}

type solarmanSavedCredentials struct {
	AppID       string `json:"appId"`
	AppSecret   string `json:"appSecret"`
	Account     string `json:"account"`
	Password    string `json:"password"`
	StationID   int64  `json:"stationId"`
	StationName string `json:"stationName"`
}

const solarmanSecretName = "solarman-cloud-v1"

func (a *API) solarmanStatus(w http.ResponseWriter, r *http.Request) {
	if a.dependencies.SolarmanSecrets == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "configured": false, "reason": "Encrypted storage is unavailable."})
		return
	}
	var stored solarmanSavedCredentials
	found, err := a.dependencies.SolarmanSecrets.Get(r.Context(), solarmanSecretName, &stored)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "solarman_unavailable", "Solarman encrypted storage is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "configured": found, "account": stored.Account, "appIdSuffix": suffix(stored.AppID), "stationId": stored.StationID, "stationName": stored.StationName})
}

func (a *API) putSolarmanCredentials(w http.ResponseWriter, r *http.Request) {
	if a.dependencies.SolarmanSecrets == nil {
		writeError(w, http.StatusServiceUnavailable, "solarman_unavailable", "Encrypted storage is unavailable")
		return
	}
	var body solarmanCredentialsDTO
	if !decodeJSON(w, r, &body) {
		return
	}
	credentials := normalizeSolarmanCredentials(body)
	if err := validateSolarmanCredentials(credentials); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_solarman_credentials", err.Error())
		return
	}
	if err := a.dependencies.SolarmanSecrets.Put(r.Context(), solarmanSecretName, credentials); err != nil {
		writeError(w, http.StatusInternalServerError, "solarman_unavailable", "Could not encrypt Solarman credentials")
		return
	}
	principal, _ := auth.PrincipalFromRequest(r)
	if a.dependencies.SolarmanAudit != nil && principal != nil {
		_ = a.dependencies.SolarmanAudit.RecordAudit(r.Context(), principal.UserID, "solarman.configure", map[string]any{"account": credentials.Account, "appIdSuffix": suffix(credentials.AppID)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "configured": true, "account": credentials.Account, "appIdSuffix": suffix(credentials.AppID)})
}

func (a *API) testSolarman(w http.ResponseWriter, r *http.Request) {
	if a.dependencies.SolarmanSecrets == nil || a.dependencies.SolarmanClient == nil {
		writeError(w, http.StatusServiceUnavailable, "solarman_unavailable", "Encrypted storage is unavailable")
		return
	}
	var stored solarmanSavedCredentials
	found, err := a.dependencies.SolarmanSecrets.Get(r.Context(), solarmanSecretName, &stored)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "solarman_unavailable", "Solarman encrypted storage is unavailable")
		return
	}
	if !found {
		writeError(w, http.StatusConflict, "solarman_not_configured", "Save Solarman credentials before testing")
		return
	}
	stations, err := a.dependencies.SolarmanClient.Test(r.Context(), cloudCredentials(stored))
	if err != nil {
		writeError(w, http.StatusBadGateway, "solarman_connection_failed", "Solarman did not accept this connection. Check credentials and account access.")
		return
	}
	if len(stations) == 1 {
		stored.StationID, stored.StationName = stations[0].ID, stations[0].Name
		_ = a.dependencies.SolarmanSecrets.Put(r.Context(), solarmanSecretName, stored)
	}
	principal, _ := auth.PrincipalFromRequest(r)
	if a.dependencies.SolarmanAudit != nil && principal != nil {
		_ = a.dependencies.SolarmanAudit.RecordAudit(r.Context(), principal.UserID, "solarman.test", map[string]any{"stations": len(stations)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": true, "stations": stations, "testedAt": time.Now().UTC().Format(time.RFC3339)})
}

func (a *API) syncSolarman(w http.ResponseWriter, r *http.Request) {
	if a.dependencies.SolarmanSecrets == nil || a.dependencies.SolarmanClient == nil || a.dependencies.Telemetry == nil {
		writeError(w, http.StatusServiceUnavailable, "solarman_unavailable", "Solarman sync is unavailable")
		return
	}
	var body struct {
		Days int `json:"days"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Days == 0 {
		body.Days = 7
	}
	if body.Days < 1 || body.Days > 30 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_sync_range", "Sync range must be between 1 and 30 days")
		return
	}
	var stored solarmanSavedCredentials
	found, err := a.dependencies.SolarmanSecrets.Get(r.Context(), solarmanSecretName, &stored)
	if err != nil || !found || stored.StationID == 0 {
		writeError(w, http.StatusConflict, "solarman_station_required", "Test Solarman connection to select its station before syncing")
		return
	}
	location := time.Local
	from := time.Now().In(location).AddDate(0, 0, -body.Days+1)
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, location)
	to := time.Now().In(location)
	frames, err := a.dependencies.SolarmanClient.FetchFrames(r.Context(), cloudCredentials(stored), stored.StationID, from, to)
	if err != nil {
		writeError(w, http.StatusBadGateway, "solarman_sync_failed", "Solarman histórico recusou consulta: "+safeSolarmanError(err))
		return
	}
	for _, frame := range frames {
		if err := a.dependencies.Telemetry.SaveMinute(r.Context(), domain.TelemetrySnapshot{ObservedAt: frame.At, ACPowerW: frame.PowerW, Status: "cloud_history"}); err != nil {
			writeError(w, http.StatusInternalServerError, "solarman_sync_failed", "Could not save Solarman history")
			return
		}
	}
	if len(frames) > 0 {
		if err := a.dependencies.Telemetry.RebuildSummaries(r.Context(), from, to); err != nil {
			writeError(w, http.StatusInternalServerError, "solarman_sync_failed", "Could not rebuild history summaries after Solarman sync")
			return
		}
	}
	principal, _ := auth.PrincipalFromRequest(r)
	if a.dependencies.SolarmanAudit != nil && principal != nil {
		_ = a.dependencies.SolarmanAudit.RecordAudit(r.Context(), principal.UserID, "solarman.sync", map[string]any{"stationId": stored.StationID, "days": body.Days, "frames": len(frames)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"stationName": stored.StationName, "days": body.Days, "frames": len(frames)})
}

func safeSolarmanError(err error) string {
	message := err.Error()
	for _, marker := range []string{"appSecret", "password", "access_token", "refresh_token"} {
		if strings.Contains(strings.ToLower(message), marker) {
			return "falha de autenticação ou autorização"
		}
	}
	if len(message) > 180 {
		return message[:180]
	}
	return message
}

func normalizeSolarmanCredentials(body solarmanCredentialsDTO) solarmanSavedCredentials {
	return solarmanSavedCredentials{AppID: strings.TrimSpace(body.AppID), AppSecret: strings.TrimSpace(body.AppSecret), Account: strings.TrimSpace(body.Account), Password: body.Password}
}
func validateSolarmanCredentials(value solarmanSavedCredentials) error {
	if value.AppID == "" || value.AppSecret == "" || value.Account == "" || value.Password == "" {
		return errors.New("app ID, app secret, account, and password are required")
	}
	if len(value.AppID) > 160 || len(value.AppSecret) > 512 || len(value.Account) > 320 || len(value.Password) > 512 {
		return errors.New("Solarman credential value is too long")
	}
	return nil
}
func suffix(value string) string {
	if len(value) <= 4 {
		return "••••"
	}
	return "••••" + value[len(value)-4:]
}
func cloudCredentials(value solarmanSavedCredentials) solarmancloud.Credentials {
	return solarmancloud.Credentials{AppID: value.AppID, AppSecret: value.AppSecret, Account: value.Account, Password: value.Password}
}
