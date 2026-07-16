package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestFinanceMigrationCreatesTables(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, table := range []string{
		"tariff_proposals", "tariff_versions", "billing_cycles", "credit_lots", "bill_reconciliations",
	} {
		var got string
		err := db.sql.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
		).Scan(&got)
		if err != nil || got != table {
			t.Fatalf("table %s: %v", table, err)
		}
	}
}

func TestFinanceMigrationEnforcesTariffImmutabilityAndConstraints(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.sql.ExecContext(ctx,
		`INSERT INTO users(id, username, password_hash, created_at) VALUES ('user-1', 'finance', 'hash', '2026-01-01')`,
	); err != nil {
		t.Fatal(err)
	}
	result, err := db.sql.ExecContext(ctx, `
		INSERT INTO tariff_versions (
			distributor, effective_from, effective_to,
			consumption_te_micros_per_kwh, consumption_tusd_micros_per_kwh,
			compensation_te_micros_per_kwh, compensation_tusd_micros_per_kwh,
			flag_micros_per_kwh, availability_kwh, cip_minor, source_url,
			retrieved_at, approved_at, approved_by
		) VALUES ('COPEL', '2026-06-24', '2027-06-23', 389503, 538944, 0, 0, 0, 100, 0,
			'https://example.test/tariff', '2026-06-24T00:00:00Z', '2026-06-24T00:00:00Z', 'user-1')`,
	)
	if err != nil {
		t.Fatal(err)
	}
	tariffID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.sql.ExecContext(ctx, `UPDATE tariff_versions SET cip_minor = 1 WHERE id = ?`, tariffID); err == nil {
		t.Fatal("approved tariff version update was not rejected")
	}
	if _, err := db.sql.ExecContext(ctx, `DELETE FROM tariff_versions WHERE id = ?`, tariffID); err == nil {
		t.Fatal("approved tariff version delete was not rejected")
	}
	if _, err := db.sql.ExecContext(ctx, `
		INSERT INTO tariff_versions (
			distributor, effective_from, effective_to,
			consumption_te_micros_per_kwh, consumption_tusd_micros_per_kwh,
			compensation_te_micros_per_kwh, compensation_tusd_micros_per_kwh,
			flag_micros_per_kwh, availability_kwh, cip_minor, source_url,
			retrieved_at, approved_at, approved_by
		) VALUES ('COPEL', '2026-06-24', '2027-06-23', -1, 538944, 0, 0, 0, 100, 0,
			'https://example.test/tariff', '2026-06-24T00:00:00Z', '2026-06-24T00:00:00Z', 'user-1')`,
	); err == nil {
		t.Fatal("negative tariff rate was not rejected")
	}
	if _, err := db.sql.ExecContext(ctx, `
		INSERT INTO billing_cycles (
			reading_start, reading_end, active_consumption_kwh, injected_kwh,
			credits_used_kwh, credit_balance_kwh, total_paid_minor, tariff_version_id, created_at
		) VALUES ('2026-06-24', '2026-07-23', 1, 0, 0, 0, 1, 9999, '2026-07-23T00:00:00Z')`,
	); err == nil {
		t.Fatal("billing cycle with missing tariff version was not rejected")
	}
}

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

func TestMigrationV7CorrectsEnergyCounterWordOrder(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "helio-v6.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 7 {
		t.Fatal("migration 0007 is missing")
	}
	for _, migration := range migrations[:6] {
		if _, err := raw.ExecContext(ctx, migration.sql); err != nil {
			t.Fatalf("apply v%d: %v", migration.version, err)
		}
	}
	if _, err := raw.ExecContext(ctx, `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 6; version++ {
		if _, err := raw.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at) VALUES(?,CURRENT_TIMESTAMP)`, version); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := raw.ExecContext(ctx, `INSERT INTO telemetry_minute(
		observed_at, observed_at_utc, ac_power_w, energy_today_wh, energy_lifetime_wh,
		pv1_voltage_v, pv1_current_a, pv1_power_w, pv2_active, pv2_voltage_v,
		pv2_current_a, pv2_power_w, grid_voltage_v, grid_frequency_hz, status, fault_codes_json
	) VALUES ('2026-07-15T18:53:00.000000000Z', '2026-07-15T18:53:00.000000000Z', 1790, 1000734720, 99490201700, 0, 0, 0, 0, 0, 0, 0, 0, 60, 'normal', '[]')`); err != nil {
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
	var today, lifetime float64
	if err := db.sql.QueryRowContext(ctx, `SELECT energy_today_wh, energy_lifetime_wh FROM telemetry_minute`).Scan(&today, &lifetime); err != nil {
		t.Fatal(err)
	}
	if today != 15270 || lifetime != 8071700 {
		t.Fatalf("energy today=%v lifetime=%v, want 15270 and 8071700", today, lifetime)
	}
}
