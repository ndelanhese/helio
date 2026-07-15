package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ndelanhese/helio/internal/alerts"
	"github.com/ndelanhese/helio/internal/analysis"
	"github.com/ndelanhese/helio/internal/collector"
	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/solar"
	"github.com/ndelanhese/helio/internal/weather"
)

const (
	defaultRetentionDays = 730
	defaultShutdownLimit = 10 * time.Second
	retryDelay           = time.Minute
	alertEventBurst      = 256
)

var errAlertSettings = errors.New("alert settings unavailable")
var ErrShutdownTimeout = errors.New("jobs shutdown timeout")

type jobStageError struct {
	class string
	err   error
}

func (e *jobStageError) Error() string { return e.err.Error() }
func (e *jobStageError) Unwrap() error { return e.err }

func staged(class string, err error) error {
	if err == nil {
		return nil
	}
	var existing *jobStageError
	if errors.As(err, &existing) {
		return err
	}
	return &jobStageError{class: class, err: err}
}

type Repository interface {
	AggregateHour(context.Context, time.Time, time.Time) (domain.HourlySummary, error)
	AggregateDay(context.Context, time.Time, time.Time) (domain.DailySummary, error)
	AggregateMonth(context.Context, time.Time) (domain.MonthlySummary, error)
	PruneBefore(context.Context, time.Time) (int64, error)
}

type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time                                { return time.Now() }
func (systemClock) After(duration time.Duration) <-chan time.Time { return time.After(duration) }

type shutdownBudget struct {
	once    sync.Once
	timeout time.Duration
	expired chan struct{}
	timer   *time.Timer
}

func (b *shutdownBudget) done() <-chan struct{} {
	b.once.Do(func() {
		b.timer = time.AfterFunc(b.timeout, func() { close(b.expired) })
	})
	return b.expired
}

func (b *shutdownBudget) stop() {
	if b.timer != nil {
		b.timer.Stop()
	}
}

func (b *shutdownBudget) wait(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
	}
	select {
	case <-done:
		return true
	case <-b.done():
		return false
	}
}

type Settings func(context.Context) (domain.Settings, error)

type AnalysisData interface {
	DailyHistory(context.Context, time.Time, time.Time) ([]domain.AggregatePoint, error)
	HourlyHistory(context.Context, time.Time, time.Time) ([]domain.AggregatePoint, error)
}

type AnalysisWriter interface {
	Save(context.Context, domain.DailyAnalysis) error
}

type WeatherService interface {
	Get(context.Context, weather.Request) weather.Result
}

type AlertEvaluator interface {
	Evaluate(context.Context, alerts.Input) ([]alerts.Transition, error)
}

type EventSource interface {
	Subscribe() (<-chan collector.Event, func())
}

type bufferedEventSource interface {
	SubscribeBuffered(int) (<-chan collector.Event, func())
}

type Integration struct {
	AnalysisData   AnalysisData
	AnalysisWriter AnalysisWriter
	Weather        WeatherService
	Alerts         AlertEvaluator
	Events         EventSource
}

type WeatherStatus struct {
	State         string
	UpdatedAt     time.Time
	FetchedAt     time.Time
	CloudCoverPct *float64
	IrradianceWM2 *float64
	ErrorClass    string
}

type WorkerStatus struct {
	State      string
	UpdatedAt  time.Time
	ErrorClass string
}

type IntegrationStatus struct {
	Alerts   WorkerStatus
	Analysis WorkerStatus
}

type Option func(*Runner)

func WithClock(clock Clock) Option {
	return func(r *Runner) {
		if clock != nil {
			r.clock = clock
		}
	}
}

func WithIntegration(integration Integration) Option {
	return func(r *Runner) { r.integration = integration }
}

func WithShutdownTimeout(timeout time.Duration) Option {
	return func(r *Runner) {
		if timeout > 0 {
			r.shutdownTimeout = timeout
		}
	}
}

type Status struct {
	State      string
	UpdatedAt  time.Time
	LastRun    time.Time
	ErrorClass string
}

