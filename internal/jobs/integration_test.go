package jobs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/alerts"
	"github.com/ndelanhese/helio/internal/analysis"
	"github.com/ndelanhese/helio/internal/collector"
	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/storage"
	"github.com/ndelanhese/helio/internal/weather"
)

type fakeAnalysisData struct{ daily, hourly []domain.AggregatePoint }

func (f fakeAnalysisData) DailyHistory(context.Context, time.Time, time.Time) ([]domain.AggregatePoint, error) {
	return append([]domain.AggregatePoint(nil), f.daily...), nil
}
func (f fakeAnalysisData) HourlyHistory(context.Context, time.Time, time.Time) ([]domain.AggregatePoint, error) {
	return append([]domain.AggregatePoint(nil), f.hourly...), nil
}

type fakeAnalysisWriter struct {
	mu     sync.Mutex
	values []domain.DailyAnalysis
	order  *[]string
}

func (f *fakeAnalysisWriter) Save(_ context.Context, value domain.DailyAnalysis) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values = append(f.values, value)
	if f.order != nil {
		*f.order = append(*f.order, "analysis")
	}
	return nil
}

type fakeWeather struct {
	mu     sync.Mutex
	calls  []weather.Request
	result weather.Result
}

func (f *fakeWeather) Get(_ context.Context, request weather.Request) weather.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, request)
	return f.result
}

type fakeAlertEvaluator struct {
	mu     sync.Mutex
	inputs []alerts.Input
	order  *[]string
}

func (f *fakeAlertEvaluator) Evaluate(_ context.Context, input alerts.Input) ([]alerts.Transition, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, input)
	if f.order != nil && input.Analysis != nil {
		*f.order = append(*f.order, "daily-alert")
	}
	return nil, nil
}

