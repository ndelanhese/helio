package storage

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"syscall"
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
	snapshot, err := db.PrepareBackup(ctx)
	if err != nil {
		_ = out.Close()
		t.Fatal(err)
	}
	if _, err := io.Copy(out, snapshot); err != nil {
		_ = snapshot.Close()
		_ = out.Close()
		t.Fatal(err)
	}
	if err := snapshot.Close(); err != nil {
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

func TestBackupUsesDedicatedConnectionWhileSnapshotIsRunning(t *testing.T) {
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
	if _, err := db.sql.ExecContext(ctx, `CREATE TABLE backup_filler(payload BLOB NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for range 32 {
		if _, err := db.sql.ExecContext(ctx, `INSERT INTO backup_filler(payload) VALUES (randomblob(1048576))`); err != nil {
			t.Fatal(err)
		}
	}

	started := make(chan (<-chan struct{}), 1)
	release := make(chan struct{})
	db.backupProgress = func(done <-chan struct{}) {
		started <- done
		<-release
	}
	t.Cleanup(func() { db.backupProgress = nil })
	type prepared struct {
		reader io.ReadCloser
		err    error
	}
	backupDone := make(chan prepared, 1)
	go func() {
		reader, err := db.PrepareBackup(ctx)
		backupDone <- prepared{reader: reader, err: err}
	}()
	var generationDone <-chan struct{}
	select {
	case generationDone = <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("backup generation did not expose progress")
	}
	operationContext, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, err := db.sql.ExecContext(operationContext, `INSERT INTO telemetry_events(observed_at, kind, payload_json) VALUES ('2026-07-14T12:01:00Z', 'during', '{}')`); err != nil {
		t.Fatalf("writer blocked during backup generation: %v", err)
	}
	if err := db.Ready(operationContext); err != nil {
		t.Fatalf("readiness-like query blocked during backup generation: %v", err)
	}
	select {
	case <-generationDone:
		t.Fatal("backup generation finished before concurrent writer/readiness assertions")
	default:
	}
	close(release)
	result := <-backupDone
	if result.err != nil {
		t.Fatal(result.err)
	}
	defer result.reader.Close()

	backupPath := filepath.Join(dir, "consistent.db")
	out, err := os.OpenFile(backupPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, result.reader); err != nil {
		_ = out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
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

func TestBackupStagingPermissionsAndPathIgnorePermissiveUmask(t *testing.T) {
	dir := t.TempDir()
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)
	db, err := Open(context.Background(), filepath.Join(dir, "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	snapshot, err := db.PrepareBackup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	named, ok := snapshot.(interface{ Name() string })
	if !ok {
		t.Fatal("prepared snapshot does not expose its constrained staging path")
	}
	backupDir := filepath.Join(dir, ".helio-backups")
	if filepath.Dir(named.Name()) != backupDir {
		t.Fatalf("snapshot path=%q, want directory %q", named.Name(), backupDir)
	}
	for path, want := range map[string]os.FileMode{backupDir: 0o700, named.Name(): 0o600} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s mode=%#o, want %#o", path, info.Mode().Perm(), want)
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Geteuid() {
			t.Fatalf("%s owner uid=%d, want process uid=%d", path, stat.Uid, os.Geteuid())
		}
	}
}

func TestOpenCleansOnlyRegularBackupOrphans(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, ".helio-backups")
	if err := os.Mkdir(backupDir, 0o777); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(backupDir, "helio-backup-123456.db")
	unrelated := filepath.Join(backupDir, "keep.txt")
	target := filepath.Join(dir, "symlink-target.db")
	link := filepath.Join(backupDir, "helio-backup-654321.db")
	for path := range map[string]struct{}{orphan: {}, unrelated: {}, target: {}} {
		if err := os.WriteFile(path, []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	db, err := Open(context.Background(), filepath.Join(dir, "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := os.Lstat(orphan); !os.IsNotExist(err) {
		t.Fatalf("regular crash orphan still exists: %v", err)
	}
	for _, path := range []string{unrelated, target, link} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("startup cleanup removed %s: %v", path, err)
		}
	}
	info, err := os.Stat(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("backup dir mode=%#o, want 0700", info.Mode().Perm())
	}
}

func TestOpenRejectsSymlinkBackupDirectoryWithoutTouchingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(target, "helio-backup-123456.db")
	if err := os.WriteFile(keep, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, ".helio-backups")); err != nil {
		t.Fatal(err)
	}
	if db, err := Open(context.Background(), filepath.Join(dir, "helio.db")); err == nil {
		_ = db.Close()
		t.Fatal("Open accepted symlink backup directory")
	}
	if contents, err := os.ReadFile(keep); err != nil || string(contents) != "keep" {
		t.Fatalf("symlink target changed: %q err=%v", contents, err)
	}
}

func TestCancelledBackupLeavesNoStagingFile(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(context.Background(), filepath.Join(dir, "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if snapshot, err := db.PrepareBackup(ctx); err == nil {
		_ = snapshot.Close()
		t.Fatal("cancelled backup succeeded")
	}
	entries, err := os.ReadDir(filepath.Join(dir, ".helio-backups"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("cancelled backup left staging entries: %v", entries)
	}
}

func TestOpenRejectsInMemoryDatabaseExplicitly(t *testing.T) {
	if db, err := Open(context.Background(), ":memory:"); err == nil {
		_ = db.Close()
		t.Fatal("Open accepted in-memory database without a concurrency-safe backup strategy")
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
