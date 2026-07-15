package storage

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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
	in := domain.Settings{LoggerHost: "10.0.0.50", LoggerSerial: "000123", LoggerPort: 8899, ModbusSlave: 1, PanelCount: 7, PanelWattage: 610, ActiveMPPT: []int{2, 1}, Latitude: -23.5505, Longitude: -46.6333, Timezone: "America/Sao_Paulo", Currency: "BRL", TariffMinorPerKWh: 95, RetentionDays: 730}
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

func TestTimezoneChangeInvalidatesCalendarAnalysisAndUnderproductionEvidenceAtomically(t *testing.T) {
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
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO daily_analysis(day,expected_wh,actual_wh,ratio,confidence,evidence_json,qualifying,updated_at) VALUES('2026-01-01',100,50,.5,'high','[]',1,'2026-01-02T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO alert_rule_state(rule,state_json,updated_at) VALUES('persistent_underproduction','{"consecutive":3,"lastKey":"2026-01-01","lastEvaluatedAt":"2026-01-02T00:00:00Z"}','2026-01-02T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO alerts(id,rule,state,severity,opened_at,evidence_json) VALUES('under','persistent_underproduction','open','warning','2026-01-02T00:00:00Z','{"values":{"ratio":0.5}}')`); err != nil {
		t.Fatal(err)
	}
	updated := old
	updated.Timezone = "Asia/Tokyo"
	if err := db.ApplySettings(ctx, updated, "actor", false); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"daily_analysis", "alert_rule_state", "alerts"} {
		var count int
		query := `SELECT COUNT(*) FROM ` + table
		if table != "daily_analysis" {
			query += ` WHERE rule='persistent_underproduction'`
		}
		if err := db.sql.QueryRowContext(ctx, query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s retained %d stale calendar rows", table, count)
		}
	}
	var auditCount int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM action_audit WHERE action='analysis.invalidate_timezone'`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("invalidation audit count=%d", auditCount)
	}
}

func TestTimezoneInvalidationRollsBackWithSettingsWhenItsAuditFails(t *testing.T) {
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
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO daily_analysis(day,expected_wh,actual_wh,ratio,confidence,evidence_json,qualifying,updated_at) VALUES('2026-01-01',100,50,.5,'high','[]',1,'2026-01-02T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `CREATE TRIGGER reject_calendar_audit BEFORE INSERT ON action_audit WHEN NEW.action='analysis.invalidate_timezone' BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	updated := old
	updated.Timezone = "Asia/Tokyo"
	if err := db.ApplySettings(ctx, updated, "actor", false); err == nil {
		t.Fatal("timezone change succeeded without invalidation audit")
	}
	got, err := db.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Timezone != old.Timezone {
		t.Fatalf("timezone committed on rollback: %s", got.Timezone)
	}
	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM daily_analysis`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("analysis removed on rollback: %d", count)
	}
}

func TestCalendarRebuildSplitsHourlyIntervalsAtFractionalZoneMidnight(t *testing.T) {
	for _, tc := range []struct {
		zone                    string
		hour                    time.Time
		firstStart, secondStart time.Time
		firstMinutes            float64
		firstProductive         int
	}{
		{"Australia/Adelaide", time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC), time.Date(2025, 12, 31, 13, 30, 0, 0, time.UTC), time.Date(2026, 1, 1, 13, 30, 0, 0, time.UTC), 30, 16},
		{"Asia/Kathmandu", time.Date(2026, 1, 1, 17, 30, 0, 0, time.UTC), time.Date(2025, 12, 31, 18, 15, 0, 0, time.UTC), time.Date(2026, 1, 1, 18, 15, 0, 0, time.UTC), 45, 23},
	} {
		t.Run(tc.zone, func(t *testing.T) {
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
			if _, err := db.sql.ExecContext(ctx, `INSERT INTO hourly_summary(hour,energy_wh,peak_power_w,coverage_pct,productive_minutes) VALUES(?,?,?,?,?)`, tc.hour.Format(time.RFC3339), 120, 500, 80, 31); err != nil {
				t.Fatal(err)
			}
			updated := old
			updated.Timezone = tc.zone
			if err := db.ApplySettings(ctx, updated, "actor", false); err != nil {
				t.Fatal(err)
			}
			loc, _ := time.LoadLocation(tc.zone)
			repo := NewTelemetryRepository(db, loc)
			points, err := repo.DailyHistory(ctx, tc.firstStart, tc.secondStart.AddDate(0, 0, 1))
			if err != nil {
				t.Fatal(err)
			}
			if len(points) != 2 {
				t.Fatalf("daily points=%#v", points)
			}
			minutes := []float64{tc.firstMinutes, 60 - tc.firstMinutes}
			for index, wantStart := range []time.Time{tc.firstStart, tc.secondStart} {
				point := points[index]
				if !point.At.Equal(wantStart) || math.Abs(point.EnergyWh-(120*minutes[index]/60)) > 1e-9 || math.Abs(point.CoveragePct-(80*minutes[index]/1440.0)) > 1e-9 || point.PeakPowerW != 500 {
					t.Fatalf("point[%d]=%#v", index, point)
				}
			}
			if points[0].ProductiveMinutes != tc.firstProductive || points[1].ProductiveMinutes != 31-tc.firstProductive {
				t.Fatalf("productive split=%d,%d", points[0].ProductiveMinutes, points[1].ProductiveMinutes)
			}
			months, err := repo.MonthlyHistory(ctx, tc.firstStart, tc.secondStart.AddDate(0, 1, 0))
			if err != nil {
				t.Fatal(err)
			}
			if len(months) != 1 || !months[0].At.Equal(tc.firstStart) || months[0].EnergyWh != 120 || months[0].ProductiveMinutes != 31 || months[0].PeakPowerW != 500 || math.Abs(months[0].CoveragePct-(80*60/(2*1440.0))) > 1e-9 {
				t.Fatalf("monthly points=%#v", months)
			}
		})
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
	return domain.Settings{LoggerHost: "10.0.0.50", LoggerSerial: "123", LoggerPort: 8899, ModbusSlave: 1, PanelCount: 7, PanelWattage: 610, ActiveMPPT: []int{1}, InstalledPowerW: 4270, Latitude: -23.5505, Longitude: -46.6333, Timezone: "America/Sao_Paulo", Currency: "BRL", TariffMinorPerKWh: 95, RetentionDays: 730}
}
