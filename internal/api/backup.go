package api

import (
	"fmt"
	"net/http"

	"github.com/ndelanhese/helio/internal/auth"
)

func (a *API) backup(w http.ResponseWriter, r *http.Request) {
	if a.dependencies.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "backup is unavailable")
		return
	}
	filename := "helio-backup-" + a.dependencies.Now().UTC().Format("20060102-150405") + ".db"
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	if principal, ok := auth.PrincipalFromRequest(r); ok {
		if log, ok := a.dependencies.Store.(auditor); ok {
			_ = log.RecordAudit(r.Context(), principal.UserID, "data.backup", map[string]any{"filename": filename})
		}
	}
	if err := a.dependencies.Store.Backup(r.Context(), w); err != nil {
		// Headers may already be committed; terminate the stream without exposing details.
		return
	}
}
