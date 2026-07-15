package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ndelanhese/helio/internal/api"
	"github.com/ndelanhese/helio/internal/auth"
	"github.com/ndelanhese/helio/internal/collector"
	"github.com/ndelanhese/helio/internal/config"
	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/httpserver"
	"github.com/ndelanhese/helio/internal/jobs"
	"github.com/ndelanhese/helio/internal/sofar"
	"github.com/ndelanhese/helio/internal/storage"
)

type App struct {
	server            *http.Server
	db                *storage.DB
	runtime           *collectorRuntime
	initErr           error
	settings          func(context.Context) (domain.Settings, error)
	settingsMu        sync.Mutex
	settingsRuntime   settingsRuntime
	shutdownContext   context.Context
	shutdownCancel    context.CancelFunc
	stopOnce          sync.Once
	allowPublicLogger bool
	repository        *storage.TelemetryRepository
	jobRunner         jobRuntime
	jobsMu            sync.Mutex
	jobsCancel        context.CancelFunc
	jobsWG            sync.WaitGroup
	stopping          bool
}

type settingsRuntime interface {
	Apply(context.Context, domain.Settings, func() error) error
}

type calendarRuntime interface {
	ApplyLocation(*time.Location, func() error) error
}

type jobRuntime interface {
	Run(context.Context) error
	Status() jobs.Status
}

func New(cfg config.Config) *App {
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":8080"
	}
	if cfg.DatabasePath == "" {
		cfg.DatabasePath = "helio.db"
	}
	db, err := storage.Open(context.Background(), cfg.DatabasePath)
	if err != nil {
		return &App{initErr: err}
	}
	hub := collector.NewHub()
	repository := storage.NewTelemetryRepository(db, time.UTC)
	runtime := &collectorRuntime{hub: hub, store: repository, calendar: repository}
	manager := auth.NewManager(db, auth.WithSecureCookies(cfg.SecureCookies))
	shutdownContext, shutdownCancel := context.WithCancel(context.Background())
	a := &App{db: db, runtime: runtime, settingsRuntime: runtime, shutdownContext: shutdownContext, shutdownCancel: shutdownCancel, allowPublicLogger: cfg.AllowPublicLogger, repository: repository,
		settings: func(ctx context.Context) (domain.Settings, error) { return db.GetSettings(ctx, cfg.AllowPublicLogger) }}
	a.jobRunner = jobs.New(repository, a.settings)
	apiHandler := api.New(api.Dependencies{
		Auth: manager, Store: db, History: repository, Hub: hub,
		Latest: runtime.latest, Reconfigure: runtime.reconfigure,
		ApplySettings: a.applySettings, ShutdownContext: shutdownContext,
		AllowPublicLogger: cfg.AllowPublicLogger,
		Components: func(ctx context.Context) api.ComponentStatus {
			status := runtime.components()
			if a.jobRunner != nil {
				jobStatus := a.jobRunner.Status()
				status.Jobs, status.JobsError = jobStatus.State, jobStatus.ErrorClass
				if !jobStatus.UpdatedAt.IsZero() {
					status.JobsUpdatedAt = jobStatus.UpdatedAt.UTC().Format(time.RFC3339)
				}
			}
			if err := db.Ready(ctx); err != nil {
				status.Database = "offline"
			}
			return status
		},
	})
	ready := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return db.Ready(ctx)
	}
	a.server = &http.Server{Addr: cfg.HTTPAddr, Handler: httpserver.New(httpserver.Dependencies{Ready: ready, API: apiHandler}), ReadHeaderTimeout: 5 * time.Second}
	return a
}

func (a *App) applySettings(ctx context.Context, settings domain.Settings, actorUserID string) error {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	if a.settingsRuntime == nil || a.db == nil {
		return errors.New("settings runtime is unavailable")
	}
	if err := a.settingsRuntime.Apply(ctx, settings, func() error { return a.db.ApplySettings(ctx, settings, actorUserID, a.allowPublicLogger) }); err != nil {
		return err
	}
	if a.runtime != nil {
		a.startJobs(a.runtime.context())
	}
	return nil
}

