package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ndelanhese/helio/internal/auth"
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
	AppID     string `json:"appId"`
	AppSecret string `json:"appSecret"`
	Account   string `json:"account"`
	Password  string `json:"password"`
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
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "configured": found, "account": stored.Account, "appIdSuffix": suffix(stored.AppID), "lastTestedAt": ""})
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
	stations, err := a.dependencies.SolarmanClient.Test(r.Context(), solarmancloud.Credentials(stored))
	if err != nil {
		writeError(w, http.StatusBadGateway, "solarman_connection_failed", "Solarman did not accept this connection. Check credentials and account access.")
		return
	}
	principal, _ := auth.PrincipalFromRequest(r)
	if a.dependencies.SolarmanAudit != nil && principal != nil {
		_ = a.dependencies.SolarmanAudit.RecordAudit(r.Context(), principal.UserID, "solarman.test", map[string]any{"stations": len(stations)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": true, "stations": stations, "testedAt": time.Now().UTC().Format(time.RFC3339)})
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
