package storage

import (
	"context"
	"encoding/json"
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

func TestApplySettingsRollsBackSettingsWhenRequiredAuditFails(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	initial := validStoredSettings()
	if err := db.PutSettings(ctx, initial); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO daily_summary(day,energy_wh,peak_power_w,productive_minutes,coverage_pct) VALUES('2025-12-31',100,50,10,90)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,created_at) VALUES('actor','Admin','x','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `CREATE TRIGGER reject_settings_audit BEFORE INSERT ON action_audit WHEN NEW.action='settings.update' BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	updated := initial
	updated.PanelCount = 8
	updated.Timezone = "Asia/Tokyo"
	if err := db.ApplySettings(ctx, updated, "actor", false); err == nil {
		t.Fatal("ApplySettings succeeded without required audit")
	}
	got, err := db.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.PanelCount != initial.PanelCount {
		t.Fatalf("settings committed without audit: panelCount=%d", got.PanelCount)
	}
	var day string
	if err := db.sql.QueryRowContext(ctx, `SELECT day FROM daily_summary`).Scan(&day); err != nil {
		t.Fatal(err)
	}
	if day != "2025-12-31" {
		t.Fatalf("calendar summaries changed on rollback: %q", day)
	}
}

func TestApplySettingsRebuildsDailyAndMonthlyFromPermanentHourlyRows(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := validStoredSettings()
	if err := db.PutSettings(ctx, old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,created_at) VALUES('actor','Admin','x','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO hourly_summary(hour,energy_wh,peak_power_w,coverage_pct,productive_minutes) VALUES('2026-01-01T01:00:00Z',100,50,90,17)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO daily_summary(day,energy_wh,peak_power_w,productive_minutes,coverage_pct) VALUES('2025-12-31',999,999,999,99)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO monthly_summary(month,energy_wh,peak_power_w,productive_minutes,coverage_pct) VALUES('2025-12',999,999,999,99)`); err != nil {
		t.Fatal(err)
	}
	updated := old
	updated.Timezone = "Asia/Tokyo"
	if err := db.ApplySettings(ctx, updated, "actor", false); err != nil {
		t.Fatal(err)
	}
	var day, month string
	var dayEnergy, monthEnergy float64
	var productive int
	if err := db.sql.QueryRowContext(ctx, `SELECT day,energy_wh,productive_minutes FROM daily_summary`).Scan(&day, &dayEnergy, &productive); err != nil {
		t.Fatal(err)
	}
	if err := db.sql.QueryRowContext(ctx, `SELECT month,energy_wh FROM monthly_summary`).Scan(&month, &monthEnergy); err != nil {
		t.Fatal(err)
	}
	if day != "2026-01-01" || month != "2026-01" || dayEnergy != 100 || monthEnergy != 100 || productive != 17 {
		t.Fatalf("rebuilt day=%q/%v month=%q/%v", day, dayEnergy, month, monthEnergy)
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

func TestSettingsGetObeysPublicHostPolicy(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	in := validStoredSettings()
	in.LoggerHost = "8.8.8.8"
	if err := db.PutSettings(ctx, in, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetSettings(ctx); err == nil {
		t.Fatal("expected public host to fail without read override")
	}
	if _, err := db.GetSettings(ctx, true); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsGetRejectsNoncanonicalStoredValues(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		mutate func(*domain.Settings)
	}{
		{"serial", func(s *domain.Settings) { s.LoggerSerial = "000123" }},
		{"MPPT order", func(s *domain.Settings) { s.ActiveMPPT = []int{2, 1} }},
		{"retention default form", func(s *domain.Settings) { s.RetentionDays = 0 }},
		{"derived power", func(s *domain.Settings) { s.InstalledPowerW = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := Open(ctx, filepath.Join(t.TempDir(), "settings.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			settings := validStoredSettings()
			tc.mutate(&settings)
			payload, err := json.Marshal(settingsEnvelope{Version: settingsVersion, Settings: settings})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.sql.ExecContext(ctx, `INSERT INTO settings(key,value_json,updated_at) VALUES('system',?,?)`, payload, "2026-01-01T00:00:00Z"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.GetSettings(ctx); err == nil {
				t.Fatal("expected noncanonical stored settings rejection")
			}
		})
	}
}

func validStoredSettings() domain.Settings {
	return domain.Settings{LoggerHost: "192.168.1.50", LoggerSerial: "123", LoggerPort: 8899, ModbusSlave: 1, PanelCount: 7, PanelWattage: 610, ActiveMPPT: []int{1}, InstalledPowerW: 4270, Latitude: -23.5505, Longitude: -46.6333, Timezone: "America/Sao_Paulo", Currency: "BRL", TariffMinorPerKWh: 95, RetentionDays: 730}
}
