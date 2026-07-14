package storage

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
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