type Runner struct {
	repository  Repository
	settings    Settings
	clock       Clock
	integration Integration

	mu                sync.RWMutex
	status            Status
	weatherStatus     WeatherStatus
	weatherResult     weather.Result
	integrationStatus IntegrationStatus
	readinessMu       sync.Mutex
	runReady          chan struct{}
	runPrepared       bool
	shutdownTimeout   time.Duration
}

func New(repository Repository, settings Settings, options ...Option) *Runner {
	runner := &Runner{repository: repository, settings: settings, clock: systemClock{}, shutdownTimeout: defaultShutdownLimit}
	for _, option := range options {
		option(runner)
	}
	runner.status = Status{State: "idle", UpdatedAt: runner.clock.Now().UTC()}
	runner.integrationStatus = IntegrationStatus{
		Alerts:   WorkerStatus{State: "unavailable", UpdatedAt: runner.clock.Now().UTC()},
		Analysis: WorkerStatus{State: "unavailable", UpdatedAt: runner.clock.Now().UTC()},
	}
	return runner
}

func (r *Runner) Status() Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.status
}

func (r *Runner) WeatherStatus() WeatherStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.weatherStatus
}

func (r *Runner) IntegrationStatus() IntegrationStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.integrationStatus
}

// PrepareRun creates the readiness signal for the next Run invocation. The
// signal closes only after the ordered alert subscription is registered (or
// immediately when that integration is disabled).
func (r *Runner) PrepareRun() <-chan struct{} {
	r.readinessMu.Lock()
	defer r.readinessMu.Unlock()
	r.runReady = make(chan struct{})
	r.runPrepared = true
	return r.runReady
}

func (r *Runner) readinessForRun() chan struct{} {
	r.readinessMu.Lock()
	defer r.readinessMu.Unlock()
	if r.runReady == nil || !r.runPrepared {
		r.runReady = make(chan struct{})
	}
	r.runPrepared = false
	return r.runReady
}

func (r *Runner) Run(ctx context.Context) (result error) {
	shutdown := &shutdownBudget{timeout: r.shutdownTimeout, expired: make(chan struct{})}
	defer shutdown.stop()
	ready := r.readinessForRun()
	var readyOnce sync.Once
	markReady := func() { readyOnce.Do(func() { close(ready) }) }
	if r.repository == nil || r.settings == nil || r.clock == nil {
		r.setStatus("degraded", time.Now(), "configuration")
		return errors.New("jobs: repository, settings, and clock are required")
	}
	r.setStatus("running", r.clock.Now(), "")
	var integrationCancel context.CancelFunc
	var integrationDone chan struct{}
	if r.integration.Weather != nil || r.integration.Alerts != nil || r.integration.Events != nil {
		integrationContext, cancel := context.WithCancel(ctx)
		integrationCancel = cancel
		integrationDone = make(chan struct{})
		go func() { r.runIntegrationReady(integrationContext, markReady); close(integrationDone) }()
		defer func() {
			integrationCancel()
			if !shutdown.wait(integrationDone) {
				r.setStatus("stopped", r.clock.Now(), "shutdown_timeout")
				result = errors.Join(result, ErrShutdownTimeout)
			}
		}()
	} else {
		markReady()
	}

	var lastDay string
	for {
		settings, location, err := r.loadSettings(ctx)
		if err != nil {
			r.setStatus("degraded", r.clock.Now(), "settings")
			if !r.wait(ctx, retryDelay) {
				r.markStopped()
				return nil
			}
			continue
		}
		r.setStatus("running", r.clock.Now(), "")
		now := r.clock.Now().In(location)
		scheduled := scheduleFor(now)
		if !now.Before(scheduled) {
			day := now.AddDate(0, 0, -1).Format("2006-01-02")
			if day != lastDay {
				runErr, stopped := r.runCurrent(ctx, now, settings, location, shutdown)
				if stopped {
					return runErr
				}
				if runErr != nil {
					if !r.wait(ctx, retryDelay) {
						r.markStopped()
						return nil
					}
					continue
				}
				lastDay = day
			}
		}

		now = r.clock.Now().In(location)
		next := scheduleFor(now)
		if !next.After(now) {
			next = scheduleFor(now.AddDate(0, 0, 1))
		}
		select {
		case <-ctx.Done():
			r.markStopped()
			return nil
		case <-r.clock.After(next.Sub(now)):
		}
	}
}

