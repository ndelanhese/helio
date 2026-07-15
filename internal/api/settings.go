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
	if err := a.dependencies.Store.PutSettings(r.Context(), settings, a.dependencies.AllowPublicLogger); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "settings could not be saved")
		return
	}
	principal, _ := auth.PrincipalFromRequest(r)
	if log, ok := a.dependencies.Store.(auditor); ok && principal != nil {
		// Record only the action and non-sensitive shape. Never persist logger serial.
		_ = log.RecordAudit(r.Context(), principal.UserID, "settings.update", map[string]any{"fields": settingsAuditFields()})
	}
	if a.dependencies.Reconfigure != nil {
		if err := a.dependencies.Reconfigure(r.Context(), settings); err != nil {
			writeError(w, http.StatusServiceUnavailable, "collector_unavailable", "settings were saved but collector could not be reconfigured")
			return
		}
	}
	writeJSON(w, http.StatusOK, settings)
}

func settingsAuditFields() []string {
	return []string{"loggerHost", "loggerPort", "modbusSlave", "panelCount", "panelWattage", "activeMPPT", "latitude", "longitude", "timezone", "currency", "tariffMinorPerKWh", "retentionDays"}
}
