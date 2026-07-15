package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/alerts"
	"github.com/ndelanhese/helio/internal/collector"
	"github.com/ndelanhese/helio/internal/config"
	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/jobs"
	"github.com/ndelanhese/helio/internal/storage"
)

type startupRepository struct{}

func (startupRepository) AggregateHour(context.Context, time.Time, time.Time) (domain.HourlySummary, error) {
	return domain.HourlySummary{}, nil
}
func (startupRepository) AggregateDay(context.Context, time.Time, time.Time) (domain.DailySummary, error) {
	return domain.DailySummary{}, nil
}
func (startupRepository) AggregateMonth(context.Context, time.Time) (domain.MonthlySummary, error) {
	return domain.MonthlySummary{}, nil
}
func (startupRepository) PruneBefore(context.Context, time.Time) (int64, error) { return 0, nil }

type startupAlertEvaluator struct{ inputs chan alerts.Input }

func (e startupAlertEvaluator) Evaluate(_ context.Context, input alerts.Input) ([]alerts.Transition, error) {
	e.inputs <- input
	return nil, nil
}

func TestStartJobsReadyRegistersOrderedAlertConsumerBeforeFirstPublish(t *testing.T) {
	hub := collector.NewHub()
	evaluator := startupAlertEvaluator{inputs: make(chan alerts.Input, 1)}
	runner := jobs.New(startupRepository{}, func(context.Context) (domain.Settings, error) {
		return domain.Settings{Timezone: "UTC", ActiveMPPT: []int{1}}, nil
	}, jobs.WithIntegration(jobs.Integration{Alerts: evaluator, Events: hub}))
	a := &App{jobRunner: runner, startupWait: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.startJobsReady(ctx); err != nil {
		t.Fatal(err)
	}
	observed := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	hub.Publish(collector.Event{Kind: "snapshot", Snapshot: &domain.TelemetrySnapshot{ObservedAt: observed}})
	select {
	case input := <-evaluator.inputs:
		if !input.TelemetryObserved || !input.At.Equal(observed) {
			t.Fatalf("first input=%#v", input)
		}
	case <-time.After(time.Second):
		t.Fatal("first collector event was published before alert subscription was ready")
	}
	a.stopServices()
}

func TestStartJobsReadyDoesNotDeadlockWhenIntegrationDisabled(t *testing.T) {
	runner := jobs.New(startupRepository{}, func(context.Context) (domain.Settings, error) {
		return domain.Settings{Timezone: "UTC"}, nil
	})
	a := &App{jobRunner: runner, startupWait: 50 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	if err := a.startJobsReady(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	a.stopServices()
}

type orderedListener struct {
	mu     *sync.Mutex
	events *[]string
	closed chan struct{}
	once   sync.Once
}

func (l *orderedListener) Accept() (net.Conn, error) { <-l.closed; return nil, net.ErrClosed }
func (l *orderedListener) Close() error {
	l.once.Do(func() {
		l.mu.Lock()
		*l.events = append(*l.events, "listener_stop")
		l.mu.Unlock()
		close(l.closed)
	})
	return nil
}
func (*orderedListener) Addr() net.Addr { return &net.TCPAddr{} }

func TestShutdownStopsListenerBeforeServicesAndJoinsBeforeReturn(t *testing.T) {
	var mu sync.Mutex
	events := []string{}
	listener := &orderedListener{mu: &mu, events: &events, closed: make(chan struct{})}
	server := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	release := make(chan struct{})
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- shutdownHTTP(context.Background(), server, listener, func() {
			mu.Lock()
			events = append(events, "service_cancel")
			mu.Unlock()
			<-release
			mu.Lock()
			events = append(events, "workers_joined")
			mu.Unlock()
		})
	}()
	eventuallyApp(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(events) >= 2 })
	mu.Lock()
	got := append([]string(nil), events...)
	mu.Unlock()
	serviceIndex := slices.Index(got, "service_cancel")
	listenerIndex := slices.Index(got, "listener_stop")
	if listenerIndex < 0 || serviceIndex < 0 || listenerIndex > serviceIndex {
		t.Fatalf("shutdown order=%v", got)
	}
	select {
	case <-shutdownDone:
		t.Fatal("shutdown returned before workers joined")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not return")
	}
	mu.Lock()
	got = append([]string(nil), events...)
	mu.Unlock()
	if got[len(got)-1] != "workers_joined" {
		t.Fatalf("shutdown order=%v", got)
	}
	<-serveDone
}

func eventuallyApp(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(time.Millisecond)
	}
}

