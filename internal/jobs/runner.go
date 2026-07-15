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
	shutdownLimit        = 10 * time.Second
	retryDelay           = time.Minute
)

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

type Integration struct {
	AnalysisData   AnalysisData
	AnalysisWriter AnalysisWriter
	Weather        WeatherService
	Alerts         AlertEvaluator
	Events         EventSource
}

type WeatherStatus struct {
	State      string
	UpdatedAt  time.Time
	FetchedAt  time.Time
	ErrorClass string
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

	mu            sync.RWMutex
	status        Status
	weatherStatus WeatherStatus
	weatherResult weather.Result
}

func New(repository Repository, settings Settings, options ...Option) *Runner {
	runner := &Runner{repository: repository, settings: settings, clock: systemClock{}}
	for _, option := range options {
		option(runner)
	}
	runner.status = Status{State: "idle", UpdatedAt: runner.clock.Now().UTC()}
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

func (r *Runner) Run(ctx context.Context) error {
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
		go func() { r.runIntegration(integrationContext); close(integrationDone) }()
		defer func() { integrationCancel(); <-integrationDone }()
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
				runErr, stopped := r.runCurrent(ctx, now, settings, location)
				if stopped {
					return nil
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

func (r *Runner) runCurrent(ctx context.Context, now time.Time, settings domain.Settings, location *time.Location) (error, bool) {
	jobContext, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.runOnce(jobContext, now, settings, location) }()
	select {
	case err := <-done:
		cancel()
		r.recordResult(now, err)
		return err, false
	case <-ctx.Done():
		select {
		case err := <-done:
			cancel()
			r.recordResult(now, err)
			return err, true
		case <-r.clock.After(shutdownLimit):
			cancel()
			err := <-done
			r.setStatus("stopped", r.clock.Now(), "shutdown_timeout")
			return err, true
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
			return fmt.Errorf("aggregate hour: %w", err)
		}
		start = end
	}
	daily, err := r.repository.AggregateDay(ctx, dayStart, dayEnd)
	if err != nil {
		return fmt.Errorf("aggregate day: %w", err)
	}
	if r.integration.AnalysisData != nil && r.integration.AnalysisWriter != nil {
		if err := r.analyzeDay(ctx, now, settings, location, dayStart, dayEnd, daily); err != nil {
			return err
		}
	}
	if _, err := r.repository.AggregateMonth(ctx, dayStart); err != nil {
		return fmt.Errorf("aggregate month: %w", err)
	}
	retention := settings.RetentionDays
	if retention == 0 {
		retention = defaultRetentionDays
	}
	if _, err := r.repository.PruneBefore(ctx, now.AddDate(0, 0, -retention)); err != nil {
		return fmt.Errorf("prune telemetry: %w", err)
	}
	return nil
}

func (r *Runner) analyzeDay(ctx context.Context, now time.Time, settings domain.Settings, location *time.Location, dayStart, dayEnd time.Time, daily domain.DailySummary) error {
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
	baseline, err := analysis.BuildBaseline(training)
	if err != nil {
		return fmt.Errorf("build production baseline: %w", err)
	}
	daylight, err := daylightHours(dayStart, settings.Latitude, settings.Longitude, location)
	if err != nil {
		return fmt.Errorf("calculate analysis daylight: %w", err)
	}
	weatherResult := weather.Result{}
	if r.integration.Weather != nil {
		weatherResult = r.integration.Weather.Get(ctx, weather.Request{Latitude: settings.Latitude, Longitude: settings.Longitude, Start: dayStart.UTC(), End: dayEnd.UTC()})
		r.setWeatherResult(weatherResult, r.clock.Now())
	}
	weatherContext, err := analysis.WeatherContextFromResult(weatherResult, settings.Timezone, daylight)
	if err != nil {
		return fmt.Errorf("adapt analysis weather: %w", err)
	}
	result, err := analysis.Evaluate(analysis.Input{Timezone: settings.Timezone, Day: dayStart, Baseline: baseline, InstalledWatts: installed,
		ActualWh: daily.EnergyWh, TelemetryCoveragePct: daily.CoveragePct, Daylight: daylight, Weather: weatherContext})
	if err != nil {
		return fmt.Errorf("evaluate production: %w", err)
	}
	persisted := domain.DailyAnalysis{Day: dayStart.Format("2006-01-02"), ExpectedWh: result.ExpectedWh, ActualWh: result.ActualWh,
		Ratio: result.Ratio, Confidence: result.Confidence, Evidence: result.Evidence, Qualifying: result.Qualifying, AnalyzedAt: now.UTC()}
	if err := r.integration.AnalysisWriter.Save(ctx, persisted); err != nil {
		return fmt.Errorf("save daily analysis: %w", err)
	}
	if r.integration.Alerts != nil {
		if _, err := r.integration.Alerts.Evaluate(ctx, alerts.Input{At: now.UTC(), WeatherAvailable: weatherResult.Available,
			TelemetryCoveragePct: daily.CoveragePct, AnalysisDay: persisted.Day, Analysis: &result}); err != nil {
			return fmt.Errorf("evaluate daily alerts: %w", err)
		}
	}
	return nil
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
	var events <-chan collector.Event
	stopEvents := func() {}
	if r.integration.Events != nil {
		events, stopEvents = r.integration.Events.Subscribe()
	}
	defer stopEvents()
	refresh := func() {
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
	timer := r.clock.After(time.Hour)
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			r.evaluateCollectorEvent(ctx, event)
		case <-timer:
			refresh()
			timer = r.clock.After(time.Hour)
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
	r.weatherStatus = WeatherStatus{State: state, UpdatedAt: at.UTC(), FetchedAt: result.FetchedAt.UTC(), ErrorClass: result.ErrorClass}
	r.mu.Unlock()
}

func (r *Runner) evaluateCollectorEvent(ctx context.Context, event collector.Event) {
	if r.integration.Alerts == nil {
		return
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
	settings, err := r.settings(ctx)
	if err == nil {
		input.SolarElevationDeg = solar.Elevation(input.At, settings.Latitude, settings.Longitude)
	}
	for _, hour := range weatherResult.Hours {
		if hour.Time.UTC().Truncate(time.Hour).Equal(input.At.UTC().Truncate(time.Hour)) {
			input.IrradianceWM2 = hour.IrradianceWM2
			break
		}
	}
	_, _ = r.integration.Alerts.Evaluate(ctx, input)
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
		r.setStatus("degraded", at, "storage")
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
