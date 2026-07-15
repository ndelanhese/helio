package collector

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

type fakeClock struct {
	mu        sync.Mutex
	now       time.Time
	waits     []*fakeWait
	requested []time.Duration
}

type fakeWait struct {
	due   time.Time
	ch    chan time.Time
	fired bool
}

func newFakeClock(now time.Time) *fakeClock { return &fakeClock{now: now} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(delay time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	wait := &fakeWait{due: c.now.Add(delay), ch: ch}
	if delay <= 0 {
		wait.fired = true
		ch <- c.now
	}
	c.waits = append(c.waits, wait)
	c.requested = append(c.requested, delay)
	return ch
}

func (c *fakeClock) Advance(delay time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(delay)
	now := c.now
	for _, wait := range c.waits {
		if !wait.fired && !wait.due.After(now) {
			wait.fired = true
			wait.ch <- wait.due
		}
	}
	c.mu.Unlock()
}

func (c *fakeClock) delays() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.requested...)
}

type readResult struct {
	snapshot domain.TelemetrySnapshot
	err      error
}

type fakeReader struct {
	mu      sync.Mutex
	results []readResult
	calls   int
}

func (r *fakeReader) ReadSnapshot(ctx context.Context) (domain.TelemetrySnapshot, error) {
	r.mu.Lock()
	index := r.calls
	r.calls++
	if index < len(r.results) {
		result := r.results[index]
		r.mu.Unlock()
		return result.snapshot, result.err
	}
	r.mu.Unlock()
	<-ctx.Done()
	return domain.TelemetrySnapshot{}, ctx.Err()
}

func (r *fakeReader) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

type fakeStore struct {
	mu        sync.Mutex
	minutes   []domain.TelemetrySnapshot
	events    []storedEvent
	minuteErr error
	eventErr  error
}

type storedEvent struct {
	at      time.Time
	kind    string
	payload any
}

func (s *fakeStore) SaveMinute(_ context.Context, snapshot domain.TelemetrySnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.minutes = append(s.minutes, snapshot)
	return s.minuteErr
}

func (s *fakeStore) SaveEvent(_ context.Context, at time.Time, kind string, payload any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, storedEvent{at: at, kind: kind, payload: payload})
	return s.eventErr
}

func (s *fakeStore) saved() ([]domain.TelemetrySnapshot, []storedEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.TelemetrySnapshot(nil), s.minutes...), append([]storedEvent(nil), s.events...)
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition was not met")
		}
		time.Sleep(time.Millisecond)
	}
}

func testConfig(clock Clock) Config {
	return Config{
		Clock: clock, PollInterval: 10 * time.Second, ReadTimeout: 3 * time.Second,
		StaleAfter: 30 * time.Second, RetryMin: time.Second, RetryMax: time.Minute,
	}
}

func TestCollectorPollsImmediatelyPublishesAndPersistsLatestPerMinute(t *testing.T) {
	base := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	clock := newFakeClock(base)
	reader := &fakeReader{results: []readResult{
		{snapshot: domain.TelemetrySnapshot{ObservedAt: base.Add(5 * time.Second), Status: "normal", ACPowerW: 1, FaultCodes: []uint16{2, 1}}},
		{snapshot: domain.TelemetrySnapshot{ObservedAt: base.Add(55 * time.Second), Status: "normal", ACPowerW: 2, FaultCodes: []uint16{1, 2}}},
		{snapshot: domain.TelemetrySnapshot{ObservedAt: base.Add(65 * time.Second), Status: "fault", ACPowerW: 3, FaultCodes: []uint16{7}}},
	}}
	store := &fakeStore{}
	hub := NewHub()
	events, unsubscribe := hub.Subscribe()
	defer unsubscribe()
	collector := New(testConfig(clock), reader, store, hub)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()

	eventually(t, func() bool { return reader.count() == 1 })
	first := <-events
	if first.Kind != "snapshot" || first.Snapshot.ACPowerW != 1 {
		t.Fatalf("first event=%+v", first)
	}
	clock.Advance(10 * time.Second)
	eventually(t, func() bool { return reader.count() == 2 })
	clock.Advance(10 * time.Second)
	eventually(t, func() bool { return reader.count() == 3 })

	state := collector.Latest()
	if state.Snapshot == nil || state.Snapshot.ACPowerW != 3 || state.Stale || state.LastError != "" {
		t.Fatalf("latest=%+v", state)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error=%v, want context cancellation", err)
	}
	minutes, savedEvents := store.saved()
	if len(minutes) != 2 || minutes[0].ACPowerW != 2 || minutes[1].ACPowerW != 3 {
		t.Fatalf("saved minutes=%+v, want latest from each observed minute", minutes)
	}
	if len(savedEvents) != 2 {
		t.Fatalf("saved events=%+v, want status and fault changes", savedEvents)
	}
	if got := []string{savedEvents[0].kind, savedEvents[1].kind}; !reflect.DeepEqual(got, []string{"status_change", "fault_change"}) {
		t.Fatalf("event kinds=%v", got)
	}
}

