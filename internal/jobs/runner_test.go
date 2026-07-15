package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
	wake  chan time.Time
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{now: now, wake: make(chan time.Time, 8)}
}
func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	c.waits = append(c.waits, d)
	c.mu.Unlock()
	return c.wake
}
func (c *fakeClock) advance(to time.Time) { c.mu.Lock(); c.now = to; c.mu.Unlock(); c.wake <- to }
func (c *fakeClock) firstWait(t *testing.T) time.Duration {
	t.Helper()
	eventually(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return len(c.waits) > 0 })
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.waits[0]
}

type fakeRepository struct {
	mu          sync.Mutex
	hours       [][2]time.Time
	days        [][2]time.Time
	months      []time.Time
	cutoffs     []time.Time
	entered     chan struct{}
	release     chan struct{}
	failDay     int
	dailyResult domain.DailySummary
	active      int
	closed      bool
	afterClose  int
}

type contextIgnoringRepository struct {
	entered chan struct{}
	release chan struct{}
}

func (r *contextIgnoringRepository) AggregateHour(context.Context, time.Time, time.Time) (domain.HourlySummary, error) {
	select {
	case <-r.entered:
	default:
		close(r.entered)
	}
	<-r.release
	return domain.HourlySummary{}, nil
}
func (*contextIgnoringRepository) AggregateDay(context.Context, time.Time, time.Time) (domain.DailySummary, error) {
	return domain.DailySummary{}, nil
}
func (*contextIgnoringRepository) AggregateMonth(context.Context, time.Time) (domain.MonthlySummary, error) {
	return domain.MonthlySummary{}, nil
}
func (*contextIgnoringRepository) PruneBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (r *fakeRepository) recordCall() {
	if r.closed {
		r.afterClose++
	}
}

func TestRunnerInitialStatusHasTimestamp(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	runner := New(&fakeRepository{}, func(context.Context) (domain.Settings, error) {
		return domain.Settings{Timezone: "UTC"}, nil
	}, WithClock(newFakeClock(now)))
	status := runner.Status()
	if status.State != "idle" || !status.UpdatedAt.Equal(now) {
		t.Fatalf("initial status = %#v", status)
	}
}

func (r *fakeRepository) AggregateHour(_ context.Context, from, to time.Time) (domain.HourlySummary, error) {
	r.mu.Lock()
	r.recordCall()
	r.hours = append(r.hours, [2]time.Time{from, to})
	r.mu.Unlock()
	return domain.HourlySummary{}, nil
}
func (r *fakeRepository) AggregateDay(ctx context.Context, from, to time.Time) (domain.DailySummary, error) {
	if r.entered != nil {
		r.mu.Lock()
		r.recordCall()
		r.active++
		r.mu.Unlock()
		select {
		case r.entered <- struct{}{}:
		default:
		}
		if r.release != nil {
			select {
			case <-r.release:
			case <-ctx.Done():
			}
		} else {
			<-ctx.Done()
		}
		r.mu.Lock()
		r.active--
		cancelled := ctx.Err()
		r.mu.Unlock()
		if cancelled != nil {
			return domain.DailySummary{}, cancelled
		}
	}
	r.mu.Lock()
	if r.entered == nil {
		r.recordCall()
	}
	if r.failDay > 0 {
		r.failDay--
		r.mu.Unlock()
		return domain.DailySummary{}, errors.New("temporary")
	}
	r.days = append(r.days, [2]time.Time{from, to})
	r.mu.Unlock()
	return r.dailyResult, nil
}
func (r *fakeRepository) AggregateMonth(_ context.Context, at time.Time) (domain.MonthlySummary, error) {
	r.mu.Lock()
	r.recordCall()
	r.months = append(r.months, at)
	r.mu.Unlock()
	return domain.MonthlySummary{}, nil
}
func (r *fakeRepository) PruneBefore(_ context.Context, cutoff time.Time) (int64, error) {
	r.mu.Lock()
	r.recordCall()
	r.cutoffs = append(r.cutoffs, cutoff)
	r.mu.Unlock()
	return 0, nil
}

func TestRunnerWaitsUntilLocalMidnightPlusFiveMinutes(t *testing.T) {
	location, _ := time.LoadLocation("America/Sao_Paulo")
	now := time.Date(2026, 7, 14, 0, 4, 0, 0, location)
	clock := newFakeClock(now)
	repository := &fakeRepository{}
	runner := New(repository, func(context.Context) (domain.Settings, error) {
		return domain.Settings{Timezone: location.String(), RetentionDays: 30}, nil
	}, WithClock(clock))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	if got, want := clock.firstWait(t), time.Minute; got != want {
		t.Fatalf("first wait = %s, want %s", got, want)
	}
	clock.advance(now.Add(time.Minute))
	eventually(t, func() bool { repository.mu.Lock(); defer repository.mu.Unlock(); return len(repository.days) == 1 })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if len(repository.cutoffs) != 1 {
		t.Fatalf("retention runs = %d, want 1", len(repository.cutoffs))
	}
	if got := repository.days[0][0].In(location).Format("2006-01-02"); got != "2026-07-13" {
		t.Fatalf("aggregated day = %s", got)
	}
}

