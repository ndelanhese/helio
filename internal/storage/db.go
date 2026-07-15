package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

// DB is Helio's durable SQLite store.
type DB struct {
	sql            *sql.DB
	path           string
	dsn            string
	backupProgress func(<-chan struct{})
}

// Open opens a SQLite database, configures it for durable local use, and
// applies all migrations known to this binary.
func Open(ctx context.Context, path string) (*DB, error) {
	if path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
		return nil, errors.New("in-memory SQLite databases are unsupported")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}

	if err := prepareBackupDirectory(filepath.Join(filepath.Dir(absPath), ".helio-backups")); err != nil {
		return nil, err
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absPath)}).String()
	dsn := uri + "?_pragma=busy_timeout%285000%29&_pragma=foreign_keys%281%29&_pragma=synchronous%28NORMAL%29"
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	sqldb.SetMaxOpenConns(1)

	db := &DB{sql: sqldb, path: absPath, dsn: dsn}
	if err := db.configure(ctx); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	if err := db.migrate(ctx); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	return db, nil
}

func (db *DB) configure(ctx context.Context) error {
	const pragma = "PRAGMA journal_mode=WAL"
	if _, err := db.sql.ExecContext(ctx, pragma); err != nil {
		return fmt.Errorf("configure sqlite (%s): %w", pragma, err)
	}
	return nil
}

// Close closes the database.
func (db *DB) Close() error {
	return db.sql.Close()
}

// Ready verifies that the database is reachable.
func (db *DB) Ready(ctx context.Context) error {
	if err := db.sql.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sqlite database: %w", err)
	}
	return nil
}

// PrepareBackup creates a consistent standalone snapshot beside the live
// database. The caller must close the returned reader; Close removes the
// snapshot even when response streaming is interrupted.
func (db *DB) PrepareBackup(ctx context.Context) (io.ReadCloser, error) {
	if db.path == ":memory:" || db.dsn == "" {
		return nil, errors.New("prepare backup: file-backed database is required")
	}
	backupDir := filepath.Join(filepath.Dir(db.path), ".helio-backups")
	if err := ensureSecureBackupDirectory(backupDir); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(backupDir, "helio-backup-*.db")
	if err != nil {
		return nil, fmt.Errorf("create backup path: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("close backup placeholder: %w", err)
	}
	if err := os.Remove(tmpPath); err != nil {
		return nil, fmt.Errorf("prepare backup path: %w", err)
	}

	source, err := sql.Open("sqlite", db.dsn)
	if err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("open dedicated backup connection: %w", err)
	}
	source.SetMaxOpenConns(1)
	source.SetMaxIdleConns(1)
	var journalMode string
	if err := source.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		_ = source.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("verify dedicated backup journal mode: %w", err)
	}
	if strings.ToLower(journalMode) != "wal" {
		_ = source.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("verify dedicated backup journal mode: got %q", journalMode)
	}
	if err := db.vacuumInto(ctx, source, tmpPath); err != nil {
		_ = source.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("vacuum backup: %w", err)
	}
	if err := source.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("close dedicated backup connection: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("secure backup snapshot: %w", err)
	}

	backup, err := os.Open(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("open backup snapshot: %w", err)
	}
	return &backupSnapshot{File: backup, path: tmpPath}, nil
}

func (db *DB) vacuumInto(ctx context.Context, source *sql.DB, target string) error {
	progress := db.backupProgress
	if progress == nil {
		_, err := source.ExecContext(ctx, "VACUUM INTO ?", target)
		return err
	}
	result := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		_, err := source.ExecContext(ctx, "VACUUM INTO ?", target)
		close(done)
		result <- err
	}()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-result:
			return err
		case <-ctx.Done():
			return <-result
		case <-ticker.C:
			info, err := os.Lstat(target)
			if err == nil && info.Mode().IsRegular() {
				progress(done)
				return <-result
			}
		}
	}
}

func prepareBackupDirectory(path string) error {
	if err := ensureSecureBackupDirectory(path); err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("read backup staging directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !isBackupStagingName(entry.Name()) {
			continue
		}
		candidate := filepath.Join(path, entry.Name())
		info, err := os.Lstat(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("inspect backup staging orphan: %w", err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := os.Remove(candidate); err != nil {
			return fmt.Errorf("remove backup staging orphan: %w", err)
		}
	}
	return nil
}

func ensureSecureBackupDirectory(path string) error {
	err := os.Mkdir(path, 0o700)
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("create backup staging directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect backup staging directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("backup staging path is not a directory")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Geteuid() {
		return errors.New("backup staging directory is not owned by the process user")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure backup staging directory: %w", err)
	}
	return nil
}

func isBackupStagingName(name string) bool {
	const prefix, suffix = "helio-backup-", ".db"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return false
	}
	middle := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	if middle == "" {
		return false
	}
	for _, character := range middle {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

type backupSnapshot struct {
	*os.File
	path string
	once sync.Once
	err  error
}

func (snapshot *backupSnapshot) Close() error {
	snapshot.once.Do(func() {
		closeErr := snapshot.File.Close()
		removeErr := os.Remove(snapshot.path)
		if closeErr != nil {
			snapshot.err = closeErr
		} else if removeErr != nil && !os.IsNotExist(removeErr) {
			snapshot.err = removeErr
		}
	})
	return snapshot.err
}
