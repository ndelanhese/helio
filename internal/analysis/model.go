package analysis

import (
	"fmt"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/weather"
)

// standardTestIrradianceWM2 is the standard-test-condition irradiance used
// only to scale the learned normalized curve. It is not a claim that cached
// horizontal irradiance is plane-of-array irradiance or a full PV simulation.
const standardTestIrradianceWM2 = 1000

type DaylightHour struct {
	At            time.Time
	DurationHours float64
}

type WeatherHour struct {
	At            time.Time
	IrradianceWM2 float64
}

type WeatherContext struct {
	Available   bool
	Stale       bool
	CoveragePct float64
	Hours       []WeatherHour
}

type localHourKey struct {
	Year   int
	Month  time.Month
	Day    int
	Hour   int
	Offset int
}

type Input struct {
	Timezone             string
	Day                  time.Time
	Baseline             Baseline
	InstalledWatts       float64
	ActualWh             float64
	TelemetryCoveragePct float64
	Daylight             []DaylightHour
	Weather              WeatherContext
}

type Result = domain.AnalysisResult

func Evaluate(input Input) (Result, error) {
	location, err := configuredLocation(input.Timezone)
	if err != nil {
		return Result{}, fmt.Errorf("evaluate production expectation: %w", err)
	}
	if input.Day.IsZero() {
		return Result{}, fmt.Errorf("evaluate production expectation: day is required")
	}
	actual := input.ActualWh
	if !finite(actual) || actual < 0 {
		actual = 0
	}
	installed := input.InstalledWatts
	if !finite(installed) || installed < 0 {
		installed = 0
	}
	weather := make(map[localHourKey]WeatherHour, len(input.Weather.Hours))
	for _, hour := range input.Weather.Hours {
		if hour.At.IsZero() {
			continue
		}
		weather[localHour(hour.At, location)] = hour
	}

	var expected, daylightHours float64
	var matchedHours, weatherMatchedHours float64
	for _, daylight := range input.Daylight {
		duration := daylight.DurationHours
		if !finite(duration) || duration <= 0 || daylight.At.IsZero() {
			continue
		}
		daylightHours += duration
		local := daylight.At.In(location)
		key := Bucket{Month: local.Month(), Hour: local.Hour()}
		bucket, ok := input.Baseline.Buckets[key]
		if !ok || bucket.SampleCount <= 0 || !finite(bucket.NormalizedPower) {
			continue
		}
		matchedHours += duration
		ratio := 1.0
		if input.Weather.Available {
			if hour, ok := weather[localHour(daylight.At, location)]; ok && finite(hour.IrradianceWM2) {
				weatherMatchedHours += duration
				ratio = clamp(hour.IrradianceWM2/standardTestIrradianceWM2, .25, 1.15)
			}
		}
		expected += clamp(bucket.NormalizedPower, 0, 1) * installed * duration * ratio
	}
	expected = clamp(expected, 0, installed*daylightHours)
	modelCoverage := 0.0
	if daylightHours > 0 {
		modelCoverage = 100 * matchedHours / daylightHours
	}
	weatherCoverage := 0.0
	if matchedHours > 0 {
		weatherCoverage = 100 * weatherMatchedHours / matchedHours
	}
	if finite(input.Weather.CoveragePct) && input.Weather.CoveragePct < weatherCoverage {
		weatherCoverage = input.Weather.CoveragePct
	}
	confidence, qualifying, evidence := assessConfidence(input, modelCoverage, weatherCoverage)
	ratio := 0.0
	if expected > 0 {
		ratio = actual / expected
		if !finite(ratio) || ratio < 0 {
			ratio = 0
		}
	}
	return Result{ExpectedWh: expected, ActualWh: actual, Ratio: ratio, Confidence: confidence, Evidence: evidence, Qualifying: qualifying}, nil
}

// WeatherContextFromResult adapts the cached provider-neutral Task 1 weather
// result into model input and reports usable coverage over configured-local
// daylight buckets. Missing weather remains an explicit unavailable context.
func WeatherContextFromResult(result weather.Result, timezone string, daylight []DaylightHour) (WeatherContext, error) {
	location, err := configuredLocation(timezone)
	if err != nil {
		return WeatherContext{}, fmt.Errorf("adapt weather context: %w", err)
	}
	context := WeatherContext{Available: result.Available && len(result.Hours) > 0, Stale: result.Stale}
	if !context.Available {
		return context, nil
	}
	available := make(map[localHourKey]struct{}, len(result.Hours))
	for _, hour := range result.Hours {
		if hour.Time.IsZero() || !finite(hour.IrradianceWM2) {
			continue
		}
		available[localHour(hour.Time, location)] = struct{}{}
		context.Hours = append(context.Hours, WeatherHour{At: hour.Time, IrradianceWM2: hour.IrradianceWM2})
	}
	var total, covered float64
	for _, hour := range daylight {
		if hour.At.IsZero() || !finite(hour.DurationHours) || hour.DurationHours <= 0 {
			continue
		}
		total += hour.DurationHours
		if _, ok := available[localHour(hour.At, location)]; ok {
			covered += hour.DurationHours
		}
	}
	if total > 0 {
		context.CoveragePct = 100 * covered / total
	}
	return context, nil
}

func localHour(at time.Time, location *time.Location) localHourKey {
	local := at.In(location)
	_, offset := local.Zone()
	return localHourKey{Year: local.Year(), Month: local.Month(), Day: local.Day(), Hour: local.Hour(), Offset: offset}
}

func InstalledWatts(settings domain.Settings) float64 {
	if settings.PanelCount <= 0 || settings.PanelWattage <= 0 || len(settings.ActiveMPPT) == 0 {
		return 0
	}
	return float64(settings.PanelCount * settings.PanelWattage)
}