func (r *Runner) runCurrent(ctx context.Context, now time.Time, settings domain.Settings, location *time.Location, shutdown *shutdownBudget) (error, bool) {
	jobContext, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.runOnce(jobContext, now, settings, location) }()
	select {
	case err := <-done:
		cancel()
		r.recordResult(now, err)
		return err, false
	case <-ctx.Done():
		cancel()
		select {
		case err := <-done:
			r.recordResult(now, err)
			return nil, true
		case <-shutdown.done():
			r.setStatus("stopped", r.clock.Now(), "shutdown_timeout")
			return ErrShutdownTimeout, true
		}
	}
}

func (r *Runner) wait(ctx context.Context, duration time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-r.clock.After(duration):
		return true
	}
}

func (r *Runner) runOnce(ctx context.Context, now time.Time, settings domain.Settings, location *time.Location) error {
	dayStart := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, location)
	dayEnd := dayStart.AddDate(0, 0, 1)
	for start := dayStart; start.Before(dayEnd); {
		end := start.Add(time.Hour)
		if end.After(dayEnd) {
			end = dayEnd
		}
		if _, err := r.repository.AggregateHour(ctx, start, end); err != nil {
			return staged("aggregate_hour", fmt.Errorf("aggregate hour: %w", err))
		}
		start = end
	}
	daily, err := r.repository.AggregateDay(ctx, dayStart, dayEnd)
	if err != nil {
		return staged("aggregate_day", fmt.Errorf("aggregate day: %w", err))
	}
	if r.integration.AnalysisData != nil && r.integration.AnalysisWriter != nil {
		if err := r.analyzeDay(ctx, now, settings, location, dayStart, dayEnd, daily); err != nil {
			return err
		}
	}
	if _, err := r.repository.AggregateMonth(ctx, dayStart); err != nil {
		return staged("aggregate_month", fmt.Errorf("aggregate month: %w", err))
	}
	retention := settings.RetentionDays
	if retention == 0 {
		retention = defaultRetentionDays
	}
	if _, err := r.repository.PruneBefore(ctx, now.AddDate(0, 0, -retention)); err != nil {
		return staged("retention", fmt.Errorf("prune telemetry: %w", err))
	}
	return nil
}

