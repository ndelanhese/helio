package api

import (
	"context"
	"net/http"

	"github.com/ndelanhese/helio/internal/auth"
	"github.com/ndelanhese/helio/internal/config"
)

type auditor interface {
	RecordAudit(context.Context, string, string, any) error
}

func (a *API) getSettings(w http.ResponseWriter, r *http.Request) {
	if a.dependencies.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "settings are unavailable")
		return
	}
	settings, err := a.dependencies.Store.GetSettings(r.Context(), a.dependencies.AllowPublicLogger)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "settings could not be loaded")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (a *API) putSettings(w http.ResponseWriter, r *http.Request) {
	if a.dependencies.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "settings are unavailable")
		return
	}
	var body settingsDTO
	if !decodeJSON(w, r, &body) {
		return
	}
	settings, err := body.domain()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_settings", err.Error())
		return
	}
	settings, err = config.ValidateSettings(settings, a.dependencies.AllowPublicLogger)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_settings", err.Error())
		return
	}
	principal, _ := auth.PrincipalFromRequest(r)
	if principal == nil || a.dependencies.ApplySettings == nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "settings update is unavailable")
		return
	}
	if err := a.dependencies.ApplySettings(r.Context(), settings, principal.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "settings could not be saved")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}