type recordingSettingsRuntime struct {
	mu         sync.Mutex
	active     domain.Settings
	inflight   int
	concurrent bool
}

type recordingJobRuntime struct {
	started chan struct{}
	stopped chan struct{}
}

type neverReadyJobRuntime struct{ ready chan struct{} }

func (r *neverReadyJobRuntime) PrepareRun() <-chan struct{} { return r.ready }
func (*neverReadyJobRuntime) Run(ctx context.Context) error { <-ctx.Done(); return nil }
func (*neverReadyJobRuntime) Status() jobs.Status           { return jobs.Status{State: "starting"} }

func TestStartJobsReadyTimesOutAndCanBeCancelledWithoutDeadlock(t *testing.T) {
	a := &App{jobRunner: &neverReadyJobRuntime{ready: make(chan struct{})}, startupWait: 15 * time.Millisecond}
	started := time.Now()
	err := a.startJobsReady(context.Background())
	if err == nil || time.Since(started) > 100*time.Millisecond {
		t.Fatalf("readiness result err=%v elapsed=%v", err, time.Since(started))
	}
	if !a.pauseJobs() {
		t.Fatal("timed-out jobs runner was not cancellable")
	}
}

type barrierJobRuntime struct {
	mu           sync.Mutex
	runs         int
	started      chan int
	firstRelease chan struct{}
}

func (r *barrierJobRuntime) Run(ctx context.Context) error {
	r.mu.Lock()
	r.runs++
	run := r.runs
	r.mu.Unlock()
	r.started <- run
	<-ctx.Done()
	if run == 1 && r.firstRelease != nil {
		<-r.firstRelease
	}
	return nil
}
func (*barrierJobRuntime) Status() jobs.Status { return jobs.Status{State: "running"} }

type signalingSettingsRuntime struct {
	called chan struct{}
	err    error
}

type exitingJobRuntime struct {
	mu   sync.Mutex
	runs int
}

type timedOutJobRuntime struct {
	db     *storage.DB
	probe  chan struct{}
	result chan error
}

func (r *timedOutJobRuntime) Run(ctx context.Context) error {
	<-ctx.Done()
	go func() {
		<-r.probe
		r.result <- r.db.Ready(context.Background())
	}()
	return jobs.ErrShutdownTimeout
}
func (*timedOutJobRuntime) Status() jobs.Status {
	return jobs.Status{State: "stopped", ErrorClass: "shutdown_timeout"}
}

