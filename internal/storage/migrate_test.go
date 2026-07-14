package storage

import (
	"context"
	"path/filepath"
	"testing"
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
		"INSERT INTO schema_migrations(version, applied_at) VALUES (2, CURRENT_TIMESTAMP)",
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