func (r *Runner) analyzeDay(ctx context.Context, now time.Time, settings domain.Settings, location *time.Location, dayStart, dayEnd time.Time, daily domain.DailySummary) (err error) {
	stage := "load_history"
	analysisComplete := false
	defer func() {
		if recovered := recover(); recovered != nil {
			if stage == "weather" {
				r.setWeatherUnavailable("panic", r.clock.Now())
				r.setAnalysisStatus("unavailable", "weather", r.clock.Now())
			} else if stage == "alerts_evaluation" {
				r.setAnalysisStatus("available", "", r.clock.Now())
				r.setAlertStatus("unavailable", "daily_evaluation", r.clock.Now())
			} else {
				r.setAnalysisStatus("unavailable", "panic", r.clock.Now())
			}
			err = staged(jobClassForAnalysisStage(stage), fmt.Errorf("analysis panic"))
			return
		}
		if err != nil {
			if stage == "alerts_evaluation" && analysisComplete {
				r.setAnalysisStatus("available", "", r.clock.Now())
				r.setAlertStatus("unavailable", "daily_evaluation", r.clock.Now())
			} else {
				r.setAnalysisStatus("unavailable", componentClassForAnalysisStage(stage), r.clock.Now())
			}
			err = staged(jobClassForAnalysisStage(stage), err)
			return
		}
		r.setAnalysisStatus("available", "", r.clock.Now())
	}()
	from := dayStart.AddDate(0, 0, -35)
	days, err := r.integration.AnalysisData.DailyHistory(ctx, from, dayStart)
	if err != nil {
		return fmt.Errorf("load analysis days: %w", err)
	}
	hours, err := r.integration.AnalysisData.HourlyHistory(ctx, from, dayStart)
	if err != nil {
		return fmt.Errorf("load analysis hours: %w", err)
	}
	hoursByDay := make(map[string][]analysis.PowerHour)
	for _, point := range hours {
		key := point.At.In(location).Format("2006-01-02")
		hoursByDay[key] = append(hoursByDay[key], analysis.PowerHour{At: point.At, PowerW: point.EnergyWh})
	}
	installed := analysis.InstalledWatts(settings)
	training := make([]analysis.TrainingDay, 0, len(days))
	for _, point := range days {
		key := point.At.In(location).Format("2006-01-02")
		training = append(training, analysis.TrainingDay{Timezone: settings.Timezone, Day: point.At, CoveragePct: point.CoveragePct, InstalledWatts: installed, Hours: hoursByDay[key]})
	}
	stage = "baseline"
	baseline, err := analysis.BuildBaseline(training)
	if err != nil {
		return fmt.Errorf("build production baseline: %w", err)
	}
	stage = "daylight"
	daylight, err := daylightHours(dayStart, settings.Latitude, settings.Longitude, location)
	if err != nil {
		return fmt.Errorf("calculate analysis daylight: %w", err)
	}
	weatherResult := weather.Result{}
	if r.integration.Weather != nil {
		stage = "weather"
		weatherResult = r.integration.Weather.Get(ctx, weather.Request{Latitude: settings.Latitude, Longitude: settings.Longitude, Start: dayStart.UTC(), End: dayEnd.UTC()})
		r.setWeatherResult(weatherResult, r.clock.Now())
	}
	weatherContext, err := analysis.WeatherContextFromResult(weatherResult, settings.Timezone, daylight)
	if err != nil {
		return fmt.Errorf("adapt analysis weather: %w", err)
	}
	stage = "evaluation"
	result, err := analysis.Evaluate(analysis.Input{Timezone: settings.Timezone, Day: dayStart, Baseline: baseline, InstalledWatts: installed,
		ActualWh: daily.EnergyWh, TelemetryCoveragePct: daily.CoveragePct, Daylight: daylight, Weather: weatherContext})
	if err != nil {
		return fmt.Errorf("evaluate production: %w", err)
	}
	persisted := domain.DailyAnalysis{Day: dayStart.Format("2006-01-02"), ExpectedWh: result.ExpectedWh, ActualWh: result.ActualWh,
		Ratio: result.Ratio, Confidence: result.Confidence, Evidence: result.Evidence, Qualifying: result.Qualifying, AnalyzedAt: now.UTC()}
	stage = "persistence"
	if err := r.integration.AnalysisWriter.Save(ctx, persisted); err != nil {
		return fmt.Errorf("save daily analysis: %w", err)
	}
	analysisComplete = true
	if r.integration.Alerts != nil {
		stage = "alerts_evaluation"
		if _, err := r.integration.Alerts.Evaluate(ctx, alerts.Input{At: now.UTC(), WeatherAvailable: weatherResult.Available,
			TelemetryCoveragePct: daily.CoveragePct, AnalysisDay: persisted.Day, Analysis: &result}); err != nil {
			return fmt.Errorf("evaluate daily alerts: %w", err)
		}
		r.setAlertStatus("available", "", r.clock.Now())
	}
	return nil
}

func jobClassForAnalysisStage(stage string) string {
	switch stage {
	case "load_history":
		return "analysis_history"
	case "baseline":
		return "baseline"
	case "daylight":
		return "daylight"
	case "weather":
		return "weather"
	case "evaluation":
		return "analysis"
	case "persistence":
		return "analysis_storage"
	case "alerts_evaluation":
		return "daily_alert"
	default:
		return "analysis"
	}
}

func componentClassForAnalysisStage(stage string) string {
	switch stage {
	case "load_history":
		return "history"
	case "persistence":
		return "storage"
	case "evaluation":
		return "evaluation"
	default:
		return stage
	}
}

func daylightHours(day time.Time, latitude, longitude float64, location *time.Location) ([]analysis.DaylightHour, error) {
	sunrise, sunset, err := solar.Daylight(day, latitude, longitude, location)
	if err != nil {
		return nil, err
	}
	result := make([]analysis.DaylightHour, 0, 14)
	for start := sunrise.Truncate(time.Hour); start.Before(sunset); start = start.Add(time.Hour) {
		end := start.Add(time.Hour)
		overlapStart := start
		if sunrise.After(overlapStart) {
			overlapStart = sunrise
		}
		overlapEnd := end
		if sunset.Before(overlapEnd) {
			overlapEnd = sunset
		}
		if overlapEnd.After(overlapStart) {
			result = append(result, analysis.DaylightHour{At: start, DurationHours: overlapEnd.Sub(overlapStart).Hours()})
		}
	}
	return result, nil
}

