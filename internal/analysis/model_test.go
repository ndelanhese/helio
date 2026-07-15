package analysis

import (
	"math"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/weather"
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
			got := mustEvaluate(t, standardInput(tc.days, tc.telemetry, tc.weather))
			if got.Confidence != tc.want || got.Qualifying != tc.qualifying {
				t.Fatalf("got confidence=%s qualifying=%v, want %s/%v; evidence=%+v", got.Confidence, got.Qualifying, tc.want, tc.qualifying, got.Evidence)
			}
		})
	}
}

func TestEvaluateWeatherIrradianceMonotonicAndMissingEvidence(t *testing.T) {
	clear := mustEvaluate(t, standardInput(30, 100, freshWeather(100)))
	cloudyWeather := freshWeather(100)
	cloudyWeather.Hours = weatherHours(250)
	cloudy := mustEvaluate(t, standardInput(30, 100, cloudyWeather))
	missing := mustEvaluate(t, standardInput(30, 100, WeatherContext{}))
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
	got := mustEvaluate(t, input)
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
	got := mustEvaluate(t, input)
	if got.Confidence != domain.ConfidenceMedium || !hasEvidence(got.Evidence, "model_coverage_reduced") {
		t.Fatalf("result = %+v", got)
	}
}

func TestEvaluateWeatherCoverageUsesUsableHours(t *testing.T) {
	weather := freshWeather(100)
	weather.Hours = weather.Hours[:2]
	got := mustEvaluate(t, standardInput(30, 100, weather))
	if got.Confidence != domain.ConfidenceMedium || !hasEvidence(got.Evidence, "weather_coverage_reduced") {
		t.Fatalf("result = %+v", got)
	}
}

func TestEvaluateInstalledWattsMonotonic(t *testing.T) {
	smaller := standardInput(30, 100, freshWeather(100))
	smaller.InstalledWatts = 3000
	larger := smaller
	larger.InstalledWatts = 6000
	if gotSmall, gotLarge := mustEvaluate(t, smaller), mustEvaluate(t, larger); gotLarge.ExpectedWh < gotSmall.ExpectedWh {
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
		got, err := Evaluate(input)
		if err != nil {
			return
		}
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
	return Input{Timezone: "UTC", Day: day, Baseline: baseline, InstalledWatts: 4000, ActualWh: 5000, TelemetryCoveragePct: telemetry, Daylight: daylight, Weather: weather}
}

func freshWeather(coverage float64) WeatherContext {
	return WeatherContext{Available: true, CoveragePct: coverage, Hours: weatherHours(1000)}
}

func weatherHours(irradiance float64) []WeatherHour {
	day := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
	hours := make([]WeatherHour, 4)
	for i := range hours {
		hours[i] = WeatherHour{At: day.Add(time.Duration(i) * time.Hour), IrradianceWM2: irradiance}
	}
	return hours
}

func TestWeatherContextFromTask1HoursUsesConfiguredLocalBuckets(t *testing.T) {
	daylight := []DaylightHour{{At: time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC), DurationHours: 1}}
	result := weather.Result{Available: true, Hours: []weather.Hour{{Time: time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC), IrradianceWM2: 700}}, Stale: false}
	context, err := WeatherContextFromResult(result, "America/Sao_Paulo", daylight)
	if err != nil {
		t.Fatal(err)
	}
	if context.CoveragePct != 100 || len(context.Hours) != 1 || context.Hours[0].IrradianceWM2 != 700 {
		t.Fatalf("weather bridge = %+v", context)
	}
	input := standardInput(30, 100, context)
	input.Timezone = "America/Sao_Paulo"
	input.Day = daylight[0].At
	input.Daylight = daylight
	input.Baseline.Buckets = map[Bucket]BaselineBucket{{Month: time.April, Hour: 9}: {NormalizedPower: .5, SampleCount: 30}}
	got := mustEvaluate(t, input)
	if got.ExpectedWh != 1400 { // .5 * 4000 W * 1 h * (700/1000)
		t.Fatalf("expected = %v, want 1400", got.ExpectedWh)
	}
}

