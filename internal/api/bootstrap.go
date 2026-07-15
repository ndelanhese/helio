package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ndelanhese/helio/internal/auth"
	"github.com/ndelanhese/helio/internal/config"
)

func (a *API) bootstrapStatus(w http.ResponseWriter, r *http.Request) {
	if a.dependencies.Auth == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "authentication is unavailable")
		return
	}
	open, err := a.dependencies.Auth.BootstrapOpen(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "bootstrap state unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"open": open})
}

func (a *API) bootstrap(w http.ResponseWriter, r *http.Request) {
	if a.dependencies.Auth == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "authentication is unavailable")
		return
	}
	var body struct {
		Username string      `json:"username"`
		Password string      `json:"password"`
		Settings settingsDTO `json:"settings"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Username) == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_request", "username is required")
		return
	}
	settings, err := body.Settings.domain()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_settings", err.Error())
		return
	}
	settings, err = config.ValidateSettings(settings, a.dependencies.AllowPublicLogger)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_settings", err.Error())
		return
	}
	credentials, err := a.dependencies.Auth.BootstrapWithSettings(r.Context(), body.Username, body.Password, settings, a.dependencies.AllowPublicLogger)
	if errors.Is(err, auth.ErrBootstrapClosed) {
		writeError(w, http.StatusConflict, "bootstrap_closed", "initial setup is already complete")
		return
	}
	if errors.Is(err, auth.ErrPasswordLength) || errors.Is(err, auth.ErrPasswordEncoding) {
		writeError(w, http.StatusUnprocessableEntity, "invalid_password", err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "bootstrap failed")
		return
	}
	// Hardware is configured only after the database transaction has committed.
	if a.dependencies.Reconfigure != nil {
		if err := a.dependencies.Reconfigure(r.Context(), settings); err != nil {
			writeError(w, http.StatusServiceUnavailable, "collector_unavailable", "settings were saved but collector could not start")
			return
		}
	}
	http.SetCookie(w, a.dependencies.Auth.SessionCookie(credentials.Token))
	writeJSON(w, http.StatusCreated, credentials)
}