func TestTimedOutJobsRetainDatabaseForAbandonedWorker(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "retained.db"))
	if err != nil {
		t.Fatal(err)
	}
	job := &timedOutJobRuntime{db: db, probe: make(chan struct{}), result: make(chan error, 1)}
	a := &App{db: db, jobRunner: job}
	ctx, cancel := context.WithCancel(context.Background())
	a.startJobs(ctx)
	cancel()
	a.stopServices()
	a.closeDatabaseUnlessRetained()
	close(job.probe)
	if err := <-job.result; err != nil {
		t.Fatalf("abandoned worker observed closed database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func (r *exitingJobRuntime) Run(context.Context) error {
	r.mu.Lock()
	r.runs++
	r.mu.Unlock()
	return errors.New("terminal")
}
func (*exitingJobRuntime) Status() jobs.Status {
	return jobs.Status{State: "degraded", ErrorClass: "configuration"}
}

func TestUnexpectedJobsExitClearsRuntimeSoItCanRestart(t *testing.T) {
	job := &exitingJobRuntime{}
	a := &App{jobRunner: job}
	a.startJobs(context.Background())
	eventuallyApp(t, func() bool { a.jobsMu.Lock(); defer a.jobsMu.Unlock(); return a.jobsCancel == nil && a.jobsDone == nil })
	a.startJobs(context.Background())
	eventuallyApp(t, func() bool { job.mu.Lock(); defer job.mu.Unlock(); return job.runs == 2 })
	a.stopServices()
}

func (r *signalingSettingsRuntime) Apply(context.Context, domain.Settings, func() error) error {
	close(r.called)
	return r.err
}

func TestSettingsApplyJoinsJobsBeforeCalendarTransactionAndRestartsAfterCommit(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	job := &barrierJobRuntime{started: make(chan int, 2), firstRelease: make(chan struct{})}
	settingsRuntime := &signalingSettingsRuntime{called: make(chan struct{})}
	runtime := &collectorRuntime{}
	runtime.start(context.Background())
	a := &App{db: db, runtime: runtime, settingsRuntime: settingsRuntime, jobRunner: job}
	a.startJobs(runtime.context())
	<-job.started
	done := make(chan error, 1)
	go func() { done <- a.applySettings(context.Background(), testSettings(7), "actor") }()
	select {
	case <-settingsRuntime.called:
		t.Fatal("settings transaction interleaved with active jobs cycle")
	case <-time.After(20 * time.Millisecond):
	}
	close(job.firstRelease)
	select {
	case <-settingsRuntime.called:
	case <-time.After(time.Second):
		t.Fatal("settings transaction did not start after jobs joined")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case run := <-job.started:
		if run != 2 {
			t.Fatalf("restart run=%d", run)
		}
	case <-time.After(time.Second):
		t.Fatal("jobs did not restart")
	}
	a.stopServices()
}

func TestSettingsRollbackRestartsPreviousJobsWithoutDuplicateRunner(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	job := &barrierJobRuntime{started: make(chan int, 3)}
	settingsRuntime := &signalingSettingsRuntime{called: make(chan struct{}), err: errors.New("rollback")}
	runtime := &collectorRuntime{}
	runtime.start(context.Background())
	a := &App{db: db, runtime: runtime, settingsRuntime: settingsRuntime, jobRunner: job}
	a.startJobs(runtime.context())
	<-job.started
	if err := a.applySettings(context.Background(), testSettings(7), "actor"); err == nil {
		t.Fatal("settings unexpectedly succeeded")
	}
	select {
	case run := <-job.started:
		if run != 2 {
			t.Fatalf("restart run=%d", run)
		}
	case <-time.After(time.Second):
		t.Fatal("previous jobs did not restart")
	}
	select {
	case run := <-job.started:
		t.Fatalf("duplicate runner %d", run)
	case <-time.After(20 * time.Millisecond):
	}
	a.stopServices()
}

func (r *recordingJobRuntime) Run(ctx context.Context) error {
	close(r.started)
	<-ctx.Done()
	close(r.stopped)
	return nil
}
func (*recordingJobRuntime) Status() jobs.Status { return jobs.Status{State: "running"} }

func TestRuntimeStartsJobsOnlyAfterPersistedSettingsLoadAndStopsThem(t *testing.T) {
	job := &recordingJobRuntime{started: make(chan struct{}), stopped: make(chan struct{})}
	runtime := &collectorRuntime{hub: collector.NewHub(), store: &discardCollectorStore{}}
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{
		runtime:   runtime,
		settings:  func(context.Context) (domain.Settings, error) { return testSettings(7), nil },
		jobRunner: job,
	}
	if err := a.initializeRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-job.started:
	case <-time.After(time.Second):
		t.Fatal("jobs did not start after settings load")
	}
	cancel()
	a.stopServices()
	select {
	case <-job.stopped:
	case <-time.After(time.Second):
		t.Fatal("jobs did not stop")
	}
}

func TestJobsCannotStartAfterShutdownBegins(t *testing.T) {
	job := &recordingJobRuntime{started: make(chan struct{}), stopped: make(chan struct{})}
	a := &App{jobRunner: job}
	a.stopServices()
	a.startJobs(context.Background())
	select {
	case <-job.started:
		t.Fatal("jobs started after shutdown")
	case <-time.After(20 * time.Millisecond):
	}
}

func (r *recordingSettingsRuntime) Apply(_ context.Context, settings domain.Settings, persist func() error) error {
	r.mu.Lock()
	r.inflight++
	if r.inflight > 1 {
		r.concurrent = true
	}
	r.mu.Unlock()
	time.Sleep(10 * time.Millisecond)
	if err := persist(); err != nil {
		return err
	}
	r.mu.Lock()
	r.active = settings
	r.inflight--
	r.mu.Unlock()
	return nil
}