func TestWeatherContextDoesNotCountAnotherLocalDayAsCoverage(t *testing.T) {
	daylight := []DaylightHour{{At: time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC), DurationHours: 1}}
	result := weather.Result{Available: true, Hours: []weather.Hour{{Time: time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC), IrradianceWM2: 700}}}
	context, err := WeatherContextFromResult(result, "America/Sao_Paulo", daylight)
	if err != nil {
		t.Fatal(err)
	}
	if context.CoveragePct != 0 {
		t.Fatalf("weather from another local day counted as %v%% coverage", context.CoveragePct)
	}
}

func TestEvaluateInvalidCoverageCannotEarnConfidence(t *testing.T) {
	for _, coverage := range []float64{-1, 100.1, math.NaN(), math.Inf(1)} {
		input := standardInput(30, coverage, freshWeather(100))
		got := mustEvaluate(t, input)
		if got.Qualifying || got.Confidence != domain.ConfidenceLow || !hasEvidence(got.Evidence, "telemetry_coverage_low") {
			t.Fatalf("coverage %v produced %+v", coverage, got)
		}
	}
}

func TestEvaluateRejectsTimezoneAndRemainsBoundedForMalformedInputs(t *testing.T) {
	input := standardInput(30, math.Inf(1), freshWeather(100))
	input.Timezone = "Local"
	if _, err := Evaluate(input); err == nil {
		t.Fatal("host Local timezone accepted")
	}
	input.Timezone = "UTC"
	input.Day = time.Time{}
	if _, err := Evaluate(input); err == nil {
		t.Fatal("missing evaluation day accepted")
	}
	input.Timezone = "America/Sao_Paulo"
	input.Day = time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	input.Daylight = []DaylightHour{{At: time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC), DurationHours: .5}, {At: time.Time{}, DurationHours: math.Inf(1)}}
	input.Baseline.Buckets = map[Bucket]BaselineBucket{{Month: time.April, Hour: 9}: {NormalizedPower: math.Inf(1), SampleCount: 30}}
	input.Weather.Hours = []WeatherHour{{At: input.Daylight[0].At, IrradianceWM2: math.NaN()}}
	got := mustEvaluate(t, input)
	if got.ExpectedWh < 0 || got.ExpectedWh > input.InstalledWatts*.5 || math.IsNaN(got.ExpectedWh) || math.IsInf(got.ExpectedWh, 0) {
		t.Fatalf("malformed input escaped bounds: %+v", got)
	}
}

func TestEvaluateFractionalDaylightEnergy(t *testing.T) {
	input := standardInput(30, 100, freshWeather(100))
	input.Daylight = []DaylightHour{{At: input.Day, DurationHours: .5}}
	input.Weather.Hours = []WeatherHour{{At: input.Day, IrradianceWM2: 700}}
	got := mustEvaluate(t, input)
	if got.ExpectedWh != 700 { // .5 normalized * 4000 W * .5 h * .7 irradiance
		t.Fatalf("fractional expected = %v, want 700", got.ExpectedWh)
	}
}

func TestEvaluateMonotonicAcrossInstalledWattsAndIrradianceRanges(t *testing.T) {
	previous := -1.0
	for watts := 0.0; watts <= 12000; watts += 137.5 {
		input := standardInput(30, 100, freshWeather(100))
		input.InstalledWatts = watts
		got := mustEvaluate(t, input)
		if got.ExpectedWh < previous {
			t.Fatalf("installed monotonicity: %v after %v", got.ExpectedWh, previous)
		}
		previous = got.ExpectedWh
	}
	previous = -1
	for irradiance := -100.0; irradiance <= 1600; irradiance += 17.5 {
		input := standardInput(30, 100, freshWeather(100))
		input.Weather.Hours = weatherHours(irradiance)
		got := mustEvaluate(t, input)
		if got.ExpectedWh < previous {
			t.Fatalf("irradiance monotonicity at %v: %v after %v", irradiance, got.ExpectedWh, previous)
		}
		previous = got.ExpectedWh
	}
}

func mustEvaluate(t *testing.T, input Input) Result {
	t.Helper()
	result, err := Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func hasEvidence(evidence []domain.Evidence, code string) bool {
	for _, item := range evidence {
		if item.Code == code {
			return true
		}
	}
	return false
}
