package api

import (
	"net/http"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

type timeRange struct{ from, to time.Time }

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
	points, err := a.dependencies.History.History(r.Context(), window.from, window.to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "history could not be loaded")
		return
	}
	for index := range points {
		points[index].At = points[index].At.UTC()
	}
	points = coalesceHistory(points, resolution)
	writeJSON(w, http.StatusOK, map[string]any{"from": window.from, "to": window.to, "resolution": resolution, "points": points})
}

func coalesceHistory(points []domain.HistoryPoint, resolution string) []domain.HistoryPoint {
	if resolution == "minute" || len(points) == 0 {
		return points
	}
	result := make([]domain.HistoryPoint, 0)
	counts := make([]int, 0)
	for _, point := range points {
		bucket := historyBucket(point.At.UTC(), resolution)
		last := len(result) - 1
		if last < 0 || !result[last].At.Equal(bucket) {
			result = append(result, domain.HistoryPoint{At: bucket, PowerW: point.PowerW})
			counts = append(counts, 1)
			continue
		}
		result[last].PowerW += point.PowerW
		counts[last]++
	}
	for index := range result {
		result[index].PowerW /= float64(counts[index])
	}
	return result
}

func historyBucket(at time.Time, resolution string) time.Time {
	switch resolution {
	case "hour":
		return at.Truncate(time.Hour)
	case "day":
		return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	case "month":
		return time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		return at.Truncate(time.Minute)
	}
}
