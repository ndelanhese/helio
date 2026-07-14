package storage

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

func TestBootstrapRepositoryIsAtomicUnderRace(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"one", "two"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			results <- db.Bootstrap(context.Background(), User{ID: id, Username: id, PasswordHash: "hash", CreatedAt: now}, Session{TokenHash: []byte(id + "-token"), UserID: id, CSRFHash: []byte(id + "-csrf"), CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)})
		}(id)
	}
	wg.Wait()
	close(results)
	var success, closed int
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrBootstrapClosed) {
			closed++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || closed != 1 {
		t.Fatalf("success=%d closed=%d", success, closed)
	}
}

func TestBootstrapWithSettingsIsAtomicUnderRace(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	settings := validStoredSettings()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"one", "two"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			results <- db.BootstrapWithSettings(context.Background(),
				User{ID: id, Username: id, PasswordHash: "hash", CreatedAt: now},
				Session{TokenHash: []byte(id + "-token"), UserID: id, CSRFHash: []byte(id + "-csrf"), CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}, settings)
		}(id)
	}
	wg.Wait()
	close(results)
	var success, closed int
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrBootstrapClosed) {
			closed++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || closed != 1 {
		t.Fatalf("success=%d closed=%d", success, closed)
	}
	assertRowCounts(t, db, 1, 1, 1)
}

func TestBootstrapWithSettingsRollsBackEveryRecord(t *testing.T) {
	for _, tc := range []struct {
		name     string
		prepare  func(*testing.T, *DB)
		settings domain.Settings
	}{
		{"invalid settings", func(*testing.T, *DB) {}, domain.Settings{}},
		{"settings write failure", func(t *testing.T, db *DB) {
			_, err := db.sql.Exec(`CREATE TRIGGER fail_settings BEFORE INSERT ON settings BEGIN SELECT RAISE(ABORT, 'injected settings failure'); END`)
			if err != nil {
				t.Fatal(err)
			}
		}, validStoredSettings()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := Open(context.Background(), filepath.Join(t.TempDir(), "helio.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			tc.prepare(t, db)
			now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
			err = db.BootstrapWithSettings(context.Background(),
				User{ID: "u", Username: "admin", PasswordHash: "hash", CreatedAt: now},
				Session{TokenHash: []byte("token"), UserID: "u", CSRFHash: []byte("csrf"), CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}, tc.settings)
			if err == nil {
				t.Fatal("expected bootstrap failure")
			}
			assertRowCounts(t, db, 0, 0, 0)
		})
	}
}

func assertRowCounts(t *testing.T, db *DB, users, sessions, settings int) {
	t.Helper()
	for table, want := range map[string]int{"users": users, "sessions": sessions, "settings": settings} {
		var got int
		if err := db.sql.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s rows=%d want=%d", table, got, want)
		}
	}
}

func TestSessionRepositoryLookupJoinsUser(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	u := User{ID: "u", Username: "Admin", PasswordHash: "hash", CreatedAt: now}
	s := Session{TokenHash: []byte("digest"), UserID: "u", CSRFHash: []byte("csrf-digest"), CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := db.Bootstrap(context.Background(), u, s); err != nil {
		t.Fatal(err)
	}
	got, err := db.LookupSession(context.Background(), []byte("digest"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "Admin" || got.PasswordHash != "hash" || got.UserID != "u" {
		t.Fatalf("session=%#v", got)
	}
}