func (r *Runner) runIntegration(ctx context.Context) {
	r.runIntegrationReady(ctx, func() {})
}

func (r *Runner) runIntegrationReady(ctx context.Context, markReady func()) {
	var workers sync.WaitGroup
	if r.integration.Weather != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			r.runWeatherIntegration(ctx)
		}()
	}
	if r.integration.Alerts != nil && r.integration.Events != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			r.runAlertIntegration(ctx, markReady)
		}()
	} else {
		markReady()
	}
	workers.Wait()
}

func (r *Runner) runAlertIntegration(ctx context.Context, markReady func()) {
	var events <-chan collector.Event
	stopEvents := func() {}
	if r.integration.Events != nil {
		if buffered, ok := r.integration.Events.(bufferedEventSource); ok {
			events, stopEvents = buffered.SubscribeBuffered(alertEventBurst)
		} else {
			events, stopEvents = r.integration.Events.Subscribe()
		}
	}
	defer stopEvents()
	markReady()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			r.evaluateCollectorEventSafely(ctx, event)
		}
	}
}

func (r *Runner) runWeatherIntegration(ctx context.Context) {
	nextDelay := time.Hour
	refresh := func() {
		nextDelay = time.Hour
		defer func() {
			if recover() != nil {
				r.setWeatherUnavailable("panic", r.clock.Now())
				nextDelay = retryDelay
			}
		}()
		settings, location, err := r.loadSettings(ctx)
		if err != nil {
			r.setWeatherUnavailable("settings", r.clock.Now())
			return
		}
		now := r.clock.Now().In(location)
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
		if r.integration.Weather == nil {
			r.setWeatherUnavailable("unconfigured", r.clock.Now())
			return
		}
		result := r.integration.Weather.Get(ctx, weather.Request{Latitude: settings.Latitude, Longitude: settings.Longitude, Start: start.UTC(), End: start.AddDate(0, 0, 1).UTC()})
		r.setWeatherResult(result, r.clock.Now())
	}
	refresh()
	timer := r.clock.After(nextDelay)
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer:
			refresh()
			timer = r.clock.After(nextDelay)
		}
	}
}

func (r *Runner) setWeatherUnavailable(class string, at time.Time) {
	r.mu.Lock()
	r.weatherResult = weather.Result{ErrorClass: class}
	r.weatherStatus = WeatherStatus{State: "unavailable", UpdatedAt: at.UTC(), ErrorClass: class}
	r.mu.Unlock()
}

func (r *Runner) setWeatherResult(result weather.Result, at time.Time) {
	state := "unavailable"
	if result.Available {
		state = "available"
	}
	if result.Available && result.Stale {
		state = "stale"
	}
	r.mu.Lock()
	r.weatherResult = result
	status := WeatherStatus{State: state, UpdatedAt: at.UTC(), FetchedAt: result.FetchedAt.UTC(), ErrorClass: result.ErrorClass}
	if hour, ok := currentWeatherHour(result, at); ok {
		cloudCover := hour.CloudCoverPct
		irradiance := hour.IrradianceWM2
		status.CloudCoverPct = &cloudCover
		status.IrradianceWM2 = &irradiance
	}
	r.weatherStatus = status
	r.mu.Unlock()
}

func currentWeatherHour(result weather.Result, at time.Time) (weather.Hour, bool) {
	cutoff := at.UTC().Truncate(time.Hour)
	var current weather.Hour
	found := false
	for _, hour := range result.Hours {
		if hour.Time.UTC().After(cutoff) {
			continue
		}
		if !found || hour.Time.UTC().After(current.Time.UTC()) {
			current = hour
			found = true
		}
	}
	return current, found
}

