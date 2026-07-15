package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
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

type Option func(*Runner)

func WithClock(clock Clock) Option {
	return func(r *Runner) {
		if clock != nil {
			r.clock = clock
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
	repository Repository
	settings   Settings
	clock      Clock

	mu     sync.RWMutex
	status Status
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

func (r *Runner) Run(ctx context.Context) error {
	if r.repository == nil || r.settings == nil || r.clock == nil {
		r.setStatus("degraded", time.Now(), "configuration")
		return errors.New("jobs: repository, settings, and clock are required")
	}
	r.setStatus("running", r.clock.Now(), "")

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
	if _, err := r.repository.AggregateDay(ctx, dayStart, dayEnd); err != nil {
		return fmt.Errorf("aggregate day: %w", err)
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
