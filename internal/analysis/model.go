package analysis

import (
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

type DaylightHour struct {
	At            time.Time
	DurationHours float64
}

type WeatherHour struct {
	At                     time.Time
	IrradianceWM2          float64
	ReferenceIrradianceWM2 float64
}

type WeatherContext struct {
	Available   bool
	Stale       bool
	CoveragePct float64
	Hours       []WeatherHour
}

type Input struct {
	Day                  time.Time
	Baseline             Baseline
	InstalledWatts       float64
	ActualWh             float64
	TelemetryCoveragePct float64
	Daylight             []DaylightHour
	Weather              WeatherContext
}

type Result = domain.AnalysisResult

func Evaluate(input Input) Result {
	actual := input.ActualWh
	if !finite(actual) || actual < 0 {
		actual = 0
	}
	installed := input.InstalledWatts
	if !finite(installed) || installed < 0 {
		installed = 0
	}
	weather := make(map[Bucket]WeatherHour, len(input.Weather.Hours))
	for _, hour := range input.Weather.Hours {
		if hour.At.IsZero() {
			continue
		}
		weather[Bucket{Month: hour.At.Month(), Hour: hour.At.Hour()}] = hour
	}

	var expected, daylightHours float64
	matched := 0
	weatherMatched := 0
	for _, daylight := range input.Daylight {
		duration := daylight.DurationHours
		if !finite(duration) || duration <= 0 || daylight.At.IsZero() {
			continue
		}
		daylightHours += duration
		key := Bucket{Month: daylight.At.Month(), Hour: daylight.At.Hour()}
		bucket, ok := input.Baseline.Buckets[key]
		if !ok || bucket.SampleCount <= 0 || !finite(bucket.NormalizedPower) {
			continue
		}
		matched++
		ratio := 1.0
		if input.Weather.Available {
			if hour, ok := weather[key]; ok && finite(hour.IrradianceWM2) && finite(hour.ReferenceIrradianceWM2) && hour.ReferenceIrradianceWM2 > 0 {
				weatherMatched++
				ratio = clamp(hour.IrradianceWM2/hour.ReferenceIrradianceWM2, .25, 1.15)
			}
		}
		expected += clamp(bucket.NormalizedPower, 0, 1) * installed * duration * ratio
	}
	expected = clamp(expected, 0, installed*daylightHours)
	modelCoverage := 0.0
	if len(input.Daylight) > 0 {
		modelCoverage = 100 * float64(matched) / float64(len(input.Daylight))
	}
	weatherCoverage := 0.0
	if matched > 0 {
		weatherCoverage = 100 * float64(weatherMatched) / float64(matched)
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
	return Result{ExpectedWh: expected, ActualWh: actual, Ratio: ratio, Confidence: confidence, Evidence: evidence, Qualifying: qualifying}
}

func InstalledWatts(settings domain.Settings) float64 {
	if settings.PanelCount <= 0 || settings.PanelWattage <= 0 || len(settings.ActiveMPPT) == 0 {
		return 0
	}
	return float64(settings.PanelCount * settings.PanelWattage)
}
