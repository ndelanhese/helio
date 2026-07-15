package storage

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOpenMigratesAndEnablesWAL(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var mode string
	if err := db.sql.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode=%q", mode)
	}

	for _, table := range []string{
		"users", "sessions", "settings", "telemetry_minute", "telemetry_events",
		"weather_hourly", "hourly_summary", "daily_summary", "monthly_summary",
		"daily_analysis", "alerts", "recommendations", "action_audit",
	} {
		var got string
		err := db.sql.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&got)
		if err != nil || got != table {
			t.Fatalf("table %s: %v", table, err)
		}
	}
}

func TestOpenConfiguresConnectionPragmas(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for query, want := range map[string]int{
		"PRAGMA foreign_keys": 1,
		"PRAGMA busy_timeout": 5000,
		"PRAGMA synchronous":  1,
	} {
		var got int
		if err := db.sql.QueryRowContext(ctx, query).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s=%d, want %d", query, got, want)
		}
	}
}

func TestSynchronousNormalSurvivesPhysicalConnectionRecycling(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// With no idle slots, each completed operation closes its physical
	// connection. The following query therefore runs on a fresh connection.
	db.sql.SetMaxIdleConns(0)
	var got int
	if err := db.sql.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("synchronous=%d after physical connection recycling, want NORMAL (1)", got)
	}
}

func TestReadyReflectsConnectionState(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Ready(ctx); err == nil {
		t.Fatal("Ready succeeded after Close")
	}
}

func TestSchemaEnforcesKeyConstraints(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.sql.ExecContext(ctx,
		`INSERT INTO users(id, username, password_hash, created_at) VALUES ('one', 'Admin', 'hash', '2026-01-01')`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx,
		`INSERT INTO users(id, username, password_hash, created_at) VALUES ('two', 'admin', 'hash', '2026-01-01')`,
	); err == nil {
		t.Fatal("case-insensitive username uniqueness was not enforced")
	}
	if _, err := db.sql.ExecContext(ctx,
		`INSERT INTO sessions(token_hash, user_id, csrf_hash, created_at, last_seen_at, expires_at)
		 VALUES (x'01', 'missing', x'02', '2026-01-01', '2026-01-01', '2026-01-02')`,
	); err == nil {
		t.Fatal("session foreign key was not enforced")
	}
	if _, err := db.sql.ExecContext(ctx,
		`INSERT INTO alerts(id, rule, state, severity, opened_at, evidence_json)
		 VALUES ('a1', 'offline', 'open', 'warning', '2026-01-01', '{}'),
		        ('a2', 'offline', 'open', 'warning', '2026-01-02', '{}')`,
	); err == nil {
		t.Fatal("one-open-alert-per-rule constraint was not enforced")
	}
}

func TestBackupCanBeReopened(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(ctx, filepath.Join(dir, "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.sql.ExecContext(ctx,
		`INSERT INTO settings(key, value_json, updated_at) VALUES ('site', '{"name":"home"}', '2026-01-01')`,
	); err != nil {
		t.Fatal(err)
	}

	backupPath := filepath.Join(dir, "helio-backup.db")
	out, err := os.Create(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Backup(ctx, out); err != nil {
		_ = out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}

	backup, err := Open(ctx, backupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var value string
	if err := backup.sql.QueryRowContext(ctx,
		"SELECT value_json FROM settings WHERE key = 'site'",
	).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != `{"name":"home"}` {
		t.Fatalf("value_json=%q", value)
	}
}

type firstWriteGate struct {
	once    sync.Once
	started chan struct{}
	release chan struct{}
	buffer  bytes.Buffer
}

func (w *firstWriteGate) Write(p []byte) (int, error) {
	w.once.Do(func() {
		close(w.started)
		<-w.release
	})
	return w.buffer.Write(p)
}

func TestBackupIsAConsistentSnapshotDuringConcurrentInsert(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(ctx, filepath.Join(dir, "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO telemetry_events(observed_at, kind, payload_json) VALUES ('2026-07-14T12:00:00Z', 'before', '{}')`); err != nil {
		t.Fatal(err)
	}

	gate := &firstWriteGate{started: make(chan struct{}), release: make(chan struct{})}
	backupDone := make(chan error, 1)
	go func() { backupDone <- db.Backup(ctx, gate) }()
	select {
	case <-gate.started:
	case <-time.After(5 * time.Second):
		t.Fatal("backup did not start streaming")
	}
	insertDone := make(chan error, 1)
	go func() {
		_, err := db.sql.ExecContext(ctx, `INSERT INTO telemetry_events(observed_at, kind, payload_json) VALUES ('2026-07-14T12:01:00Z', 'during', '{}')`)
		insertDone <- err
	}()
	select {
	case err := <-insertDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent insert remained blocked after snapshot preparation")
	}
	close(gate.release)
	if err := <-backupDone; err != nil {
		t.Fatal(err)
	}

	backupPath := filepath.Join(dir, "consistent.db")
	if err := os.WriteFile(backupPath, gate.buffer.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := sql.Open("sqlite", backupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	var count int
	if err := snapshot.QueryRow(`SELECT count(*) FROM telemetry_events`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("snapshot row count=%d, want pre-insert set of 1", count)
	}
}

func TestPreparedBackupRemovesSnapshotOnClose(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	snapshot, err := db.PrepareBackup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	if leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(db.path), ".helio-backup-*.db")); err != nil || len(leftovers) != 0 {
		t.Fatalf("prepared backup leaked: %v err=%v", leftovers, err)
	}
}
