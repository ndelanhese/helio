package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrationIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helio.db")
	first, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationRejectsSchemaNewerThanBinary(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "helio.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx,
		"INSERT INTO schema_migrations(version, applied_at) VALUES (4, CURRENT_TIMESTAMP)",
	); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(ctx, path); err == nil {
		t.Fatal("Open accepted a database schema newer than the binary")
	}
}

func TestMigrationV2BackfillsPreciseObservationFromMinuteBucket(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "helio-v1.db")
	v1, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v1.ExecContext(ctx, migrations[0].sql); err != nil {
		t.Fatal(err)
	}
	if _, err := v1.ExecContext(ctx, `
		CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
		INSERT INTO schema_migrations(version, applied_at) VALUES (1, CURRENT_TIMESTAMP);
		INSERT INTO telemetry_minute(
			observed_at, ac_power_w, energy_today_wh, energy_lifetime_wh,
			pv1_voltage_v, pv1_current_a, pv1_power_w, pv2_active,
			pv2_voltage_v, pv2_current_a, pv2_power_w, grid_voltage_v,
			grid_frequency_hz, status, fault_codes_json
		) VALUES ('2026-01-02T10:00:00Z', 1, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 'normal', '[]')`); err != nil {
		t.Fatal(err)
	}
	if err := v1.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var bucket, source string
	if err := db.sql.QueryRowContext(ctx, `SELECT observed_at, observed_at_utc FROM telemetry_minute`).Scan(&bucket, &source); err != nil {
		t.Fatal(err)
	}
	if bucket != "2026-01-02T10:00:00.000000000Z" || source != bucket {
		t.Fatalf("backfilled bucket=%q source=%q", bucket, source)
	}
	var version int
	if err := db.sql.QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 3 {
		t.Fatalf("schema version=%d, want 3", version)
	}
}