func (r *Runner) evaluateCollectorEventSafely(ctx context.Context, event collector.Event) {
	defer func() {
		if recover() != nil {
			r.setAlertStatus("unavailable", "panic", r.clock.Now())
		}
	}()
	if err := r.evaluateCollectorEvent(ctx, event); err != nil {
		class := "evaluation"
		if errors.Is(err, errAlertSettings) {
			class = "settings"
		}
		r.setAlertStatus("unavailable", class, r.clock.Now())
		return
	}
	r.setAlertStatus("available", "", r.clock.Now())
}

func (r *Runner) evaluateCollectorEvent(ctx context.Context, event collector.Event) error {
	if r.integration.Alerts == nil {
		return nil
	}
	r.mu.RLock()
	weatherResult := r.weatherResult
	r.mu.RUnlock()
	at := r.clock.Now().UTC()
	input := alerts.Input{At: at, PollObserved: true, LastTelemetryAt: event.State.LastSuccess, WeatherAvailable: weatherResult.Available}
	if event.Kind == "snapshot" && event.Snapshot != nil {
		snapshot := event.Snapshot
		if !snapshot.ObservedAt.IsZero() {
			input.At = snapshot.ObservedAt.UTC()
		}
		input.PollSucceeded, input.TelemetryObserved, input.TelemetryFresh = true, true, !event.State.Stale
		input.LastTelemetryAt, input.ACPowerW = snapshot.ObservedAt, snapshot.ACPowerW
		input.GridVoltageV, input.GridFrequencyHz = snapshot.Grid.VoltageV, snapshot.Grid.FrequencyHz
		input.PV1Fault = snapshot.Status == "fault" || len(snapshot.FaultCodes) > 0
		input.PV2Active = snapshot.PV2.Active
		input.TelemetryCoveragePct = 100
	}
	settings, settingsErr := r.settings(ctx)
	if settingsErr == nil {
		input.SolarElevationDeg = solar.Elevation(input.At, settings.Latitude, settings.Longitude)
	}
	for _, hour := range weatherResult.Hours {
		if hour.Time.UTC().Truncate(time.Hour).Equal(input.At.UTC().Truncate(time.Hour)) {
			input.IrradianceWM2 = hour.IrradianceWM2
			break
		}
	}
	if _, err := r.integration.Alerts.Evaluate(ctx, input); err != nil {
		return err
	}
	if settingsErr != nil {
		return fmt.Errorf("%w: %v", errAlertSettings, settingsErr)
	}
	return nil
}

func (r *Runner) setAlertStatus(state, class string, at time.Time) {
	r.mu.Lock()
	r.integrationStatus.Alerts = WorkerStatus{State: state, UpdatedAt: at.UTC(), ErrorClass: class}
	r.mu.Unlock()
}

func (r *Runner) setAnalysisStatus(state, class string, at time.Time) {
	r.mu.Lock()
	r.integrationStatus.Analysis = WorkerStatus{State: state, UpdatedAt: at.UTC(), ErrorClass: class}
	r.mu.Unlock()
}

func (r *Runner) loadSettings(ctx context.Context) (domain.Settings, *time.Location, error) {
	settings, err := r.settings(ctx)
	if err != nil {
		return domain.Settings{}, nil, fmt.Errorf("load job settings: %w", err)
	}
	location, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return domain.Settings{}, nil, errors.New("load job settings: invalid timezone")
	}
	return settings, location, nil
}

func scheduleFor(at time.Time) time.Time {
	return time.Date(at.Year(), at.Month(), at.Day(), 0, 5, 0, 0, at.Location())
}

func (r *Runner) recordResult(at time.Time, err error) {
	if err != nil {
		class := "storage"
		var stage *jobStageError
		if errors.As(err, &stage) {
			class = stage.class
		}
		r.setStatus("degraded", at, class)
		return
	}
	r.mu.Lock()
	r.status = Status{State: "running", UpdatedAt: at.UTC(), LastRun: at.UTC()}
	r.mu.Unlock()
}

func (r *Runner) setStatus(state string, at time.Time, class string) {
	r.mu.Lock()
	lastRun := r.status.LastRun
	r.status = Status{State: state, UpdatedAt: at.UTC(), LastRun: lastRun, ErrorClass: class}
	r.mu.Unlock()
}

func (r *Runner) markStopped() {
	r.mu.Lock()
	r.status.State = "stopped"
	r.status.UpdatedAt = r.clock.Now().UTC()
	r.mu.Unlock()
}
