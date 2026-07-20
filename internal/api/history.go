package api

import (
	"context"
	"net/http"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

type timeRange struct{ from, to time.Time }

type summaryHistoryStore interface {
	HourlyHistory(context.Context, time.Time, time.Time) ([]domain.AggregatePoint, error)
	DailyHistory(context.Context, time.Time, time.Time) ([]domain.AggregatePoint, error)
	MonthlyHistory(context.Context, time.Time, time.Time) ([]domain.AggregatePoint, error)
}

func parseRange(r *http.Request, requireResolution bool) (timeRange, string, error) {
	from, err := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	if err != nil {
		return timeRange{}, "", err
	}
	to, err := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	if err != nil {
		return timeRange{}, "", err
	}
	from, to = from.UTC(), to.UTC()
	if !from.Before(to) {
		return timeRange{}, "", errInvalidRange
	}
	resolution := r.URL.Query().Get("resolution")
	if requireResolution {
		switch resolution {
		case "minute", "hour", "day", "month":
		default:
			return timeRange{}, "", errInvalidResolution
		}
		if resolution == "minute" && to.Sub(from) > 366*24*time.Hour {
			return timeRange{}, "", errRangeTooLarge
		}
	}
	return timeRange{from: from, to: to}, resolution, nil
}

type rangeError string

func (e rangeError) Error() string { return string(e) }

const (
	errInvalidRange      rangeError = "from must be before to and both must be RFC3339 timestamps"
	errInvalidResolution rangeError = "resolution must be minute, hour, day, or month"
	errRangeTooLarge     rangeError = "minute history cannot exceed 366 days"
)

func (a *API) history(w http.ResponseWriter, r *http.Request) {
	if a.dependencies.History == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "history is unavailable")
		return
	}
	window, resolution, err := parseRange(r, true)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_range", err.Error())
		return
	}
	if err := a.refreshCurrentHistory(r.Context(), window, resolution); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "current history could not be refreshed")
		return
	}
	if resolution != "minute" {
		summaries, ok := a.dependencies.History.(summaryHistoryStore)
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "summary history is unavailable")
			return
		}
		var points []domain.AggregatePoint
		switch resolution {
		case "hour":
			points, err = summaries.HourlyHistory(r.Context(), window.from, window.to)
		case "day":
			points, err = summaries.DailyHistory(r.Context(), window.from, window.to)
		case "month":
			points, err = summaries.MonthlyHistory(r.Context(), window.from, window.to)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "history could not be loaded")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"from": window.from, "to": window.to, "resolution": resolution, "points": points})
		return
	}
	points, err := a.dependencies.History.History(r.Context(), window.from, window.to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "history could not be loaded")
		return
	}
	for index := range points {
		points[index].At = points[index].At.UTC()
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": window.from, "to": window.to, "resolution": resolution, "points": points})
}

// refreshCurrentHistory keeps the weekly view useful before the nightly job
// closes today's summaries. Older periods remain read-only.
func (a *API) refreshCurrentHistory(ctx context.Context, window timeRange, resolution string) error {
	if resolution != "hour" || a.dependencies.Telemetry == nil || a.dependencies.BillingLocation == nil {
		return nil
	}
	location, err := a.dependencies.BillingLocation(ctx)
	if err != nil {
		return err
	}
	if location == nil {
		location = time.UTC
	}
	now := a.dependencies.Now().In(location)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	dayEnd := dayStart.AddDate(0, 0, 1)
	if !window.from.Before(dayEnd) || !window.to.After(dayStart) || !dayStart.Before(now) {
		return nil
	}
	return a.dependencies.Telemetry.RebuildSummaries(ctx, dayStart, now)
}