func TestRunnerRunsActualAnalysisAfterAggregationBeforeDailyAlert(t *testing.T) {
	location, _ := time.LoadLocation("America/Sao_Paulo")
	dayStart := time.Date(2026, 7, 14, 0, 0, 0, 0, location)
	now := dayStart.AddDate(0, 0, 1).Add(5 * time.Minute)
	repository := &fakeRepository{dailyResult: domain.DailySummary{Day: "2026-07-14", EnergyWh: 800, CoveragePct: 95}}
	var daily []domain.AggregatePoint
	var hourly []domain.AggregatePoint
	for i := range 7 {
		day := dayStart.AddDate(0, 0, -7+i)
		daily = append(daily, domain.AggregatePoint{At: day.UTC(), EnergyWh: 3_000, CoveragePct: 95})
		for hour := 6; hour < 18; hour++ {
			hourly = append(hourly, domain.AggregatePoint{At: time.Date(day.Year(), day.Month(), day.Day(), hour, 0, 0, 0, location).UTC(), EnergyWh: 200, CoveragePct: 100})
		}
	}
	order := []string{}
	writer := &fakeAnalysisWriter{order: &order}
	weatherService := &fakeWeather{result: weather.Result{Available: true, FetchedAt: now.UTC(), Hours: daylightWeather(dayStart, location)}}
	alertEngine := &fakeAlertEvaluator{order: &order}
	runner := New(repository, func(context.Context) (domain.Settings, error) {
		return domain.Settings{Timezone: location.String(), Latitude: -23.5, Longitude: -46.6, PanelCount: 7, PanelWattage: 610, ActiveMPPT: []int{1}}, nil
	}, WithClock(newFakeClock(now)), WithIntegration(Integration{
		AnalysisData: fakeAnalysisData{daily: daily, hourly: hourly}, AnalysisWriter: writer,
		Weather: weatherService, Alerts: alertEngine,
	}))
	if err := runner.runOnce(context.Background(), now, domain.Settings{Timezone: location.String(), Latitude: -23.5, Longitude: -46.6, PanelCount: 7, PanelWattage: 610, ActiveMPPT: []int{1}}, location); err != nil {
		t.Fatal(err)
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if len(writer.values) != 1 || writer.values[0].Day != "2026-07-14" || writer.values[0].ExpectedWh <= 0 || writer.values[0].Confidence != domain.ConfidenceMedium {
		t.Fatalf("analysis=%#v", writer.values)
	}
	if len(order) != 2 || order[0] != "analysis" || order[1] != "daily-alert" {
		t.Fatalf("order=%v", order)
	}
}

func TestRunnerRefreshesWeatherImmediatelyHourlyAndEvaluatesEveryCollectorEvent(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	clock := newFakeClock(now)
	weatherService := &fakeWeather{result: weather.Result{Available: true, FetchedAt: now, Hours: []weather.Hour{{Time: now.Truncate(time.Hour), IrradianceWM2: 500, FetchedAt: now}}}}
	alertEngine := &fakeAlertEvaluator{}
	hub := collector.NewHub()
	runner := New(&fakeRepository{}, func(context.Context) (domain.Settings, error) {
		return domain.Settings{Timezone: "UTC", Latitude: -23.5, Longitude: -46.6, ActiveMPPT: []int{1}}, nil
	}, WithClock(clock), WithIntegration(Integration{Weather: weatherService, Alerts: alertEngine, Events: hub}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runner.runIntegration(ctx); close(done) }()
	eventually(t, func() bool {
		weatherService.mu.Lock()
		defer weatherService.mu.Unlock()
		return len(weatherService.calls) == 1
	})
	hub.Publish(collector.Event{Kind: "snapshot", Snapshot: &domain.TelemetrySnapshot{ObservedAt: now, ACPowerW: 123, Grid: domain.Grid{VoltageV: 230, FrequencyHz: 60}}, State: collector.State{LastSuccess: now}})
	eventually(t, func() bool { alertEngine.mu.Lock(); defer alertEngine.mu.Unlock(); return len(alertEngine.inputs) == 1 })
	clock.advance(now.Add(time.Hour))
	eventually(t, func() bool {
		weatherService.mu.Lock()
		defer weatherService.mu.Unlock()
		return len(weatherService.calls) == 2
	})
	status := runner.WeatherStatus()
	if status.State != "available" || status.FetchedAt.IsZero() {
		t.Fatalf("weather status=%#v", status)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("integration loop did not stop")
	}
}

func daylightWeather(day time.Time, location *time.Location) []weather.Hour {
	hours := make([]weather.Hour, 0, 12)
	for hour := 6; hour < 18; hour++ {
		hours = append(hours, weather.Hour{Time: time.Date(day.Year(), day.Month(), day.Day(), hour, 0, 0, 0, location).UTC(), IrradianceWM2: 1_000, FetchedAt: day})
	}
	return hours
}

func TestDeterministic35DayAcceptance(t *testing.T) {
	const installed = 4_270.0
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	days := make([]analysis.TrainingDay, 0, 35)
	for index := range 35 {
		day := base.AddDate(0, 0, index)
		days = append(days, analysis.TrainingDay{Timezone: "UTC", Day: day, CoveragePct: 95, InstalledWatts: installed,
			Hours: []analysis.PowerHour{{At: day.Add(12 * time.Hour), PowerW: 2_000}}})
	}
	evaluate := func(count int) domain.AnalysisResult {
		baseline, err := analysis.BuildBaseline(days[:count])
		if err != nil {
			t.Fatal(err)
		}
		day := days[count-1].Day
		result, err := analysis.Evaluate(analysis.Input{Timezone: "UTC", Day: day, Baseline: baseline, InstalledWatts: installed,
			ActualWh: 2_000, TelemetryCoveragePct: 95,
			Daylight: []analysis.DaylightHour{{At: day.Add(12 * time.Hour), DurationHours: 1}},
			Weather:  analysis.WeatherContext{Available: true, CoveragePct: 100, Hours: []analysis.WeatherHour{{At: day.Add(12 * time.Hour), IrradianceWM2: 1_000}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	low, medium, high := evaluate(6), evaluate(7), evaluate(30)
	if low.Confidence != domain.ConfidenceLow || medium.Confidence != domain.ConfidenceMedium || high.Confidence != domain.ConfidenceHigh {
		t.Fatalf("confidence progression: %s -> %s -> %s; high evidence=%#v", low.Confidence, medium.Confidence, high.Confidence, high.Evidence)
	}

	db, err := storage.Open(context.Background(), t.TempDir()+"/acceptance.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	engine, err := alerts.NewEngine(storage.NewAlertRepository(db), alerts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	at := base.AddDate(0, 0, 35).Add(12 * time.Hour)
	low.Ratio, low.Qualifying = .2, false
	transitions, err := engine.Evaluate(context.Background(), alerts.Input{At: at, WeatherAvailable: true, AnalysisDay: "2026-07-06", Analysis: &low})
	if err != nil || len(transitions) != 0 {
		t.Fatalf("single low sample: transitions=%v err=%v", transitions, err)
	}
	transitions, err = engine.Evaluate(context.Background(), alerts.Input{At: at.Add(time.Minute), TelemetryObserved: true, TelemetryFresh: true,
		LastTelemetryAt: at.Add(time.Minute), PV2Fault: true, PV2Active: false, GridVoltageV: 230, GridFrequencyHz: 60})
	if err != nil || hasRule(transitions, alerts.RuleInverterFault) {
		t.Fatalf("inactive PV2 transition=%v err=%v", transitions, err)
	}
	under := medium
	under.Ratio, under.Qualifying = .5, true
	for index := range 3 {
		transitions, err = engine.Evaluate(context.Background(), alerts.Input{At: at.AddDate(0, 0, index+1), WeatherAvailable: true,
			AnalysisDay: base.AddDate(0, 0, 36+index).Format("2006-01-02"), Analysis: &under})
		if err != nil {
			t.Fatal(err)
		}
		if (index < 2 && hasRule(transitions, alerts.RulePersistentUnderproduction)) || (index == 2 && !hasRule(transitions, alerts.RulePersistentUnderproduction)) {
			t.Fatalf("underproduction day %d transitions=%v", index+1, transitions)
		}
	}
	recovered := medium
	recovered.Ratio, recovered.Qualifying = .9, true
	for index := range 2 {
		transitions, err = engine.Evaluate(context.Background(), alerts.Input{At: at.AddDate(0, 0, index+4), WeatherAvailable: true,
			AnalysisDay: base.AddDate(0, 0, 39+index).Format("2006-01-02"), Analysis: &recovered})
		if err != nil {
			t.Fatal(err)
		}
		if (index == 0 && hasRule(transitions, alerts.RulePersistentUnderproduction)) || (index == 1 && !hasRule(transitions, alerts.RulePersistentUnderproduction)) {
			t.Fatalf("recovery day %d transitions=%v", index+1, transitions)
		}
	}
	transitions, err = engine.Evaluate(context.Background(), alerts.Input{At: at.AddDate(0, 0, 6), TelemetryObserved: true, TelemetryFresh: true,
		LastTelemetryAt: at.AddDate(0, 0, 6), SolarElevationDeg: 45, IrradianceWM2: 800, TelemetryCoveragePct: 100, WeatherAvailable: false,
		GridVoltageV: 230, GridFrequencyHz: 60})
	if err != nil || hasRule(transitions, alerts.RuleZeroSunnyGeneration) {
		t.Fatalf("weather outage transition=%v err=%v", transitions, err)
	}
}

func hasRule(transitions []alerts.Transition, rule string) bool {
	for _, transition := range transitions {
		if transition.Rule == rule {
			return true
		}
	}
	return false
}