func TestCollectorRetriesWithCappedBackoffAndResetsAfterSuccess(t *testing.T) {
	base := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	clock := newFakeClock(base)
	reader := &fakeReader{results: []readResult{
		{err: errors.New("timeout 1")}, {err: errors.New("timeout 2")},
		{err: errors.New("timeout 3")}, {snapshot: domain.TelemetrySnapshot{ObservedAt: base, Status: "normal"}},
		{err: errors.New("timeout after recovery")},
	}}
	cfg := testConfig(clock)
	cfg.StaleAfter = time.Hour
	collector := New(cfg, reader, &fakeStore{}, NewHub())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()

	for call, delay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		eventually(t, func() bool { return reader.count() == call+1 })
		clock.Advance(delay)
	}
	eventually(t, func() bool { return reader.count() == 4 })
	eventually(t, func() bool {
		delays := clock.delays()
		return slices.Contains(delays, 10*time.Second)
	})
	clock.Advance(10 * time.Second)
	eventually(t, func() bool { return reader.count() == 5 })
	eventually(t, func() bool {
		delays := clock.delays()
		count := 0
		for _, delay := range delays {
			if delay == time.Second {
				count++
			}
		}
		return count >= 2
	})
	cancel()
	<-done
}

func TestCollectorFailureMetadataIsClassifiedTimestampedAndClearedOnRecovery(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	clock := newFakeClock(now)
	collector := New(testConfig(clock), &fakeReader{}, &fakeStore{}, NewHub())
	collector.recordError(errors.New("dial 192.168.1.50 secret-frame"))
	state := collector.Latest()
	if state.ErrorClass != "communication" || !state.LastErrorAt.Equal(now) {
		t.Fatalf("failure metadata=%+v", state)
	}
	collector.recordSuccess(&domain.TelemetrySnapshot{ObservedAt: now}, nil)
	state = collector.Latest()
	if state.ErrorClass != "" || !state.LastErrorAt.IsZero() || state.LastError != "" {
		t.Fatalf("failure metadata survived recovery=%+v", state)
	}
}

