package storage

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ndelanhese/helio/internal/config"
	"github.com/ndelanhese/helio/internal/domain"
)

func TestSettingsVersionedRoundTripAndUpsert(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	in := domain.Settings{LoggerHost: "192.168.1.50", LoggerSerial: "000123", LoggerPort: 8899, ModbusSlave: 1, PanelCount: 7, PanelWattage: 610, ActiveMPPT: []int{2, 1}, Latitude: -23.5505, Longitude: -46.6333, Timezone: "America/Sao_Paulo", Currency: "BRL", TariffMinorPerKWh: 95, RetentionDays: 730}
	want, err := config.ValidateSettings(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutSettings(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v", got)
	}

	in.PanelCount = 8
	if err := db.PutSettings(ctx, in); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM settings WHERE key='system'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("settings rows = %d", count)
	}
	got, err = db.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.InstalledPowerW != 4880 {
		t.Fatalf("upsert installed power = %d", got.InstalledPowerW)
	}
	var raw string
	if err := db.sql.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key='system'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"version":1`) {
		t.Fatalf("unversioned JSON: %s", raw)
	}
}

func TestSettingsGetMissing(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.GetSettings(context.Background())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v", err)
	}
}

func TestSettingsRejectsInvalidInputAndStrictStoredJSON(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	invalid := domain.Settings{LoggerHost: "8.8.8.8"}
	if err := db.PutSettings(ctx, invalid); err == nil {
		t.Fatal("expected invalid input rejection")
	}

	bad := `{"version":1,"settings":{},"future":true}`
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO settings(key,value_json,updated_at) VALUES('system',?,?)`, bad, "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetSettings(ctx); err == nil {
		t.Fatal("expected unknown stored field rejection")
	}

	if _, err := db.sql.ExecContext(ctx, `UPDATE settings SET value_json=? WHERE key='system'`, `{"version":2,"settings":{}}`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetSettings(ctx); err == nil {
		t.Fatal("expected unsupported version rejection")
	}
}
