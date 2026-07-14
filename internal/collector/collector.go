package collector

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time                             { return time.Now() }
func (systemClock) After(delay time.Duration) <-chan time.Time { return time.After(delay) }

type SnapshotReader interface {
	ReadSnapshot(context.Context) (domain.TelemetrySnapshot, error)
}

type Store interface {
	SaveMinute(context.Context, domain.TelemetrySnapshot) error
	SaveEvent(context.Context, time.Time, string, any) error
}

type Config struct {
	Clock        Clock
	PollInterval time.Duration
	ReadTimeout  time.Duration
	StaleAfter   time.Duration
	RetryMin     time.Duration
	RetryMax     time.Duration
	Jitter       Jitter
}

type Collector struct {
	config  Config
	reader  SnapshotReader
	store   Store
	hub     *Hub
	backoff *Backoff

	mu      sync.RWMutex
	state   State
	running bool
}

func New(config Config, reader SnapshotReader, store Store, hub *Hub) *Collector {
	if config.Clock == nil {
		config.Clock = systemClock{}
	}
	if config.PollInterval == 0 {
		config.PollInterval = 10 * time.Second
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 3 * time.Second
	}
	if config.StaleAfter == 0 {
		config.StaleAfter = 30 * time.Second
	}
	if config.RetryMin == 0 {
		config.RetryMin = time.Second
	}
	if config.RetryMax == 0 {
		config.RetryMax = time.Minute
	}
	if hub == nil {
		hub = NewHub()
	}
	return &Collector{
		config: config, reader: reader, store: store, hub: hub,
		backoff: NewBackoff(config.RetryMin, config.RetryMax, config.Jitter),
	}
}

func (c *Collector) Latest() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneState(c.state)
}

func (c *Collector) Run(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return err
	}
	if err := c.beginRun(); err != nil {
		return err
	}
	defer c.endRun()
	freshnessUpdates := make(chan time.Time, 1)
	freshnessDone := make(chan struct{})
	go c.monitorFreshness(ctx, freshnessUpdates, freshnessDone)
	defer func() { <-freshnessDone }()

	var pending *domain.TelemetrySnapshot
	var previous *domain.TelemetrySnapshot
	delay := time.Duration(0)

	for {
		if delay > 0 {
			poll := c.config.Clock.After(delay)
			select {
			case <-ctx.Done():
				c.flushPending(pending)
				return ctx.Err()
			case <-poll:
			}
		}

		snapshot, err := c.read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				c.flushPending(pending)
				return ctx.Err()
			}
			c.recordError(err)
			delay = c.backoff.Next()
			continue
		}

		copy := cloneSnapshot(&snapshot)
		storageErrors := make([]error, 0, 3)
		if pending != nil && minuteOf(*pending) != minuteOf(*copy) {
			if err := c.store.SaveMinute(ctx, *cloneSnapshot(pending)); err != nil {
				storageErrors = append(storageErrors, fmt.Errorf("save minute: %w", err))
			}
			pending = nil
		}
		if pending == nil || !copy.ObservedAt.Before(pending.ObservedAt) {
			pending = cloneSnapshot(copy)
		}
		if previous != nil {
			at := copy.ObservedAt
			if at.IsZero() {
				at = c.config.Clock.Now()
			}
			if previous.Status != copy.Status {
				if err := c.store.SaveEvent(ctx, at, "status_change", *cloneSnapshot(copy)); err != nil {
					storageErrors = append(storageErrors, fmt.Errorf("save status event: %w", err))
				}
			}
			if !sameFaults(previous.FaultCodes, copy.FaultCodes) {
				if err := c.store.SaveEvent(ctx, at, "fault_change", *cloneSnapshot(copy)); err != nil {
					storageErrors = append(storageErrors, fmt.Errorf("save fault event: %w", err))
				}
			}
		}
		previous = cloneSnapshot(copy)
		lastSuccess := c.recordSuccess(copy, errors.Join(storageErrors...))
		signalLatestTime(freshnessUpdates, lastSuccess)
		c.backoff.Reset()
		delay = c.config.PollInterval
	}
}

