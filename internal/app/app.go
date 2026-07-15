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
	"github.com/ndelanhese/helio/internal/sofar"
	"github.com/ndelanhese/helio/internal/storage"
)

type App struct {
	server   *http.Server
	db       *storage.DB
	runtime  *collectorRuntime
	initErr  error
	settings func(context.Context) (domain.Settings, error)
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
	runtime := &collectorRuntime{hub: hub, store: repository}
	manager := auth.NewManager(db, auth.WithSecureCookies(cfg.SecureCookies))
	apiHandler := api.New(api.Dependencies{
		Auth: manager, Store: db, History: repository, Hub: hub,
		Latest: runtime.latest, Reconfigure: runtime.reconfigure,
		AllowPublicLogger: cfg.AllowPublicLogger,
		Components: func(ctx context.Context) api.ComponentStatus {
			status := runtime.components()
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
	return &App{
		server: &http.Server{Addr: cfg.HTTPAddr, Handler: httpserver.New(httpserver.Dependencies{Ready: ready, API: apiHandler}), ReadHeaderTimeout: 5 * time.Second},
		db:     db, runtime: runtime,
		settings: func(ctx context.Context) (domain.Settings, error) { return db.GetSettings(ctx, cfg.AllowPublicLogger) },
	}
}

func (a *App) Run(ctx context.Context) error {
	if a.initErr != nil {
		return a.initErr
	}
	a.runtime.start(ctx)
	defer a.runtime.stop()
	defer a.db.Close()
	if settings, err := a.settings(ctx); err == nil {
		if err := a.runtime.reconfigure(ctx, settings); err != nil {
			return fmt.Errorf("start collector: %w", err)
		}
	} else if open, openErr := a.db.BootstrapOpen(ctx); openErr != nil || !open {
		return fmt.Errorf("load settings: %w", err)
	}
	errC := make(chan error, 1)
	go func() { errC <- a.server.ListenAndServe() }()
	select {
	case err := <-errC:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return a.server.Shutdown(shutdown)
	}
}

type collectorRuntime struct {
	mu          sync.RWMutex
	configureMu sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	current     *collector.Collector
	hub         *collector.Hub
	store       collector.Store
}

func (r *collectorRuntime) start(ctx context.Context) { r.mu.Lock(); r.ctx = ctx; r.mu.Unlock() }
func (r *collectorRuntime) stop() {
	r.configureMu.Lock()
	defer r.configureMu.Unlock()
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.ctx, r.cancel, r.done = nil, nil, nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
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
	r.configureMu.Lock()
	defer r.configureMu.Unlock()
	serial, err := strconv.ParseUint(settings.LoggerSerial, 10, 32)
	if err != nil {
		return errors.New("invalid logger serial")
	}
	reader := sofar.NewHardwareReader(sofar.HardwareConfig{Address: net.JoinHostPort(settings.LoggerHost, strconv.Itoa(settings.LoggerPort)), Serial: uint32(serial), SlaveID: byte(settings.ModbusSlave)})
	next := collector.New(collector.Config{}, reader, r.store, r.hub)
	r.mu.Lock()
	if r.ctx == nil {
		r.mu.Unlock()
		return errors.New("collector runtime is not started")
	}
	oldCancel, oldDone := r.cancel, r.done
	r.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
	}
	if oldDone != nil {
		select {
		case <-oldDone:
		case <-time.After(5 * time.Second):
			return errors.New("previous collector did not stop")
		}
	}
	r.mu.Lock()
	if r.ctx == nil {
		r.mu.Unlock()
		return errors.New("collector runtime stopped")
	}
	runCtx, cancel := context.WithCancel(r.ctx)
	done := make(chan struct{})
	r.current, r.cancel, r.done = next, cancel, done
	r.mu.Unlock()
	go func() { defer close(done); _ = next.Run(runCtx) }()
	return nil
}

func (r *collectorRuntime) components() api.ComponentStatus {
	state := r.latest()
	status := api.ComponentStatus{Database: "ok", Logger: "unknown", Collector: "idle"}
	r.mu.RLock()
	running := r.current != nil
	r.mu.RUnlock()
	if running {
		status.Collector = "running"
	}
	if state.Snapshot != nil && !state.Stale {
		status.Logger = "online"
	}
	if state.Stale || state.LastError != "" {
		status.Logger = "offline"
	}
	if !state.LastSuccess.IsZero() {
		status.LastSuccess = state.LastSuccess.UTC().Format(time.RFC3339)
	}
	return status
}
