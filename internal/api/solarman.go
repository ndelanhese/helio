package api

import (
	"context"
	"errors"
	"fmt"
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

const (
	automaticRecoveryDays     = 2
	automaticRecoveryCooldown = 90 * time.Minute
	maximumCloudHistoryLag    = 20 * time.Minute
)

var automaticRecoveryDelays = []time.Duration{
	2 * time.Minute,
	15 * time.Minute, 15 * time.Minute, 15 * time.Minute, 15 * time.Minute,
	15 * time.Minute, 15 * time.Minute, 15 * time.Minute, 15 * time.Minute,
	30 * time.Minute,
	12 * time.Hour,
}

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
	stored, frames, err := a.recoverSolarman(r.Context(), body.Days)
	if errors.Is(err, errSolarmanStationRequired) {
		writeError(w, http.StatusConflict, "solarman_station_required", "Test Solarman connection to select its station before syncing")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "solarman_sync_failed", "Solarman histórico recusou consulta: "+safeSolarmanError(err))
		return
	}
	principal, _ := auth.PrincipalFromRequest(r)
	if a.dependencies.SolarmanAudit != nil && principal != nil {
		_ = a.dependencies.SolarmanAudit.RecordAudit(r.Context(), principal.UserID, "solarman.sync", map[string]any{"stationId": stored.StationID, "days": body.Days, "frames": len(frames)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"stationName": stored.StationName, "days": body.Days, "frames": len(frames)})
}

var errSolarmanStationRequired = errors.New("Solarman station is required")

func (a *API) recoverSolarman(ctx context.Context, days int) (solarmanSavedCredentials, []solarmancloud.Frame, error) {
	var stored solarmanSavedCredentials
	found, err := a.dependencies.SolarmanSecrets.Get(ctx, solarmanSecretName, &stored)
	if err != nil {
		return stored, nil, err
	}
	if !found || stored.StationID == 0 {
		return stored, nil, errSolarmanStationRequired
	}
	location := time.Local
	if a.dependencies.BillingLocation != nil {
		if configured, locationErr := a.dependencies.BillingLocation(ctx); locationErr == nil && configured != nil {
			location = configured
		}
	}
	now := a.dependencies.Now().In(location)
	from := now.AddDate(0, 0, -days+1)
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, location)
	frames, err := a.dependencies.SolarmanClient.FetchFrames(ctx, cloudCredentials(stored), stored.StationID, from, now)
	if err != nil {
		return stored, nil, err
	}
	for _, frame := range frames {
		if err := a.dependencies.Telemetry.SaveMinute(ctx, domain.TelemetrySnapshot{ObservedAt: frame.At, ACPowerW: frame.PowerW, Status: "cloud_history"}); err != nil {
			return stored, nil, fmt.Errorf("save Solarman history: %w", err)
		}
	}
	if len(frames) > 0 {
		if err := a.dependencies.Telemetry.RebuildSummaries(ctx, from, now); err != nil {
			return stored, nil, fmt.Errorf("rebuild history summaries: %w", err)
		}
	}
	return stored, frames, nil
}

func (a *API) startSolarmanRecovery() {
	if a.dependencies.Hub == nil || a.dependencies.SolarmanSecrets == nil || a.dependencies.SolarmanClient == nil || a.dependencies.Telemetry == nil {
		return
	}
	events, unsubscribe := a.dependencies.Hub.Subscribe()
	go func() {
		defer unsubscribe()
		offline := true
		for {
			select {
			case <-a.dependencies.ShutdownContext.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				if event.State.Stale {
					offline = true
					continue
				}
				if event.Kind == "snapshot" && offline {
					offline = false
					a.scheduleSolarmanRecovery()
				}
			}
		}
	}()
}

func (a *API) scheduleSolarmanRecovery() {
	now := a.dependencies.Now()
	a.recoveryMu.Lock()
	if a.recovering || now.Sub(a.lastRecovery) < automaticRecoveryCooldown {
		a.recoveryMu.Unlock()
		return
	}
	a.recovering, a.lastRecovery = true, now
	a.recoveryMu.Unlock()
	go func() {
		defer func() {
			a.recoveryMu.Lock()
			a.recovering = false
			a.recoveryMu.Unlock()
		}()
		for _, delay := range automaticRecoveryDelays {
			timer := time.NewTimer(delay)
			select {
			case <-a.dependencies.ShutdownContext.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			_, frames, err := a.recoverSolarman(a.dependencies.ShutdownContext, automaticRecoveryDays)
			if err == nil && !cloudHistoryLagging(frames, a.dependencies.Now()) {
				return
			}
		}
	}()
}

func cloudHistoryLagging(frames []solarmancloud.Frame, now time.Time) bool {
	var latest time.Time
	for _, frame := range frames {
		if frame.At.After(latest) {
			latest = frame.At
		}
	}
	return latest.IsZero() || now.UTC().Sub(latest.UTC()) > maximumCloudHistoryLag
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