func TestCollectorMarksStateStaleThirtySecondsAfterLastSuccess(t *testing.T) {
	base := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	clock := newFakeClock(base)
	reader := &fakeReader{results: []readResult{
		{snapshot: domain.TelemetrySnapshot{ObservedAt: base, Status: "normal"}},
		{err: errors.New("offline")},
	}}
	hub := NewHub()
	events, unsubscribe := hub.Subscribe()
	defer unsubscribe()
	collector := New(testConfig(clock), reader, &fakeStore{}, hub)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()
	eventually(t, func() bool { return reader.count() == 1 })
	<-events

	clock.Advance(30 * time.Second)
	eventually(t, func() bool { return collector.Latest().Stale })
	select {
	case event := <-events:
		if event.Kind != "state" || !event.State.Stale {
			t.Fatalf("stale event=%+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("stale transition was not published")
	}
	cancel()
	<-done
}

func TestCollectorStaleBoundaryIsThirtySecondsExactly(t *testing.T) {
	base := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	clock := newFakeClock(base)
	reader := &fakeReader{results: []readResult{
		{snapshot: domain.TelemetrySnapshot{ObservedAt: base, Status: "normal"}},
		{err: errors.New("offline")},
	}}
	collector := New(testConfig(clock), reader, &fakeStore{}, NewHub())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()
	eventually(t, func() bool { return reader.count() == 1 })

	clock.Advance(29*time.Second + 999*time.Millisecond)
	if collector.Latest().Stale {
		t.Fatal("state became stale before the 30-second boundary")
	}
	clock.Advance(time.Millisecond)
	eventually(t, func() bool { return collector.Latest().Stale })
	cancel()
	<-done
}

func TestCollectorStaleTransitionDoesNotMovePendingRetryDeadline(t *testing.T) {
	base := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	clock := newFakeClock(base)
	results := []readResult{{snapshot: domain.TelemetrySnapshot{ObservedAt: base, Status: "normal"}}}
	for range 6 {
		results = append(results, readResult{err: errors.New("offline")})
	}
	reader := &fakeReader{results: results}
	collector := New(testConfig(clock), reader, &fakeStore{}, NewHub())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()
	eventually(t, func() bool { return reader.count() == 1 })

	for call, delay := range []time.Duration{10 * time.Second, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second} {
		clock.Advance(delay)
		eventually(t, func() bool { return reader.count() == call+2 })
	}
	// The next 16-second retry was scheduled at t+25, hence is due at t+41.
	clock.Advance(5 * time.Second)
	eventually(t, func() bool { return collector.Latest().Stale })
	clock.Advance(10*time.Second + 999*time.Millisecond)
	if got := reader.count(); got != 6 {
		t.Fatalf("reader calls before retry deadline=%d, want 6", got)
	}
	clock.Advance(time.Millisecond)
	eventually(t, func() bool { return reader.count() == 7 })
	cancel()
	<-done
}

type blockingEventStore struct {
	entered chan struct{}
	release chan struct{}
}

func (s *blockingEventStore) SaveMinute(context.Context, domain.TelemetrySnapshot) error {
	return nil
}

func (s *blockingEventStore) SaveEvent(ctx context.Context, _ time.Time, _ string, _ any) error {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestCollectorPublishesStaleWhileStorageIsBlockedAndRecoversOnSuccess(t *testing.T) {
	base := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	clock := newFakeClock(base)
	reader := &fakeReader{results: []readResult{
		{snapshot: domain.TelemetrySnapshot{ObservedAt: base, Status: "normal"}},
		{snapshot: domain.TelemetrySnapshot{ObservedAt: base.Add(10 * time.Second), Status: "fault"}},
	}}
	store := &blockingEventStore{entered: make(chan struct{}, 1), release: make(chan struct{})}
	hub := NewHub()
	events, unsubscribe := hub.Subscribe()
	defer unsubscribe()
	collector := New(testConfig(clock), reader, store, hub)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()
	eventually(t, func() bool { return reader.count() == 1 })
	<-events
	clock.Advance(10 * time.Second)
	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("collector did not enter blocking storage call")
	}

	clock.Advance(19*time.Second + 999*time.Millisecond)
	if collector.Latest().Stale {
		t.Fatal("state became stale before storage crossed the freshness threshold")
	}
	clock.Advance(time.Millisecond)
	eventually(t, func() bool { return collector.Latest().Stale })
	select {
	case event := <-events:
		if event.Kind != "state" || !event.State.Stale {
			t.Fatalf("event while storage blocked=%+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("stale event was not published while storage was blocked")
	}

	close(store.release)
	eventually(t, func() bool {
		state := collector.Latest()
		return !state.Stale && state.Snapshot != nil && state.Snapshot.Status == "fault" && state.LastSuccess.Equal(clock.Now())
	})
	cancel()
	<-done
}

func TestCollectorReadTimeoutAndCancellationDoNotLeakOwner(t *testing.T) {
	clock := newFakeClock(time.Now())
	reader := &fakeReader{}
	collector := New(testConfig(clock), reader, &fakeStore{}, NewHub())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()
	eventually(t, func() bool { return reader.count() == 1 })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("collector did not release blocked reader on cancellation")
	}
}

func TestCollectorRejectsInvalidConfiguration(t *testing.T) {
	collector := New(Config{}, &fakeReader{}, &fakeStore{}, NewHub())
	collector.config.PollInterval = -time.Second
	if err := collector.Run(context.Background()); err == nil {
		t.Fatal("invalid configuration was accepted")
	}
}

func TestCollectorAppliesOperationalTimingDefaults(t *testing.T) {
	clock := newFakeClock(time.Now())
	reader := &fakeReader{results: []readResult{{snapshot: domain.TelemetrySnapshot{ObservedAt: clock.Now()}}}}
	collector := New(Config{Clock: clock}, reader, &fakeStore{}, NewHub())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()
	eventually(t, func() bool { return reader.count() == 1 })
	eventually(t, func() bool { return slices.Contains(clock.delays(), 10*time.Second) })
	cancel()
	<-done
}
