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
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	newerVersion := migrations[len(migrations)-1].version + 1
	if _, err := db.sql.ExecContext(ctx,
		"INSERT INTO schema_migrations(version, applied_at) VALUES (?, CURRENT_TIMESTAMP)", newerVersion,
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
	wantVersion := migrations[len(migrations)-1].version
	if version != wantVersion {
		t.Fatalf("schema version=%d, want %d", version, wantVersion)
	}
}

func TestMigrationV6ClearsLegacyResolvedOpeningEvidence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "helio-v5.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 6 {
		t.Fatal("migration 0006 is missing")
	}
	for _, migration := range migrations[:5] {
		if _, err := raw.ExecContext(ctx, migration.sql); err != nil {
			t.Fatalf("apply v%d: %v", migration.version, err)
		}
	}
	if _, err := raw.ExecContext(ctx, `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 5; version++ {
		if _, err := raw.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at) VALUES(?,CURRENT_TIMESTAMP)`, version); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := raw.ExecContext(ctx, `INSERT INTO alerts(id,rule,state,severity,opened_at,resolved_at,evidence_json)
		VALUES('legacy','logger_offline','resolved','critical','2026-07-01T00:00:00Z','2026-07-01T00:05:00Z','{"values":{"failed_polls":3}}')`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var evidence string
	if err := db.sql.QueryRowContext(ctx, `SELECT evidence_json FROM alerts WHERE id='legacy'`).Scan(&evidence); err != nil {
		t.Fatal(err)
	}
	if evidence != `{}` {
		t.Fatalf("legacy resolved evidence=%s", evidence)
	}
}