func TestConcurrentApplySettingsKeepsDatabaseAndActiveReaderConsistent(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := db.Bootstrap(ctx, storage.User{ID: "actor", Username: "Admin", PasswordHash: "x", CreatedAt: now}, storage.Session{TokenHash: []byte("token"), UserID: "actor", CSRFHash: []byte("csrf"), CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	runtime := &recordingSettingsRuntime{}
	a := &App{db: db, settingsRuntime: runtime}
	settings := []domain.Settings{testSettings(7), testSettings(8)}
	start := make(chan struct{})
	errors := make(chan error, 2)
	for _, candidate := range settings {
		candidate := candidate
		go func() { <-start; errors <- a.applySettings(ctx, candidate, "actor") }()
	}
	close(start)
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	stored, err := db.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	active := runtime.active
	concurrent := runtime.concurrent
	runtime.mu.Unlock()
	if concurrent {
		t.Fatal("app allowed concurrent logical settings operations")
	}
	if stored.PanelCount != active.PanelCount || stored.LoggerHost != active.LoggerHost {
		t.Fatalf("stored=%+v active=%+v", stored, active)
	}
}

func TestCollectorRuntimeRestoresPreviousConfigurationWhenAtomicPersistFails(t *testing.T) {
	repository := &locationRecorder{location: time.UTC}
	runtime := &collectorRuntime{hub: collector.NewHub(), store: &discardCollectorStore{}, calendar: repository}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.start(ctx)
	defer runtime.stop()
	initial := testSettings(7)
	if err := runtime.Apply(ctx, initial, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	updated := testSettings(8)
	updated.Timezone = "Asia/Tokyo"
	if err := runtime.Apply(ctx, updated, func() error { return errors.New("audit failed") }); err == nil {
		t.Fatal("settings apply unexpectedly succeeded")
	}
	if active := runtime.activeConfiguration(); active.PanelCount != initial.PanelCount {
		t.Fatalf("active config=%+v, want previous", active)
	}
	if repository.Location().String() != "America/Sao_Paulo" {
		t.Fatalf("location changed on rollback: %s", repository.Location())
	}
}

func TestCollectorRuntimeStopDoesNotReportCollectorAsRunning(t *testing.T) {
	runtime := &collectorRuntime{hub: collector.NewHub(), store: &discardCollectorStore{}}
	runtime.start(context.Background())
	runtime.mu.Lock()
	runtime.current = collector.New(collector.Config{}, nil, nil, runtime.hub)
	runtime.done = make(chan struct{})
	close(runtime.done)
	runtime.mu.Unlock()
	runtime.stop()
	status := runtime.components()
	if status.Collector != "stopped" {
		t.Fatalf("collector status = %q, want stopped", status.Collector)
	}
	if status.CollectorUpdatedAt == "" {
		t.Fatal("collector status has no timestamp")
	}
}

func TestCollectorReconfigureTimeoutDoesNotReportCancelledCollectorAsRunning(t *testing.T) {
	runtime := &collectorRuntime{hub: collector.NewHub(), store: &discardCollectorStore{}, waitTimeout: time.Millisecond}
	runtime.start(context.Background())
	runtime.mu.Lock()
	runtime.current = collector.New(collector.Config{}, nil, nil, runtime.hub)
	runtime.cancel = func() {}
	runtime.done = make(chan struct{})
	done := runtime.done
	runtime.state = "running"
	runtime.mu.Unlock()
	go func() { time.Sleep(20 * time.Millisecond); close(done) }()
	started := time.Now()
	if err := runtime.Apply(context.Background(), testSettings(7), func() error { return nil }); err == nil {
		t.Fatal("reconfiguration unexpectedly succeeded")
	}
	if time.Since(started) < 15*time.Millisecond {
		t.Fatal("reconfigure returned before cancelled collector joined")
	}
	status := runtime.components()
	if status.Collector != "degraded" || status.CollectorError != "stop_timeout" {
		t.Fatalf("collector status after timeout = %#v", status)
	}
}

func TestCollectorStopJoinsWorkerAfterTimeoutBeforeReturning(t *testing.T) {
	runtime := &collectorRuntime{hub: collector.NewHub(), store: &discardCollectorStore{}, waitTimeout: time.Millisecond}
	runtime.start(context.Background())
	done := make(chan struct{})
	runtime.mu.Lock()
	runtime.current = collector.New(collector.Config{}, nil, nil, runtime.hub)
	runtime.cancel = func() {}
	runtime.done = done
	runtime.state = "running"
	runtime.mu.Unlock()
	returned := make(chan struct{})
	go func() { runtime.stop(); close(returned) }()
	select {
	case <-returned:
		t.Fatal("stop returned before worker joined")
	case <-time.After(10 * time.Millisecond):
	}
	close(done)
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("stop did not join worker")
	}
	status := runtime.components()
	if status.Collector != "stopped" || status.CollectorError != "stop_timeout" {
		t.Fatalf("status = %#v", status)
	}
}

type locationRecorder struct {
	mu       sync.RWMutex
	location *time.Location
}

func (r *locationRecorder) ApplyLocation(location *time.Location, apply func() error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if apply != nil {
		if err := apply(); err != nil {
			return err
		}
	}
	r.location = location
	return nil
}

func (r *locationRecorder) SetLocation(location *time.Location) {
	r.mu.Lock()
	r.location = location
	r.mu.Unlock()
}
func (r *locationRecorder) Location() *time.Location {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.location
}

func TestRestartInitializesRepositoryFromPersistedSettingsBeforeRuntime(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "helio.db")
	db, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	settings := testSettings(7)
	if err := db.BootstrapWithSettings(ctx, storage.User{ID: "actor", Username: "Admin", PasswordHash: "x", CreatedAt: now}, storage.Session{TokenHash: []byte("token"), UserID: "actor", CSRFHash: []byte("csrf"), CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}, settings); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	a := New(config.Config{HTTPAddr: "127.0.0.1:0", DatabasePath: path})
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := a.initializeRuntime(runCtx); err != nil {
		t.Fatal(err)
	}
	defer func() { a.stopServices(); _ = a.db.Close() }()
	if got := a.repository.Location().String(); got != settings.Timezone {
		t.Fatalf("restart repository location=%q", got)
	}
}

func TestTimezoneChangeRebuildsCalendarThenUsesNewProducerLocation(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	old := testSettings(7)
	if err := db.BootstrapWithSettings(ctx, storage.User{ID: "actor", Username: "Admin", PasswordHash: "x", CreatedAt: now}, storage.Session{TokenHash: []byte("token"), UserID: "actor", CSRFHash: []byte("csrf"), CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}, old); err != nil {
		t.Fatal(err)
	}
	repository := storage.NewTelemetryRepository(db, time.UTC)
	oldLocation, _ := time.LoadLocation(old.Timezone)
	repository.SetLocation(oldLocation)
	historic := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	for minute := range 2 {
		if err := repository.SaveMinute(ctx, domain.TelemetrySnapshot{ObservedAt: historic.Add(time.Duration(minute) * time.Minute), ACPowerW: 60}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repository.AggregateHour(ctx, historic, historic.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	runtime := &collectorRuntime{hub: collector.NewHub(), store: repository, calendar: repository}
	runtime.start(ctx)
	defer runtime.stop()
	if err := runtime.reconfigure(ctx, old); err != nil {
		t.Fatal(err)
	}
	a := &App{db: db, runtime: runtime, settingsRuntime: runtime}
	updated := old
	updated.Timezone = "Asia/Tokyo"
	if err := a.applySettings(ctx, updated, "actor"); err != nil {
		t.Fatal(err)
	}
	if got := repository.Location().String(); got != updated.Timezone {
		t.Fatalf("repository timezone=%q", got)
	}
	tokyo, _ := time.LoadLocation(updated.Timezone)
	points, err := repository.DailyHistory(ctx, time.Date(2026, 1, 1, 0, 0, 0, 0, tokyo).UTC(), time.Date(2026, 1, 2, 0, 0, 0, 0, tokyo).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0].EnergyWh != 1 {
		t.Fatalf("rebuilt summaries=%#v", points)
	}
	future := time.Date(2026, 1, 2, 2, 0, 0, 0, time.UTC)
	for minute := range 2 {
		if err := repository.SaveMinute(ctx, domain.TelemetrySnapshot{ObservedAt: future.Add(time.Duration(minute) * time.Minute), ACPowerW: 60}); err != nil {
			t.Fatal(err)
		}
	}
	hour, err := repository.AggregateHour(ctx, future, future.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if hour.Hour != "2026-01-02T11:00:00+09:00" {
		t.Fatalf("future producer hour=%q", hour.Hour)
	}
}

type discardCollectorStore struct{}

func (*discardCollectorStore) SaveMinute(context.Context, domain.TelemetrySnapshot) error { return nil }
func (*discardCollectorStore) SaveEvent(context.Context, time.Time, string, any) error    { return nil }

func testSettings(panels int) domain.Settings {
	return domain.Settings{LoggerHost: "192.168.1.50", LoggerSerial: "123", LoggerPort: 8899, ModbusSlave: 1,
		PanelCount: panels, PanelWattage: 610, ActiveMPPT: []int{1}, Latitude: -23.5, Longitude: -46.6,
		Timezone: "America/Sao_Paulo", Currency: "BRL", TariffMinorPerKWh: 95, RetentionDays: 730}
}