func (c *Collector) validate() error {
	if c.reader == nil || c.store == nil || c.config.Clock == nil {
		return errors.New("collector: reader, store, and clock are required")
	}
	if c.config.PollInterval <= 0 || c.config.ReadTimeout <= 0 || c.config.StaleAfter <= 0 ||
		c.config.RetryMin <= 0 || c.config.RetryMax < c.config.RetryMin {
		return errors.New("collector: durations must be positive and retry maximum must not be below minimum")
	}
	return nil
}

func (c *Collector) beginRun() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return errors.New("collector: already running")
	}
	c.running = true
	return nil
}

func (c *Collector) endRun() {
	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
}

func (c *Collector) read(ctx context.Context) (domain.TelemetrySnapshot, error) {
	readContext, cancel := context.WithTimeout(ctx, c.config.ReadTimeout)
	defer cancel()
	return c.reader.ReadSnapshot(readContext)
}

func (c *Collector) recordSuccess(snapshot *domain.TelemetrySnapshot, storageError error) time.Time {
	state := State{Snapshot: cloneSnapshot(snapshot), LastSuccess: c.config.Clock.Now()}
	if storageError != nil {
		state.LastError = storageError.Error()
	}
	c.mu.Lock()
	c.state = state
	c.mu.Unlock()
	c.hub.Publish(Event{Kind: "snapshot", Snapshot: snapshot, State: state})
	return state.LastSuccess
}

func (c *Collector) recordError(err error) {
	c.mu.Lock()
	c.state.LastError = err.Error()
	state := cloneState(c.state)
	c.mu.Unlock()
	c.hub.Publish(Event{Kind: "state", State: state})
}

func (c *Collector) markStale() {
	c.mu.Lock()
	if c.state.LastSuccess.IsZero() || c.state.Stale || c.config.Clock.Now().Before(c.state.LastSuccess.Add(c.config.StaleAfter)) {
		c.mu.Unlock()
		return
	}
	c.state.Stale = true
	state := cloneState(c.state)
	c.mu.Unlock()
	c.hub.Publish(Event{Kind: "state", State: state})
}

// monitorFreshness is independent of synchronous reader and storage work. It
// only observes successful-read timestamps; polling remains owned by Run.
func (c *Collector) monitorFreshness(ctx context.Context, updates <-chan time.Time, done chan<- struct{}) {
	defer close(done)
	var deadline time.Time
	for {
		if deadline.IsZero() {
			select {
			case <-ctx.Done():
				return
			case lastSuccess := <-updates:
				deadline = lastSuccess.Add(c.config.StaleAfter)
			}
			continue
		}

		delay := deadline.Sub(c.config.Clock.Now())
		if delay <= 0 {
			c.markStale()
			deadline = time.Time{}
			continue
		}
		timer := c.config.Clock.After(delay)
		select {
		case <-ctx.Done():
			return
		case lastSuccess := <-updates:
			deadline = lastSuccess.Add(c.config.StaleAfter)
		case <-timer:
			c.markStale()
			deadline = time.Time{}
		}
	}
}

func signalLatestTime(updates chan time.Time, value time.Time) {
	select {
	case updates <- value:
	default:
		select {
		case <-updates:
		default:
		}
		updates <- value
	}
}

func (c *Collector) flushPending(pending *domain.TelemetrySnapshot) {
	if pending == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.config.ReadTimeout)
	defer cancel()
	if err := c.store.SaveMinute(ctx, *cloneSnapshot(pending)); err != nil {
		c.recordError(fmt.Errorf("save final minute: %w", err))
	}
}

func minuteOf(snapshot domain.TelemetrySnapshot) int64 {
	return snapshot.ObservedAt.UTC().Truncate(time.Minute).UnixNano()
}

func sameFaults(left, right []uint16) bool {
	left = append([]uint16(nil), left...)
	right = append([]uint16(nil), right...)
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(left, right)
}
