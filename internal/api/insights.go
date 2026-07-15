package api

import (
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

type trendDTO struct {
	Direction  string  `json:"direction"`
	ChangePct  float64 `json:"changePct"`
	WindowDays int     `json:"windowDays"`
}

type insightsDTO struct {
	Version           string            `json:"version"`
	Day               string            `json:"day"`
	ActualWh          float64           `json:"actualWh"`
	ExpectedWh        float64           `json:"expectedWh"`
	Ratio             float64           `json:"ratio"`
	Confidence        domain.Confidence `json:"confidence"`
	Qualifying        bool              `json:"qualifying"`
	Evidence          []domain.Evidence `json:"evidence"`
	ObservationWindow struct {
		QualifyingDays int `json:"qualifyingDays"`
		MinimumDays    int `json:"minimumDays"`
	} `json:"observationWindow"`
	Trends struct {
		PeakPower         trendDTO `json:"peakPower"`
		ProductiveMinutes trendDTO `json:"productiveMinutes"`
	} `json:"trends"`
	GeneratedEnergyValue struct {
		Minor    int64  `json:"minor"`
		Currency string `json:"currency"`
		Label    string `json:"label"`
		Estimate bool   `json:"estimate"`
	} `json:"generatedEnergyValue"`
}

func (a *API) insights(w http.ResponseWriter, r *http.Request) {
	if a.dependencies.Insights == nil || a.dependencies.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "insights are unavailable")
		return
	}
	settings, err := a.dependencies.Store.GetSettings(r.Context(), a.dependencies.AllowPublicLogger)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "insights could not be loaded")
		return
	}
	location, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "insights could not be loaded")
		return
	}
	day, dayStart, err := parseLocalDate(r.URL.Query().Get("day"), location)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_day", "day must be a valid local date in YYYY-MM-DD format")
		return
	}
	result, found, err := a.dependencies.Insights.Load(r.Context(), day)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "insights could not be loaded")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "insights_not_found", "insights are not available for this day")
		return
	}
	dto := insightsDTO{
		Version: "v1", Day: result.Day, ActualWh: result.ActualWh, ExpectedWh: result.ExpectedWh,
		Ratio: result.Ratio, Confidence: result.Confidence, Qualifying: result.Qualifying,
		Evidence: append([]domain.Evidence(nil), result.Evidence...),
	}
	dto.ObservationWindow.MinimumDays = 7
	for _, evidence := range result.Evidence {
		if evidence.Code == "history_days" && evidence.Unit == "days" && evidence.Value >= 0 {
			dto.ObservationWindow.QualifyingDays = int(math.Round(evidence.Value))
			break
		}
	}
	dto.GeneratedEnergyValue.Minor = int64(math.Round(result.ActualWh / 1000 * float64(settings.TariffMinorPerKWh)))
	dto.GeneratedEnergyValue.Currency = settings.Currency
	dto.GeneratedEnergyValue.Label = "valor estimado da energia gerada"
	dto.GeneratedEnergyValue.Estimate = true
	dto.Trends.PeakPower = trendDTO{Direction: "insufficient", WindowDays: 0}
	dto.Trends.ProductiveMinutes = trendDTO{Direction: "insufficient", WindowDays: 0}
	if a.dependencies.Summaries != nil {
		points, historyErr := a.dependencies.Summaries.DailyHistory(r.Context(), dayStart.AddDate(0, 0, -6), dayStart.AddDate(0, 0, 1))
		if historyErr != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "insights could not be loaded")
			return
		}
		if dto.ObservationWindow.QualifyingDays == 0 {
			dto.ObservationWindow.QualifyingDays = qualifyingSummaryDays(points)
		}
		dto.Trends.PeakPower = summarizeTrend(points, func(point domain.AggregatePoint) float64 { return point.PeakPowerW })
		dto.Trends.ProductiveMinutes = summarizeTrend(points, func(point domain.AggregatePoint) float64 { return float64(point.ProductiveMinutes) })
	}
	writeJSON(w, http.StatusOK, dto)
}

func parseLocalDate(value string, location *time.Location) (string, time.Time, error) {
	if len(value) != len("2006-01-02") {
		return "", time.Time{}, errors.New("invalid date")
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, location)
	if err != nil || parsed.Format("2006-01-02") != value {
		return "", time.Time{}, errors.New("invalid date")
	}
	return value, parsed, nil
}

func qualifyingSummaryDays(points []domain.AggregatePoint) int {
	count := 0
	for _, point := range points {
		if point.CoveragePct >= 80 {
			count++
		}
	}
	return count
}

func summarizeTrend(points []domain.AggregatePoint, value func(domain.AggregatePoint) float64) trendDTO {
	result := trendDTO{Direction: "insufficient", WindowDays: len(points)}
	if len(points) < 4 {
		return result
	}
	middle := len(points) / 2
	average := func(slice []domain.AggregatePoint) float64 {
		var sum float64
		for _, point := range slice {
			sum += value(point)
		}
		return sum / float64(len(slice))
	}
	previous, current := average(points[:middle]), average(points[middle:])
	if previous <= 0 {
		return result
	}
	result.ChangePct = (current - previous) * 100 / previous
	result.Direction = "stable"
	if result.ChangePct > 5 {
		result.Direction = "up"
	}
	if result.ChangePct < -5 {
		result.Direction = "down"
	}
	return result
}