func (a *App) Run(ctx context.Context) error {
	if a.initErr != nil {
		return a.initErr
	}
	defer a.db.Close()
	listener, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		return err
	}
	errC := make(chan error, 1)
	go func() { errC <- a.server.Serve(listener) }()
	if err := a.initializeRuntime(ctx); err != nil {
		_ = a.server.Close()
		a.stopServices()
		return err
	}
	defer a.stopServices()
	select {
	case err := <-errC:
		a.stopServices()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownDone := make(chan error, 1)
		go func() { shutdownDone <- a.server.Shutdown(shutdown) }()
		a.stopServices()
		return <-shutdownDone
	}
}

func (a *App) initializeRuntime(ctx context.Context) error {
	a.runtime.start(ctx)
	if settings, err := a.settings(ctx); err == nil {
		if err := a.runtime.reconfigure(ctx, settings); err != nil {
			return fmt.Errorf("start collector: %w", err)
		}
		a.startJobs(ctx)
	} else if open, openErr := a.db.BootstrapOpen(ctx); openErr != nil || !open {
		return fmt.Errorf("load settings: %w", err)
	}
	return nil
}

func (a *App) stopServices() {
	a.stopOnce.Do(func() {
		if a.shutdownCancel != nil {
			a.shutdownCancel()
		}
		a.jobsMu.Lock()
		a.stopping = true
		jobsCancel := a.jobsCancel
		a.jobsMu.Unlock()
		if jobsCancel != nil {
			jobsCancel()
		}
		if a.runtime != nil {
			a.runtime.stop()
		}
		a.jobsWG.Wait()
	})
}

func (a *App) startJobs(ctx context.Context) {
	if ctx == nil || a.jobRunner == nil {
		return
	}
	a.jobsMu.Lock()
	defer a.jobsMu.Unlock()
	if a.stopping || a.jobsCancel != nil {
		return
	}
	jobContext, cancel := context.WithCancel(ctx)
	a.jobsCancel = cancel
	a.jobsWG.Add(1)
	go func() { defer a.jobsWG.Done(); _ = a.jobRunner.Run(jobContext) }()
}

type collectorRuntime struct {
	mu          sync.RWMutex
	configureMu sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	current     *collector.Collector
	active      domain.Settings
	hub         *collector.Hub
	store       collector.Store
	calendar    calendarRuntime
	state       string
	stateAt     time.Time
	errorClass  string
	waitTimeout time.Duration
}

func (r *collectorRuntime) start(ctx context.Context) {
	r.mu.Lock()
	r.ctx, r.state, r.stateAt, r.errorClass = ctx, "idle", time.Now().UTC(), ""
	r.mu.Unlock()
}
func (r *collectorRuntime) context() context.Context {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ctx
}
func (r *collectorRuntime) stop() {
	r.configureMu.Lock()
	defer r.configureMu.Unlock()
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.ctx, r.cancel, r.done, r.current = nil, nil, nil, nil
	r.state, r.stateAt, r.errorClass = "stopped", time.Now().UTC(), ""
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(r.stopWait()):
		}
	}
}

func (r *collectorRuntime) stopWait() time.Duration {
	if r.waitTimeout > 0 {
		return r.waitTimeout
	}
	return 5 * time.Second
}
func (r *collectorRuntime) latest() collector.State {
	r.mu.RLock()
	current := r.current
	r.mu.RUnlock()
	if current == nil {
		return collector.State{}
	}
	return current.Latest()
}

func (r *collectorRuntime) reconfigure(_ context.Context, settings domain.Settings) error {
	return r.Apply(context.Background(), settings, func() error { return nil })
}

