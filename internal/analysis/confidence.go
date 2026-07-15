package analysis

import "github.com/ndelanhese/helio/internal/domain"

func assessConfidence(input Input, modelCoverage, weatherCoverage float64) (domain.Confidence, bool, []domain.Evidence) {
	evidence := []domain.Evidence{{Code: "history_days", Label: "Qualifying history days", Value: float64(input.Baseline.QualifyingDays), Unit: "days"}}
	if input.Baseline.QualifyingDays < 7 {
		evidence = append(evidence, domain.Evidence{Code: "history_insufficient", Label: "At least seven qualifying days are required", Value: float64(input.Baseline.QualifyingDays), Unit: "days"})
		return domain.ConfidenceLow, false, evidence
	}
	level := 1
	if input.Baseline.QualifyingDays >= 30 {
		level = 2
	}
	qualifying := finite(input.TelemetryCoveragePct) && input.TelemetryCoveragePct >= qualifyingCoveragePct
	if !qualifying {
		evidence = append(evidence, domain.Evidence{Code: "telemetry_coverage_low", Label: "Telemetry coverage is below the qualifying threshold", Value: safeValue(input.TelemetryCoveragePct), Unit: "percent"})
		level = 0
	} else if input.TelemetryCoveragePct < 90 {
		evidence = append(evidence, domain.Evidence{Code: "telemetry_coverage_reduced", Label: "Telemetry coverage reduces confidence", Value: input.TelemetryCoveragePct, Unit: "percent"})
		level = min(level, 1)
	}
	if modelCoverage < 90 {
		evidence = append(evidence, domain.Evidence{Code: "model_coverage_reduced", Label: "Learned baseline coverage reduces confidence", Value: safeValue(modelCoverage), Unit: "percent"})
		level = min(level, 1)
	}
	if !input.Weather.Available {
		evidence = append(evidence, domain.Evidence{Code: "weather_missing", Label: "Weather unavailable; seasonal hourly baseline used", Value: 0, Unit: "percent"})
		level = min(level, 1)
	} else {
		if input.Weather.Stale {
			evidence = append(evidence, domain.Evidence{Code: "weather_stale", Label: "Weather data is stale", Value: 1, Unit: "boolean"})
			level = max(0, level-1)
		}
		if weatherCoverage < 90 {
			evidence = append(evidence, domain.Evidence{Code: "weather_coverage_reduced", Label: "Usable weather coverage reduces confidence", Value: safeValue(weatherCoverage), Unit: "percent"})
			level = min(level, 1)
		}
	}
	return []domain.Confidence{domain.ConfidenceLow, domain.ConfidenceMedium, domain.ConfidenceHigh}[level], qualifying, evidence
}

func safeValue(value float64) float64 {
	if !finite(value) {
		return 0
	}
	return value
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
