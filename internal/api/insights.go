package api

import (
	"errors"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

type trendDTO struct {
	Direction   string  `json:"direction"`
	Current     float64 `json:"current"`
	Previous    float64 `json:"previous"`
	Delta       float64 `json:"delta"`
	DeltaPct    float64 `json:"deltaPct"`
	CoveragePct float64 `json:"coveragePct"`
	WindowDays  int     `json:"windowDays"`
}

type insightEvidenceDTO struct {
	Code  string  `json:"code"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

type insightsDTO struct {
	Version           string               `json:"version"`
	Day               string               `json:"day"`
	ActualWh          float64              `json:"actualWh"`
	ExpectedWh        float64              `json:"expectedWh"`
	Ratio             float64              `json:"ratio"`
	Confidence        domain.Confidence    `json:"confidence"`
	Qualifying        bool                 `json:"qualifying"`
	Evidence          []insightEvidenceDTO `json:"evidence"`
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
		Evidence: insightEvidence(result.Evidence),
	}
	dto.ObservationWindow.MinimumDays = 7
	for _, evidence := range result.Evidence {
		if evidence.Code == "history_days" && evidence.Value >= 0 {
			dto.ObservationWindow.QualifyingDays = int(math.Round(evidence.Value))
			break
		}
	}
	minor, err := generatedValueMinor(result.ActualWh, settings.TariffMinorPerKWh)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "insights could not be loaded")
		return
	}
	dto.GeneratedEnergyValue.Minor = minor
	dto.GeneratedEnergyValue.Currency = settings.Currency
	dto.GeneratedEnergyValue.Label = "valor estimado da energia gerada"
	dto.GeneratedEnergyValue.Estimate = true
	dto.Trends.PeakPower = trendDTO{Direction: "insufficient", WindowDays: 0}
	dto.Trends.ProductiveMinutes = trendDTO{Direction: "insufficient", WindowDays: 0}
	if a.dependencies.Summaries != nil {
		windowStart, windowEnd := dayStart.AddDate(0, 0, -6), dayStart.AddDate(0, 0, 1)
		points, historyErr := a.dependencies.Summaries.DailyHistory(r.Context(), windowStart, windowEnd)
		if historyErr != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "insights could not be loaded")
			return
		}
		if dto.ObservationWindow.QualifyingDays == 0 {
			dto.ObservationWindow.QualifyingDays = qualifyingSummaryDays(points)
		}
		dto.Trends.PeakPower = summarizeTrend(points, windowStart, windowEnd, location, func(point domain.AggregatePoint) float64 { return point.PeakPowerW })
		dto.Trends.ProductiveMinutes = summarizeTrend(points, windowStart, windowEnd, location, func(point domain.AggregatePoint) float64 { return float64(point.ProductiveMinutes) })
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

func summarizeTrend(points []domain.AggregatePoint, windowStart, windowEnd time.Time, location *time.Location, value func(domain.AggregatePoint) float64) trendDTO {
	if location == nil {
		location = time.UTC
	}
	windowDays := 0
	for day := windowStart.In(location); day.Before(windowEnd); day = day.AddDate(0, 0, 1) {
		windowDays++
	}
	result := trendDTO{Direction: "insufficient", WindowDays: windowDays}
	previousPoints, currentPoints := make([]domain.AggregatePoint, 0, len(points)), make([]domain.AggregatePoint, 0, len(points))
	midpoint := windowStart.In(location).AddDate(0, 0, windowDays/2)
	totalDuration := windowEnd.Sub(windowStart).Hours()
	for _, point := range points {
		local := point.At.In(location)
		dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
		if dayStart.Before(windowStart) || !dayStart.Before(windowEnd) {
			continue
		}
		dayDuration := dayStart.AddDate(0, 0, 1).Sub(dayStart).Hours()
		result.CoveragePct += point.CoveragePct * dayDuration
		if point.CoveragePct >= 80 {
			if dayStart.Before(midpoint) {
				previousPoints = append(previousPoints, point)
			} else {
				currentPoints = append(currentPoints, point)
			}
		}
	}
	if totalDuration > 0 {
		result.CoveragePct /= totalDuration
	}
	if len(previousPoints)+len(currentPoints) < 4 || len(previousPoints) == 0 || len(currentPoints) == 0 {
		return result
	}
	average := func(slice []domain.AggregatePoint) float64 {
		var sum float64
		for _, point := range slice {
			sum += value(point)
		}
		return sum / float64(len(slice))
	}
	previous, current := average(previousPoints), average(currentPoints)
	result.Previous, result.Current = previous, current
	result.Delta = current - previous
	if previous <= 0 {
		return result
	}
	result.DeltaPct = result.Delta * 100 / previous
	result.Direction = "stable"
	if result.DeltaPct > 5 {
		result.Direction = "up"
	}
	if result.DeltaPct < -5 {
		result.Direction = "down"
	}
	return result
}

const maxSafeJSONInteger int64 = 9_007_199_254_740_991

func generatedValueMinor(actualWh float64, tariffMinorPerKWh int64) (int64, error) {
	if math.IsNaN(actualWh) || math.IsInf(actualWh, 0) || actualWh < 0 || tariffMinorPerKWh < 0 {
		return 0, errors.New("invalid generated value operands")
	}
	actual, ok := new(big.Rat).SetString(strconv.FormatFloat(actualWh, 'f', -1, 64))
	if !ok {
		return 0, errors.New("invalid generated energy")
	}
	value := new(big.Rat).Mul(actual, new(big.Rat).SetInt64(tariffMinorPerKWh))
	value.Quo(value, big.NewRat(1000, 1))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(value.Num(), value.Denom(), remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(value.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() || quotient.Sign() < 0 || quotient.Cmp(big.NewInt(maxSafeJSONInteger)) > 0 {
		return 0, errors.New("generated value exceeds safe JSON integer range")
	}
	return quotient.Int64(), nil
}

var safeInsightEvidence = map[string]struct{ label, unit string }{
	"history_days":               {"Dias qualificáveis no histórico", "dias"},
	"history_insufficient":       {"Histórico ainda insuficiente", "dias"},
	"telemetry_coverage_low":     {"Cobertura da telemetria abaixo do mínimo", "%"},
	"telemetry_coverage_reduced": {"Cobertura reduzida da telemetria", "%"},
	"model_coverage_reduced":     {"Cobertura reduzida da referência", "%"},
	"weather_missing":            {"Contexto meteorológico indisponível", "%"},
	"weather_stale":              {"Contexto meteorológico desatualizado", ""},
	"weather_coverage_reduced":   {"Cobertura meteorológica útil", "%"},
}

func insightEvidence(evidence []domain.Evidence) []insightEvidenceDTO {
	result := make([]insightEvidenceDTO, 0, len(evidence))
	for _, item := range evidence {
		copy, ok := safeInsightEvidence[item.Code]
		if !ok || math.IsNaN(item.Value) || math.IsInf(item.Value, 0) {
			continue
		}
		result = append(result, insightEvidenceDTO{Code: item.Code, Label: copy.label, Value: item.Value, Unit: copy.unit})
	}
	return result
}
