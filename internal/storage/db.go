package storage

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

// DB is Helio's durable SQLite store.
type DB struct {
	sql  *sql.DB
	path string
}

// Open opens a SQLite database, configures it for durable local use, and
// applies all migrations known to this binary.
func Open(ctx context.Context, path string) (*DB, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}

	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absPath)}).String()
	dsn := uri + "?_pragma=busy_timeout%285000%29&_pragma=foreign_keys%281%29&_pragma=synchronous%28NORMAL%29"
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	sqldb.SetMaxOpenConns(1)

	db := &DB{sql: sqldb, path: absPath}
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

// Backup writes a consistent, standalone SQLite snapshot to w.
func (db *DB) Backup(ctx context.Context, w io.Writer) error {
	snapshot, err := db.PrepareBackup(ctx)
	if err != nil {
		return err
	}
	defer snapshot.Close()
	if _, err := io.Copy(w, snapshot); err != nil {
		return fmt.Errorf("stream backup snapshot: %w", err)
	}
	return nil
}

// PrepareBackup creates a consistent standalone snapshot beside the live
// database. The caller must close the returned reader; Close removes the
// snapshot even when response streaming is interrupted.
func (db *DB) PrepareBackup(ctx context.Context) (io.ReadCloser, error) {
	tmp, err := os.CreateTemp(filepath.Dir(db.path), ".helio-backup-*.db")
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

	if _, err := db.sql.ExecContext(ctx, "VACUUM INTO ?", tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("vacuum backup: %w", err)
	}

	backup, err := os.Open(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("open backup snapshot: %w", err)
	}
	return &backupSnapshot{File: backup, path: tmpPath}, nil
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
