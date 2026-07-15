package analysis

import (
	"math"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

func TestEvaluateConfidenceTable(t *testing.T) {
	tests := []struct {
		name       string
		days       int
		telemetry  float64
		weather    WeatherContext
		want       domain.Confidence
		qualifying bool
	}{
		{"six days", 6, 100, freshWeather(100), domain.ConfidenceLow, false},
		{"seven days", 7, 90, freshWeather(90), domain.ConfidenceMedium, true},
		{"twenty nine days", 29, 100, freshWeather(100), domain.ConfidenceMedium, true},
		{"thirty complete days", 30, 90, freshWeather(90), domain.ConfidenceHigh, true},
		{"thirty days low telemetry", 30, 89.9, freshWeather(100), domain.ConfidenceMedium, true},
		{"thirty days stale weather", 30, 100, WeatherContext{Available: true, Stale: true, CoveragePct: 100, Hours: weatherHours(1000)}, domain.ConfidenceMedium, true},
		{"thirty days missing weather", 30, 100, WeatherContext{}, domain.ConfidenceMedium, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(standardInput(tc.days, tc.telemetry, tc.weather))
			if got.Confidence != tc.want || got.Qualifying != tc.qualifying {
				t.Fatalf("got confidence=%s qualifying=%v, want %s/%v; evidence=%+v", got.Confidence, got.Qualifying, tc.want, tc.qualifying, got.Evidence)
			}
		})
	}
}

func TestEvaluateWeatherIrradianceMonotonicAndMissingEvidence(t *testing.T) {
	clear := Evaluate(standardInput(30, 100, freshWeather(100)))
	cloudyWeather := freshWeather(100)
	cloudyWeather.Hours = weatherHours(250)
	cloudy := Evaluate(standardInput(30, 100, cloudyWeather))
	missing := Evaluate(standardInput(30, 100, WeatherContext{}))
	if cloudy.ExpectedWh >= clear.ExpectedWh {
		t.Fatalf("cloudy expected %v >= clear %v", cloudy.ExpectedWh, clear.ExpectedWh)
	}
	if missing.ExpectedWh <= 0 || !hasEvidence(missing.Evidence, "weather_missing") {
		t.Fatalf("missing weather result = %+v", missing)
	}
}

func TestEvaluateBoundsRatiosAndInstalledCapacity(t *testing.T) {
	input := standardInput(30, 100, freshWeather(100))
	input.ActualWh = 2200
	input.InstalledWatts = 1000
	input.Daylight = []DaylightHour{{At: input.Day, DurationHours: 1}, {At: input.Day.Add(time.Hour), DurationHours: 1}}
	got := Evaluate(input)
	if got.ExpectedWh < 0 || got.ExpectedWh > 2000 || math.IsNaN(got.ExpectedWh) || math.IsInf(got.ExpectedWh, 0) {
		t.Fatalf("expected out of bounds: %+v", got)
	}
	if got.Ratio != got.ActualWh/got.ExpectedWh {
		t.Fatalf("ratio = %v", got.Ratio)
	}

	settings := domain.Settings{PanelCount: 7, PanelWattage: 610, ActiveMPPT: []int{1}}
	if watts := InstalledWatts(settings); watts != 4270 {
		t.Fatalf("installed watts with inactive PV2 = %v", watts)
	}
}

func TestEvaluateModelCoverageReduction(t *testing.T) {
	input := standardInput(30, 100, freshWeather(100))
	input.Daylight = append(input.Daylight, DaylightHour{At: input.Day.Add(4 * time.Hour), DurationHours: 1})
	got := Evaluate(input)
	if got.Confidence != domain.ConfidenceMedium || !hasEvidence(got.Evidence, "model_coverage_reduced") {
		t.Fatalf("result = %+v", got)
	}
}

func TestEvaluateWeatherCoverageUsesUsableHours(t *testing.T) {
	weather := freshWeather(100)
	weather.Hours = weather.Hours[:2]
	got := Evaluate(standardInput(30, 100, weather))
	if got.Confidence != domain.ConfidenceMedium || !hasEvidence(got.Evidence, "weather_coverage_reduced") {
		t.Fatalf("result = %+v", got)
	}
}

func TestEvaluateInstalledWattsMonotonic(t *testing.T) {
	smaller := standardInput(30, 100, freshWeather(100))
	smaller.InstalledWatts = 3000
	larger := smaller
	larger.InstalledWatts = 6000
	if gotSmall, gotLarge := Evaluate(smaller), Evaluate(larger); gotLarge.ExpectedWh < gotSmall.ExpectedWh {
		t.Fatalf("larger installation expected %v < smaller %v", gotLarge.ExpectedWh, gotSmall.ExpectedWh)
	}
}

func FuzzEvaluateBounded(f *testing.F) {
	f.Add(4270.0, 1000.0)
	f.Add(-1.0, math.NaN())
	f.Fuzz(func(t *testing.T, watts, irradiance float64) {
		input := standardInput(30, 100, freshWeather(100))
		input.InstalledWatts = watts
		input.Weather.Hours = weatherHours(irradiance)
		got := Evaluate(input)
		if got.ExpectedWh < 0 || math.IsNaN(got.ExpectedWh) || math.IsInf(got.ExpectedWh, 0) {
			t.Fatalf("unbounded result: %+v", got)
		}
	})
}

func standardInput(days int, telemetry float64, weather WeatherContext) Input {
	day := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
	baseline := Baseline{QualifyingDays: days, Buckets: map[Bucket]BaselineBucket{}}
	daylight := make([]DaylightHour, 4)
	for i := range daylight {
		at := day.Add(time.Duration(i) * time.Hour)
		daylight[i] = DaylightHour{At: at, DurationHours: 1}
		baseline.Buckets[Bucket{Month: at.Month(), Hour: at.Hour()}] = BaselineBucket{NormalizedPower: .5, SampleCount: days}
	}
	return Input{Day: day, Baseline: baseline, InstalledWatts: 4000, ActualWh: 5000, TelemetryCoveragePct: telemetry, Daylight: daylight, Weather: weather}
}

func freshWeather(coverage float64) WeatherContext {
	return WeatherContext{Available: true, CoveragePct: coverage, Hours: weatherHours(1000)}
}

func weatherHours(irradiance float64) []WeatherHour {
	day := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
	hours := make([]WeatherHour, 4)
	for i := range hours {
		hours[i] = WeatherHour{At: day.Add(time.Duration(i) * time.Hour), IrradianceWM2: irradiance, ReferenceIrradianceWM2: 1000}
	}
	return hours
}

func hasEvidence(evidence []domain.Evidence, code string) bool {
	for _, item := range evidence {
		if item.Code == code {
			return true
		}
	}
	return false
}
