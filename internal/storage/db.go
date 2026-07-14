package storage

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

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
	dsn := uri + "?_pragma=busy_timeout%285000%29&_pragma=foreign_keys%281%29"
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
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.sql.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("configure sqlite (%s): %w", pragma, err)
		}
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
	tmp, err := os.CreateTemp(filepath.Dir(db.path), ".helio-backup-*.db")
	if err != nil {
		return fmt.Errorf("create backup path: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close backup placeholder: %w", err)
	}
	if err := os.Remove(tmpPath); err != nil {
		return fmt.Errorf("prepare backup path: %w", err)
	}
	defer os.Remove(tmpPath)

	if _, err := db.sql.ExecContext(ctx, "VACUUM INTO ?", tmpPath); err != nil {
		return fmt.Errorf("vacuum backup: %w", err)
	}

	backup, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("open backup snapshot: %w", err)
	}
	defer backup.Close()
	if _, err := io.Copy(w, backup); err != nil {
		return fmt.Errorf("stream backup snapshot: %w", err)
	}
	return nil
}
