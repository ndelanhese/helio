package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

var (
	ErrBootstrapClosed = errors.New("bootstrap is closed")
	ErrNotFound        = errors.New("not found")
)

type User struct {
	ID           string
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

type Session struct {
	TokenHash    []byte
	UserID       string
	Username     string
	PasswordHash string
	CSRFHash     []byte
	CreatedAt    time.Time
	LastSeenAt   time.Time
	ExpiresAt    time.Time
}

func (db *DB) Bootstrap(ctx context.Context, user User, session Session) error {
	return db.bootstrap(ctx, user, session, nil)
}

// BootstrapWithSettings creates the first user, its initial session, and the
// normalized settings document in one transaction.
func (db *DB) BootstrapWithSettings(ctx context.Context, user User, session Session, settings domain.Settings, allowPublicLogger ...bool) error {
	return db.bootstrap(ctx, user, session, func(ctx context.Context, tx execer) error {
		return putSettings(ctx, tx, settings, allowPublicLogger...)
	})
}

func (db *DB) bootstrap(ctx context.Context, user User, session Session, put func(context.Context, execer) error) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin bootstrap: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO users(id, username, password_hash, created_at)
		SELECT ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM users LIMIT 1)`,
		user.ID, user.Username, user.PasswordHash, formatTime(user.CreatedAt))
	if err != nil {
		return fmt.Errorf("create bootstrap user: %w", err)
	}
	created, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect bootstrap user: %w", err)
	}
	if created != 1 {
		return ErrBootstrapClosed
	}
	if err := insertSession(ctx, tx, session); err != nil {
		return err
	}
	if put != nil {
		if err := put(ctx, tx); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bootstrap: %w", err)
	}
	return nil
}

func (db *DB) BootstrapOpen(ctx context.Context) (bool, error) {
	var exists int
	if err := db.sql.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users LIMIT 1)`).Scan(&exists); err != nil {
		return false, fmt.Errorf("check bootstrap state: %w", err)
	}
	return exists == 0, nil
}

func (db *DB) FindUserByUsername(ctx context.Context, username string) (User, error) {
	var user User
	var created string
	err := db.sql.QueryRowContext(ctx,
		`SELECT id, username, password_hash, created_at FROM users WHERE username = ? COLLATE NOCASE`, username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("find user: %w", err)
	}
	user.CreatedAt, err = parseTime(created)
	if err != nil {
		return User{}, fmt.Errorf("parse user created_at: %w", err)
	}
	return user, nil
}

func (db *DB) CreateSession(ctx context.Context, session Session) error {
	return insertSession(ctx, db.sql, session)
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertSession(ctx context.Context, db execer, session Session) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO sessions(token_hash, user_id, csrf_hash, created_at, last_seen_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`, session.TokenHash, session.UserID, session.CSRFHash,
		formatTime(session.CreatedAt), formatTime(session.LastSeenAt), formatTime(session.ExpiresAt))
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (db *DB) LookupSession(ctx context.Context, tokenHash []byte) (Session, error) {
	var session Session
	var created, seen, expires string
	err := db.sql.QueryRowContext(ctx, `
		SELECT s.token_hash, s.user_id, u.username, u.password_hash, s.csrf_hash,
		       s.created_at, s.last_seen_at, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash = ?`, tokenHash,
	).Scan(&session.TokenHash, &session.UserID, &session.Username, &session.PasswordHash,
		&session.CSRFHash, &created, &seen, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("lookup session: %w", err)
	}
	if session.CreatedAt, err = parseTime(created); err != nil {
		return Session{}, fmt.Errorf("parse session created_at: %w", err)
	}
	if session.LastSeenAt, err = parseTime(seen); err != nil {
		return Session{}, fmt.Errorf("parse session last_seen_at: %w", err)
	}
	if session.ExpiresAt, err = parseTime(expires); err != nil {
		return Session{}, fmt.Errorf("parse session expires_at: %w", err)
	}
	return session, nil
}

func (db *DB) TouchSession(ctx context.Context, tokenHash []byte, seenAt time.Time) error {
	_, err := db.sql.ExecContext(ctx, `
		UPDATE sessions SET last_seen_at = ?
		WHERE token_hash = ? AND last_seen_at <= ?`,
		formatTime(seenAt), tokenHash, formatTime(seenAt.Add(-5*time.Minute)))
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

// RotateSessionCSRF atomically replaces the single valid CSRF digest for a session.
// The previous digest becomes invalid as soon as this update commits.
func (db *DB) RotateSessionCSRF(ctx context.Context, tokenHash, csrfHash []byte) error {
	result, err := db.sql.ExecContext(ctx, `UPDATE sessions SET csrf_hash = ? WHERE token_hash = ?`, csrfHash, tokenHash)
	if err != nil {
		return fmt.Errorf("rotate session csrf: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect session csrf rotation: %w", err)
	}
	if updated != 1 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) DeleteSession(ctx context.Context, tokenHash []byte) error {
	if _, err := db.sql.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// SetSessionTimes supports administrative expiry maintenance and deterministic tests.
func (db *DB) SetSessionTimes(ctx context.Context, tokenHash []byte, lastSeen, expires time.Time) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE token_hash = ?`, formatTime(lastSeen), formatTime(expires), tokenHash)
	if err != nil {
		return fmt.Errorf("set session times: %w", err)
	}
	return nil
}

func (db *DB) ContainsSessionMaterial(ctx context.Context, token, csrf string) (bool, error) {
	var exists int
	err := db.sql.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM sessions
		WHERE token_hash = CAST(? AS BLOB) OR csrf_hash = CAST(? AS BLOB)
		   OR token_hash = CAST(? AS BLOB) OR csrf_hash = CAST(? AS BLOB))`, token, token, csrf, csrf).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("inspect session material: %w", err)
	}
	return exists != 0, nil
}

func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }
