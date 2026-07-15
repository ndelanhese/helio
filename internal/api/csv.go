package api

import (
	"context"
	"encoding/csv"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/ndelanhese/helio/internal/auth"
	"github.com/ndelanhese/helio/internal/domain"
)

type csvHistoryStore interface {
	HistorySnapshots(context.Context, time.Time, time.Time) ([]domain.TelemetrySnapshot, error)
}

func (a *API) csv(w http.ResponseWriter, r *http.Request) {
	window, _, err := parseRange(r, false)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_range", err.Error())
		return
	}
	var snapshots []domain.TelemetrySnapshot
	if source, ok := a.dependencies.History.(csvHistoryStore); ok {
		snapshots, err = source.HistorySnapshots(r.Context(), window.from, window.to)
	} else if a.dependencies.History != nil {
		points, historyErr := a.dependencies.History.History(r.Context(), window.from, window.to)
		err = historyErr
		for _, point := range points {
			snapshots = append(snapshots, domain.TelemetrySnapshot{ObservedAt: point.At, ACPowerW: point.PowerW})
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "history could not be exported")
		return
	}
	staged, err := os.CreateTemp("", ".helio-history-*.csv")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "history export could not be prepared")
		return
	}
	stagedPath := staged.Name()
	defer func() { _ = staged.Close(); _ = os.Remove(stagedPath) }()
	writer := csv.NewWriter(staged)
	_ = writer.Write([]string{"at", "power_w", "energy_today_wh", "status"})
	for _, snapshot := range snapshots {
		_ = writer.Write([]string{snapshot.ObservedAt.UTC().Format(time.RFC3339), strconv.FormatFloat(snapshot.ACPowerW, 'f', -1, 64), strconv.FormatFloat(snapshot.EnergyTodayWh, 'f', -1, 64), snapshot.Status})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "history export could not be prepared")
		return
	}
	if _, err := staged.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "history export could not be prepared")
		return
	}
	principal, principalOK := auth.PrincipalFromRequest(r)
	log, auditOK := a.dependencies.Store.(auditor)
	if !principalOK || !auditOK || log.RecordAudit(r.Context(), principal.UserID, "history.export_csv", map[string]any{"from": window.from, "to": window.to}) != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "history export audit could not be recorded")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="helio-history.csv"`)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, staged)
}