func (r *collectorRuntime) Apply(_ context.Context, settings domain.Settings, persist func() error) error {
	r.configureMu.Lock()
	defer r.configureMu.Unlock()
	serial, err := strconv.ParseUint(settings.LoggerSerial, 10, 32)
	if err != nil {
		return errors.New("invalid logger serial")
	}
	location, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return errors.New("invalid settings timezone")
	}
	reader := sofar.NewHardwareReader(sofar.HardwareConfig{Address: net.JoinHostPort(settings.LoggerHost, strconv.Itoa(settings.LoggerPort)), Serial: uint32(serial), SlaveID: byte(settings.ModbusSlave), ActiveMPPT: append([]int(nil), settings.ActiveMPPT...)})
	next := collector.New(collector.Config{}, reader, r.store, r.hub)
	r.mu.Lock()
	if r.ctx == nil {
		r.mu.Unlock()
		return errors.New("collector runtime is not started")
	}
	oldCancel, oldDone, oldCurrent, oldActive := r.cancel, r.done, r.current, r.active
	r.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
	}
	if oldDone != nil {
		select {
		case <-oldDone:
		case <-time.After(r.stopWait()):
			r.mu.Lock()
			if r.done == oldDone {
				r.current, r.cancel, r.done, r.active = nil, nil, nil, domain.Settings{}
				r.state, r.stateAt, r.errorClass = "degraded", time.Now().UTC(), "stop_timeout"
			}
			r.mu.Unlock()
			return errors.New("previous collector did not stop")
		}
	}
	apply := persist
	if apply == nil {
		apply = func() error { return nil }
	}
	if r.calendar != nil {
		err = r.calendar.ApplyLocation(location, apply)
	} else {
		err = apply()
	}
	if err != nil {
		if oldCancel != nil {
			r.startCollectorLocked(oldCurrent, oldActive)
		}
		return err
	}
	r.mu.Lock()
	if r.ctx == nil {
		r.mu.Unlock()
		return errors.New("collector runtime stopped")
	}
	r.mu.Unlock()
	r.startCollectorLocked(next, settings)
	return nil
}

func (r *collectorRuntime) startCollectorLocked(next *collector.Collector, settings domain.Settings) {
	r.mu.Lock()
	if r.ctx == nil {
		r.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(r.ctx)
	done := make(chan struct{})
	r.current, r.cancel, r.done, r.active = next, cancel, done, settings
	r.state, r.stateAt, r.errorClass = "running", time.Now().UTC(), ""
	r.mu.Unlock()
	go func() {
		err := next.Run(runCtx)
		close(done)
		r.mu.Lock()
		if r.done == done {
			r.current, r.cancel, r.done = nil, nil, nil
			r.state, r.stateAt = "stopped", time.Now().UTC()
			if err != nil && !errors.Is(err, context.Canceled) {
				r.state, r.errorClass = "degraded", "runtime"
			}
		}
		r.mu.Unlock()
	}()
}

func (r *collectorRuntime) activeConfiguration() domain.Settings {
	r.mu.RLock()
	defer r.mu.RUnlock()
	settings := r.active
	settings.ActiveMPPT = append([]int(nil), settings.ActiveMPPT...)
	return settings
}

func (r *collectorRuntime) components() api.ComponentStatus {
	state := r.latest()
	status := api.ComponentStatus{Database: "ok", Logger: "unknown", Collector: "idle", Weather: "unconfigured"}
	r.mu.RLock()
	status.Collector = r.state
	status.CollectorError = r.errorClass
	if !r.stateAt.IsZero() {
		status.CollectorUpdatedAt = r.stateAt.UTC().Format(time.RFC3339)
	}
	r.mu.RUnlock()
	if status.Collector == "" {
		status.Collector = "idle"
	}
	if state.Snapshot != nil && !state.Stale {
		status.Logger = "online"
	}
	if state.Stale || state.LastError != "" {
		status.Logger = "offline"
	}
	if !state.LastSuccess.IsZero() {
		status.LastSuccess = state.LastSuccess.UTC().Format(time.RFC3339)
		status.LoggerUpdatedAt = status.LastSuccess
	}
	return status
}
