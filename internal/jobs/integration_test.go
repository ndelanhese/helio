package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/alerts"
	"github.com/ndelanhese/helio/internal/analysis"
	"github.com/ndelanhese/helio/internal/api"
	"github.com/ndelanhese/helio/internal/auth"
	"github.com/ndelanhese/helio/internal/collector"
	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/storage"
	"github.com/ndelanhese/helio/internal/weather"
)

type replayProfile struct {
	actualWh float64
	powerW   float64
	coverage float64
}

type replayRepository struct {
	profiles map[string]replayProfile
	daily    []domain.AggregatePoint
	hourly   []domain.AggregatePoint
}

func (r *replayRepository) profile(at time.Time) replayProfile {
	return r.profiles[at.UTC().Format("2006-01-02")]
}
func (r *replayRepository) AggregateHour(_ context.Context, from, _ time.Time) (domain.HourlySummary, error) {
	profile := r.profile(from)
	energy, productive := 0.0, 0
	if from.UTC().Hour() >= 6 && from.UTC().Hour() < 18 {
		energy, productive = profile.powerW, 60
	}
	r.hourly = append(r.hourly, domain.AggregatePoint{At: from.UTC(), EnergyWh: energy, PeakPowerW: profile.powerW, CoveragePct: profile.coverage, ProductiveMinutes: productive})
	return domain.HourlySummary{Hour: from.UTC().Format(time.RFC3339), EnergyWh: energy, PeakPowerW: profile.powerW, CoveragePct: profile.coverage, ProductiveMinutes: productive}, nil
}
func (r *replayRepository) AggregateDay(_ context.Context, from, _ time.Time) (domain.DailySummary, error) {
	profile := r.profile(from)
	point := domain.AggregatePoint{At: from.UTC(), EnergyWh: profile.actualWh, PeakPowerW: profile.powerW, CoveragePct: profile.coverage, ProductiveMinutes: 720}
	r.daily = append(r.daily, point)
	return domain.DailySummary{Day: from.UTC().Format("2006-01-02"), EnergyWh: point.EnergyWh, PeakPowerW: point.PeakPowerW, CoveragePct: point.CoveragePct, ProductiveMinutes: point.ProductiveMinutes}, nil
}
func (r *replayRepository) AggregateMonth(_ context.Context, at time.Time) (domain.MonthlySummary, error) {
	return domain.MonthlySummary{Month: at.UTC().Format("2006-01")}, nil
}
func (r *replayRepository) PruneBefore(context.Context, time.Time) (int64, error) { return 0, nil }
func (r *replayRepository) DailyHistory(_ context.Context, from, to time.Time) ([]domain.AggregatePoint, error) {
	return replayPoints(r.daily, from, to), nil
}
func (r *replayRepository) HourlyHistory(_ context.Context, from, to time.Time) ([]domain.AggregatePoint, error) {
	return replayPoints(r.hourly, from, to), nil
}
func replayPoints(points []domain.AggregatePoint, from, to time.Time) []domain.AggregatePoint {
	result := make([]domain.AggregatePoint, 0, len(points))
	for _, point := range points {
		if !point.At.Before(from) && point.At.Before(to) {
			result = append(result, point)
		}
	}
	return result
}

type replayWeather struct {
	modes map[string]string
	seen  map[string]bool
}

func (w *replayWeather) Get(_ context.Context, request weather.Request) weather.Result {
	day := request.Start.UTC().Format("2006-01-02")
	mode := w.modes[day]
	if mode == "" {
		mode = "clear"
	}
	w.seen[mode] = true
	if mode == "missing" || mode == "provider_outage" {
		return weather.Result{ErrorClass: mode}
	}
	irradiance := 1_000.0
	if mode == "cloudy" {
		irradiance = 300
	}
	hours := make([]weather.Hour, 0, 24)
	for hour := range 24 {
		hours = append(hours, weather.Hour{Time: request.Start.Add(time.Duration(hour) * time.Hour), IrradianceWM2: irradiance, FetchedAt: request.Start.Add(12 * time.Hour)})
	}
	return weather.Result{Available: true, FetchedAt: request.Start.Add(12 * time.Hour), Hours: hours}
}

