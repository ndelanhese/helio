package app

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/collector"
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
	runtime := &collectorRuntime{hub: collector.NewHub(), store: &discardCollectorStore{}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.start(ctx)
	defer runtime.stop()
	initial := testSettings(7)
	if err := runtime.Apply(ctx, initial, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	updated := testSettings(8)
	if err := runtime.Apply(ctx, updated, func() error { return errors.New("audit failed") }); err == nil {
		t.Fatal("settings apply unexpectedly succeeded")
	}
	if active := runtime.activeConfiguration(); active.PanelCount != initial.PanelCount {
		t.Fatalf("active config=%+v, want previous", active)
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