func TestRunnerExecutesOneMissedRunOnStartupAndThenOnceDaily(t *testing.T) {
	location, _ := time.LoadLocation("America/Sao_Paulo")
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, location)
	clock := newFakeClock(now)
	repository := &fakeRepository{}
	runner := New(repository, func(context.Context) (domain.Settings, error) {
		return domain.Settings{Timezone: location.String()}, nil
	}, WithClock(clock))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	eventually(t, func() bool { repository.mu.Lock(); defer repository.mu.Unlock(); return len(repository.days) == 1 })
	clock.advance(time.Date(2026, 7, 15, 0, 5, 0, 0, location))
	eventually(t, func() bool { repository.mu.Lock(); defer repository.mu.Unlock(); return len(repository.days) == 2 })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if len(repository.cutoffs) != 2 {
		t.Fatalf("retention runs = %d, want 2", len(repository.cutoffs))
	}
	if got := repository.cutoffs[0].In(location).Format("2006-01-02"); got != "2024-07-14" {
		t.Fatalf("default retention cutoff = %s, want 2024-07-14", got)
	}
}

func TestRunnerCancellationCancelsAndJoinsCooperativeCurrentWork(t *testing.T) {
	repository := &fakeRepository{entered: make(chan struct{}, 1), release: make(chan struct{})}
	runner := New(repository, func(context.Context) (domain.Settings, error) {
		return domain.Settings{Timezone: "UTC", RetentionDays: 30}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	<-repository.entered
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("runner did not finish")
	}
	close(repository.release)
}

func TestRunnerCancellationCapsTransactionWait(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{entered: make(chan struct{}, 1)}
	runner := New(repository, func(context.Context) (domain.Settings, error) {
		return domain.Settings{Timezone: "UTC", RetentionDays: 30}, nil
	}, WithClock(newFakeClock(now)))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	<-repository.entered
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		repository.mu.Lock()
		active := repository.active
		repository.closed = true
		repository.mu.Unlock()
		if active != 0 {
			t.Fatalf("runner returned with %d active workers", active)
		}
		time.Sleep(20 * time.Millisecond)
		repository.mu.Lock()
		afterClose := repository.afterClose
		repository.mu.Unlock()
		if afterClose != 0 {
			t.Fatalf("%d repository calls occurred after owner close", afterClose)
		}
	case <-time.After(time.Second):
		t.Fatal("runner exceeded shutdown cap")
	}
}

func TestRunnerReturnsAtShutdownDeadlineWhenDependencyIgnoresContext(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	repository := &contextIgnoringRepository{entered: make(chan struct{}), release: make(chan struct{})}
	runner := New(repository, func(context.Context) (domain.Settings, error) {
		return domain.Settings{Timezone: "UTC", RetentionDays: 30}, nil
	}, WithClock(newFakeClock(now)), WithShutdownTimeout(20*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	<-repository.entered
	started := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrShutdownTimeout) {
			t.Fatalf("shutdown error=%v", err)
		}
		if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
			t.Fatalf("shutdown elapsed=%v", elapsed)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("runner exceeded shutdown deadline")
	}
	close(repository.release)
}

func TestRunnerRetriesFailedDayBeforeAdvancingCatchup(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	clock := newFakeClock(now)
	repository := &fakeRepository{failDay: 1}
	runner := New(repository, func(context.Context) (domain.Settings, error) {
		return domain.Settings{Timezone: "UTC", RetentionDays: 30}, nil
	}, WithClock(clock))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	eventually(t, func() bool {
		clock.mu.Lock()
		defer clock.mu.Unlock()
		return len(clock.waits) > 0 && clock.waits[len(clock.waits)-1] == time.Minute
	})
	repository.mu.Lock()
	firstAttempts := repository.failDay
	repository.mu.Unlock()
	if firstAttempts != 0 {
		t.Fatal("first failed attempt did not run")
	}
	clock.advance(now.Add(time.Minute))
	eventually(t, func() bool {
		repository.mu.Lock()
		defer repository.mu.Unlock()
		return len(repository.days) == 1 && len(repository.cutoffs) == 1
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRunnerRecoversFromTransientSettingsRead(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	clock := newFakeClock(now)
	repository := &fakeRepository{}
	reads := 0
	runner := New(repository, func(context.Context) (domain.Settings, error) {
		reads++
		if reads == 1 {
			return domain.Settings{}, errors.New("temporary")
		}
		return domain.Settings{Timezone: "UTC", RetentionDays: 30}, nil
	}, WithClock(clock))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	eventually(t, func() bool { return runner.Status().ErrorClass == "settings" })
	clock.advance(now.Add(time.Minute))
	eventually(t, func() bool { repository.mu.Lock(); defer repository.mu.Unlock(); return len(repository.days) == 1 })
	if status := runner.Status(); status.ErrorClass != "" || status.State != "running" {
		t.Fatalf("status did not recover: %#v", status)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(time.Millisecond)
	}
}
