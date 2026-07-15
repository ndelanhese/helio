package app

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/collector"
	"github.com/ndelanhese/helio/internal/config"
	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/storage"
)

type recordingSettingsRuntime struct {
	mu         sync.Mutex
	active     domain.Settings
	inflight   int
	concurrent bool
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
	points, err := repository.DailyHistory(ctx, time.Date(2026, 1, 1, 0, 0, 0, 0, tokyo).UTC(), time.Date(2026, 1, 2, 0, 0, 0, 0, tokyo).UTC(), tokyo)
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
