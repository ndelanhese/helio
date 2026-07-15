package api

import (
	"fmt"
	"io"
	"net/http"

	"github.com/ndelanhese/helio/internal/auth"
)

func (a *API) backup(w http.ResponseWriter, r *http.Request) {
	if a.dependencies.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "backup is unavailable")
		return
	}
	filename := "helio-backup-" + a.dependencies.Now().UTC().Format("20060102-150405") + ".db"
	staged, err := a.dependencies.Store.PrepareBackup(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "backup could not be prepared")
		return
	}
	defer staged.Close()
	principal, ok := auth.PrincipalFromRequest(r)
	log, auditOK := a.dependencies.Store.(auditor)
	if !ok || !auditOK || log.RecordAudit(r.Context(), principal.UserID, "data.backup", map[string]any{"filename": filename}) != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "backup audit could not be recorded")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	_, _ = io.Copy(w, staged)
}