type fakeAnalysisData struct {
	daily, hourly       []domain.AggregatePoint
	dailyErr, hourlyErr error
}

type panicAnalysisData struct{}

func (panicAnalysisData) DailyHistory(context.Context, time.Time, time.Time) ([]domain.AggregatePoint, error) {
	panic("analysis data panic")
}
func (panicAnalysisData) HourlyHistory(context.Context, time.Time, time.Time) ([]domain.AggregatePoint, error) {
	return nil, nil
}

func (f fakeAnalysisData) DailyHistory(context.Context, time.Time, time.Time) ([]domain.AggregatePoint, error) {
	return append([]domain.AggregatePoint(nil), f.daily...), f.dailyErr
}
func (f fakeAnalysisData) HourlyHistory(context.Context, time.Time, time.Time) ([]domain.AggregatePoint, error) {
	return append([]domain.AggregatePoint(nil), f.hourly...), f.hourlyErr
}

type fakeAnalysisWriter struct {
	mu     sync.Mutex
	values []domain.DailyAnalysis
	order  *[]string
	panic  bool
	err    error
}

func (f *fakeAnalysisWriter) Save(_ context.Context, value domain.DailyAnalysis) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.panic {
		panic("analysis writer panic")
	}
	if f.err != nil {
		return f.err
	}
	f.values = append(f.values, value)
	if f.order != nil {
		*f.order = append(*f.order, "analysis")
	}
	return nil
}

type panicOnceWeather struct {
	mu    sync.Mutex
	calls int
	now   time.Time
}

func (f *panicOnceWeather) Get(context.Context, weather.Request) weather.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls == 1 {
		panic("weather provider panic")
	}
	return weather.Result{Available: true, FetchedAt: f.now}
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
	err    error
	panicN int
}

func (f *fakeAlertEvaluator) Evaluate(_ context.Context, input alerts.Input) ([]alerts.Transition, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, input)
	if f.panicN > 0 && len(f.inputs) == f.panicN {
		panic("evaluator panic")
	}
	if f.order != nil && input.Analysis != nil {
		*f.order = append(*f.order, "daily-alert")
	}
	return nil, f.err
}

type blockingWeather struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

type contextIgnoringWeather struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (f *contextIgnoringWeather) Get(context.Context, weather.Request) weather.Result {
	f.once.Do(func() { close(f.started) })
	<-f.release
	return weather.Result{}
}

type readyEventSource struct {
	events chan collector.Event
	ready  chan struct{}
	once   sync.Once
}

func newReadyEventSource() *readyEventSource {
	return &readyEventSource{events: make(chan collector.Event, 32), ready: make(chan struct{})}
}

func (s *readyEventSource) Subscribe() (<-chan collector.Event, func()) {
	s.once.Do(func() { close(s.ready) })
	return s.events, func() {}
}

func (s *readyEventSource) SubscribeBuffered(int) (<-chan collector.Event, func()) {
	return s.Subscribe()
}

