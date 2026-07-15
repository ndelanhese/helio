package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

func (db *DB) migrate(ctx context.Context) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	if _, err := db.sql.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	var current int
	if err := db.sql.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(version), 0) FROM schema_migrations",
	).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	latest := 0
	if len(migrations) != 0 {
		latest = migrations[len(migrations)-1].version
	}
	if current > latest {
		return fmt.Errorf("database schema version %d is newer than binary version %d", current, latest)
	}

	for _, migration := range migrations {
		var applied int
		err := db.sql.QueryRowContext(ctx,
			"SELECT 1 FROM schema_migrations WHERE version = ?", migration.version,
		).Scan(&applied)
		if err == nil {
			continue
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check migration %04d: %w", migration.version, err)
		}

		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %04d: %w", migration.version, err)
		}
		if _, err := tx.ExecContext(ctx, migration.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %04d: %w", migration.version, err)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations(version, applied_at) VALUES (?, CURRENT_TIMESTAMP)",
			migration.version,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %04d: %w", migration.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %04d: %w", migration.version, err)
		}
	}
	return nil
}

func loadMigrations() ([]migration, error) {
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(names)
	migrations := make([]migration, 0, len(names))
	seen := make(map[int]struct{}, len(names))
	for _, name := range names {
		prefix := strings.SplitN(filepath.Base(name), "_", 2)[0]
		version, err := strconv.Atoi(prefix)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("invalid migration filename %q", name)
		}
		if _, exists := seen[version]; exists {
			return nil, fmt.Errorf("duplicate migration version %d", version)
		}
		seen[version] = struct{}{}
		contents, err := migrationFiles.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", name, err)
		}
		migrations = append(migrations, migration{version: version, name: name, sql: string(contents)})
	}
	return migrations, nil
}