func (f *blockingWeather) Get(ctx context.Context, _ weather.Request) weather.Result {
	f.once.Do(func() { close(f.started) })
	select {
	case <-ctx.Done():
		return weather.Result{ErrorClass: "cancelled"}
	case <-f.release:
		return weather.Result{Available: true, FetchedAt: time.Now().UTC()}
	}
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

func TestRunnerEvaluatesBurstWhileWeatherRefreshIsBlocked(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	weatherService := &blockingWeather{started: make(chan struct{}), release: make(chan struct{})}
	alertEngine := &fakeAlertEvaluator{}
	events := newReadyEventSource()
	runner := New(&fakeRepository{}, func(context.Context) (domain.Settings, error) {
		return domain.Settings{Timezone: "UTC", Latitude: -23.5, Longitude: -46.6, ActiveMPPT: []int{1}}, nil
	}, WithIntegration(Integration{Weather: weatherService, Alerts: alertEngine, Events: events}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runner.runIntegration(ctx); close(done) }()
	select {
	case <-weatherService.started:
	case <-time.After(time.Second):
		t.Fatal("weather refresh did not start")
	}
	<-events.ready
	for index := range 20 {
		faultCodes := []uint16(nil)
		if index == 9 {
			faultCodes = []uint16{42}
		}
		events.events <- collector.Event{Kind: "snapshot", Snapshot: &domain.TelemetrySnapshot{
			ObservedAt: now.Add(time.Duration(index) * time.Second), ACPowerW: float64(index), FaultCodes: faultCodes,
			Grid: domain.Grid{VoltageV: 230, FrequencyHz: 60},
		}, State: collector.State{LastSuccess: now.Add(time.Duration(index) * time.Second)}}
	}
	eventually(t, func() bool {
		alertEngine.mu.Lock()
		defer alertEngine.mu.Unlock()
		return len(alertEngine.inputs) == 20
	})
	alertEngine.mu.Lock()
	if !alertEngine.inputs[9].PV1Fault {
		t.Fatal("transient fault event was not evaluated")
	}
	alertEngine.mu.Unlock()
	cancel()
	close(weatherService.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("integration workers did not stop")
	}
}

func TestRunnerRecordsEvaluatorErrorsAndContinuesConsuming(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	alertEngine := &fakeAlertEvaluator{err: context.DeadlineExceeded}
	events := newReadyEventSource()
	runner := New(&fakeRepository{}, func(context.Context) (domain.Settings, error) {
		return domain.Settings{Timezone: "UTC"}, nil
	}, WithIntegration(Integration{Alerts: alertEngine, Events: events}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runner.runIntegration(ctx); close(done) }()
	<-events.ready
	for index := range 2 {
		events.events <- collector.Event{Kind: "snapshot", Snapshot: &domain.TelemetrySnapshot{ObservedAt: now.Add(time.Duration(index) * time.Second)}}
	}
	eventually(t, func() bool {
		alertEngine.mu.Lock()
		defer alertEngine.mu.Unlock()
		return len(alertEngine.inputs) == 2
	})
	status := runner.IntegrationStatus().Alerts
	if status.State != "unavailable" || status.ErrorClass != "evaluation" {
		t.Fatalf("alert status=%#v", status)
	}
	cancel()
	<-done
}

func TestRunnerClassifiesAlertSettingsFailureWithoutDroppingEvaluation(t *testing.T) {
	alertEngine := &fakeAlertEvaluator{}
	runner := New(&fakeRepository{}, func(context.Context) (domain.Settings, error) {
		return domain.Settings{}, context.DeadlineExceeded
	}, WithIntegration(Integration{Alerts: alertEngine}))
	runner.evaluateCollectorEventSafely(context.Background(), collector.Event{Kind: "state"})
	alertEngine.mu.Lock()
	inputs := len(alertEngine.inputs)
	alertEngine.mu.Unlock()
	status := runner.IntegrationStatus().Alerts
	if inputs != 1 || status.State != "unavailable" || status.ErrorClass != "settings" {
		t.Fatalf("inputs=%d status=%#v", inputs, status)
	}
}

func TestRunnerContainsEvaluatorPanicAndContinuesConsuming(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	alertEngine := &fakeAlertEvaluator{panicN: 1}
	events := newReadyEventSource()
	runner := New(&fakeRepository{}, func(context.Context) (domain.Settings, error) {
		return domain.Settings{Timezone: "UTC"}, nil
	}, WithIntegration(Integration{Alerts: alertEngine, Events: events}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runner.runIntegration(ctx); close(done) }()
	<-events.ready
	events.events <- collector.Event{Kind: "snapshot", Snapshot: &domain.TelemetrySnapshot{ObservedAt: now}}
	events.events <- collector.Event{Kind: "snapshot", Snapshot: &domain.TelemetrySnapshot{ObservedAt: now.Add(time.Second)}}
	eventually(t, func() bool {
		alertEngine.mu.Lock()
		defer alertEngine.mu.Unlock()
		return len(alertEngine.inputs) == 2
	})
	status := runner.IntegrationStatus().Alerts
	if status.State != "available" || status.ErrorClass != "" {
		t.Fatalf("alert status after recovery=%#v", status)
	}
	cancel()
	<-done
}

func TestRunnerContainsWeatherPanicAndRetriesWithBackoff(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	clock := newFakeClock(now)
	weatherService := &panicOnceWeather{now: now}
	runner := New(&fakeRepository{}, func(context.Context) (domain.Settings, error) {
		return domain.Settings{Timezone: "UTC"}, nil
	}, WithClock(clock), WithIntegration(Integration{Weather: weatherService}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runner.runWeatherIntegration(ctx); close(done) }()
	eventually(t, func() bool { return runner.WeatherStatus().ErrorClass == "panic" })
	if wait := clock.firstWait(t); wait != retryDelay {
		t.Fatalf("panic retry delay=%v", wait)
	}
	clock.advance(now.Add(retryDelay))
	eventually(t, func() bool { return runner.WeatherStatus().State == "available" })
	cancel()
	<-done
}

func TestRunnerShutdownCapsContextIgnoringWeatherWorker(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	provider := &contextIgnoringWeather{started: make(chan struct{}), release: make(chan struct{})}
	runner := New(&fakeRepository{}, func(context.Context) (domain.Settings, error) {
		return domain.Settings{Timezone: "UTC"}, nil
	}, WithClock(newFakeClock(now)), WithIntegration(Integration{Weather: provider}), WithShutdownTimeout(20*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	<-provider.started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrShutdownTimeout) {
			t.Fatalf("shutdown error=%v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("weather worker exceeded shutdown cap")
	}
	close(provider.release)
}

func TestRunnerContainsAnalysisDependencyPanicAndClassifiesIt(t *testing.T) {
	location := time.UTC
	dayStart := time.Date(2026, 7, 14, 0, 0, 0, 0, location)
	runner := New(&fakeRepository{}, func(context.Context) (domain.Settings, error) { return domain.Settings{}, nil }, WithIntegration(Integration{
		AnalysisData: panicAnalysisData{}, AnalysisWriter: &fakeAnalysisWriter{},
	}))
	err := runner.analyzeDay(context.Background(), dayStart.AddDate(0, 0, 1), domain.Settings{Timezone: "UTC", PanelCount: 1, PanelWattage: 100}, location,
		dayStart, dayStart.AddDate(0, 0, 1), domain.DailySummary{})
	if err == nil {
		t.Fatal("expected contained analysis panic")
	}
	status := runner.IntegrationStatus().Analysis
	if status.State != "unavailable" || status.ErrorClass != "panic" {
		t.Fatalf("analysis status=%#v", status)
	}
}

func TestDailyPipelineReportsTruthfulStageAndComponent(t *testing.T) {
	day := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	baseSettings := domain.Settings{Timezone: "UTC", Latitude: -23.5, Longitude: -46.6, PanelCount: 1, PanelWattage: 100, ActiveMPPT: []int{1}}
	for _, test := range []struct {
		name          string
		settings      domain.Settings
		integration   Integration
		wantJob       string
		wantAnalysis  string
		wantAlerts    string
		analysisState string
	}{
		{"baseline", baseSettings, Integration{AnalysisData: fakeAnalysisData{daily: []domain.AggregatePoint{{CoveragePct: 95}}}, AnalysisWriter: &fakeAnalysisWriter{}}, "baseline", "baseline", "", "unavailable"},
		{"daylight", func() domain.Settings { value := baseSettings; value.Latitude = 100; return value }(), Integration{AnalysisData: fakeAnalysisData{}, AnalysisWriter: &fakeAnalysisWriter{}}, "daylight", "daylight", "", "unavailable"},
		{"weather", baseSettings, Integration{AnalysisData: fakeAnalysisData{}, AnalysisWriter: &fakeAnalysisWriter{}, Weather: &panicOnceWeather{}}, "weather", "weather", "", "unavailable"},
		{"analysis storage", baseSettings, Integration{AnalysisData: fakeAnalysisData{}, AnalysisWriter: &fakeAnalysisWriter{err: errors.New("disk")}}, "analysis_storage", "storage", "", "unavailable"},
		{"daily alert", baseSettings, Integration{AnalysisData: fakeAnalysisData{}, AnalysisWriter: &fakeAnalysisWriter{}, Alerts: &fakeAlertEvaluator{err: errors.New("alerts")}}, "daily_alert", "", "daily_evaluation", "available"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := New(&fakeRepository{}, func(context.Context) (domain.Settings, error) { return test.settings, nil }, WithIntegration(test.integration))
			err := runner.analyzeDay(context.Background(), day.AddDate(0, 0, 1), test.settings, time.UTC, day, day.AddDate(0, 0, 1), domain.DailySummary{CoveragePct: 95})
			if err == nil {
				t.Fatal("expected staged error")
			}
			runner.recordResult(day, err)
			if got := runner.Status().ErrorClass; got != test.wantJob {
				t.Fatalf("jobs error class=%q want=%q err=%v", got, test.wantJob, err)
			}
			status := runner.IntegrationStatus()
			if status.Analysis.State != test.analysisState || status.Analysis.ErrorClass != test.wantAnalysis || status.Alerts.ErrorClass != test.wantAlerts {
				t.Fatalf("integration status=%#v", status)
			}
		})
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

func Test35DayCrossStackReplayPersistsAndServesHonestAnalysis(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	settings := domain.Settings{LoggerHost: "192.168.1.50", LoggerSerial: "123", LoggerPort: 8899, ModbusSlave: 1,
		PanelCount: 7, PanelWattage: 610, ActiveMPPT: []int{1}, Latitude: -23.5, Longitude: -46.6,
		Timezone: "UTC", Currency: "BRL", TariffMinorPerKWh: 95, RetentionDays: 730}
	repository := &replayRepository{profiles: map[string]replayProfile{}}
	weatherService := &replayWeather{modes: map[string]string{}, seen: map[string]bool{}}
	for index := range 35 {
		day := base.AddDate(0, 0, index)
		profile := replayProfile{actualWh: 24_000, powerW: 2_000, coverage: 95}
		if index >= 30 && index <= 32 {
			profile.actualWh = 6_000
		}
		repository.profiles[day.Format("2006-01-02")] = profile
	}
	weatherService.modes[base.AddDate(0, 0, 5).Format("2006-01-02")] = "cloudy"
	weatherService.modes[base.AddDate(0, 0, 10).Format("2006-01-02")] = "missing"
	weatherService.modes[base.AddDate(0, 0, 11).Format("2006-01-02")] = "provider_outage"

	db, err := storage.Open(ctx, t.TempDir()+"/replay.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	analysisRepository := storage.NewAnalysisRepository(db)
	alertRepository := storage.NewAlertRepository(db)
	alertEngine, err := alerts.NewEngine(alertRepository, alerts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	clock := newFakeClock(base)
	runner := New(repository, func(context.Context) (domain.Settings, error) { return settings, nil }, WithClock(clock), WithIntegration(Integration{
		AnalysisData: repository, AnalysisWriter: analysisRepository, Weather: weatherService, Alerts: alertEngine,
	}))
	for index := range 35 {
		day := base.AddDate(0, 0, index)
		now := day.AddDate(0, 0, 1).Add(5 * time.Minute)
		clock.mu.Lock()
		clock.now = now
		clock.mu.Unlock()
		if err := runner.runOnce(ctx, now, settings, time.UTC); err != nil {
			t.Fatalf("replay day %d: %v", index, err)
		}
		if index == 11 {
			status := runner.WeatherStatus()
			if status.State != "unavailable" || status.ErrorClass != "provider_outage" {
				t.Fatalf("outage health=%#v", status)
			}
		}
	}
	for _, check := range []struct {
		index int
		want  domain.Confidence
	}{{6, domain.ConfidenceLow}, {7, domain.ConfidenceMedium}, {30, domain.ConfidenceHigh}} {
		result, found, err := analysisRepository.Load(ctx, base.AddDate(0, 0, check.index).Format("2006-01-02"))
		if err != nil || !found || result.Confidence != check.want {
			t.Fatalf("confidence day %d: found=%v confidence=%s evidence=%v err=%v", check.index, found, result.Confidence, result.Evidence, err)
		}
	}
	for _, mode := range []string{"clear", "cloudy", "missing", "provider_outage"} {
		if !weatherService.seen[mode] {
			t.Fatalf("weather mode %q was not replayed", mode)
		}
	}

	at := base.AddDate(0, 0, 36).Add(12 * time.Hour)
	for index := range 3 {
		clock.mu.Lock()
		clock.now = at.Add(time.Duration(index) * time.Second)
		clock.mu.Unlock()
		runner.evaluateCollectorEventSafely(ctx, collector.Event{Kind: "state"})
	}
	clock.mu.Lock()
	clock.now = at.Add(3 * time.Second)
	clock.mu.Unlock()
	runner.evaluateCollectorEventSafely(ctx, collector.Event{Kind: "snapshot", Snapshot: &domain.TelemetrySnapshot{ObservedAt: at.Add(3 * time.Second), ACPowerW: 100, Grid: domain.Grid{VoltageV: 230, FrequencyHz: 60}}})
	quiet, err := alertEngine.Evaluate(ctx, alerts.Input{At: at.Add(4 * time.Second), TelemetryObserved: true, TelemetryFresh: true, PV2Active: false, PV2Fault: true, GridVoltageV: 230, GridFrequencyHz: 60})
	if err != nil || hasRule(quiet, alerts.RuleInverterFault) {
		t.Fatalf("inactive PV2 must stay silent: transitions=%v err=%v", quiet, err)
	}
	clock.mu.Lock()
	clock.now = at.Add(5 * time.Second)
	clock.mu.Unlock()
	runner.evaluateCollectorEventSafely(ctx, collector.Event{Kind: "snapshot", Snapshot: &domain.TelemetrySnapshot{ObservedAt: at.Add(5 * time.Second), Status: "fault", FaultCodes: []uint16{42}, Grid: domain.Grid{VoltageV: 230, FrequencyHz: 60}}})

	open, err := alertRepository.List(ctx, "open")
	if err != nil || len(open) != 1 || open[0].Rule != alerts.RuleInverterFault {
		t.Fatalf("open alerts=%v err=%v", open, err)
	}
	resolved, err := alertRepository.List(ctx, "resolved")
	if err != nil || len(resolved) != 2 || resolved[0].Rule != alerts.RuleLoggerOffline || resolved[1].Rule != alerts.RulePersistentUnderproduction {
		t.Fatalf("resolved newest-first=%v err=%v", resolved, err)
	}
	if resolved[0].Evidence.Values["failed_polls"] != 0 {
		t.Fatalf("logger recovery evidence=%v", resolved[0].Evidence)
	}

	manager := auth.NewManager(db)
	credentials, err := manager.BootstrapWithSettings(ctx, "admin", "correct horse battery staple", settings, false)
	if err != nil {
		t.Fatal(err)
	}
	handler := api.New(api.Dependencies{Auth: manager, Store: db, Insights: analysisRepository, Alerts: alertRepository, Summaries: repository})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/insights?day="+base.AddDate(0, 0, 34).Format("2006-01-02"), nil)
	request.AddCookie(manager.SessionCookie(credentials.Token))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("insights API: %d %s", response.Code, response.Body.String())
	}
	var insight struct {
		Confidence domain.Confidence        `json:"confidence"`
		Evidence   []struct{ Label string } `json:"evidence"`
		Trends     struct {
			PeakPower struct {
				WindowDays  int     `json:"windowDays"`
				CoveragePct float64 `json:"coveragePct"`
			} `json:"peakPower"`
		} `json:"trends"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &insight); err != nil {
		t.Fatal(err)
	}
	if insight.Confidence != domain.ConfidenceHigh || len(insight.Evidence) == 0 || insight.Evidence[0].Label != "Dias qualificáveis no histórico" || insight.Trends.PeakPower.WindowDays != 7 || insight.Trends.PeakPower.CoveragePct != 95 {
		t.Fatalf("insights API contract=%#v", insight)
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
