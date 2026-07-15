package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/storage"
)

const (
	sessionLifetime      = 30 * 24 * time.Hour
	sessionIdleTime      = 24 * time.Hour
	sessionTouchInterval = 5 * time.Minute
	confirmationLifetime = 5 * time.Minute
	tokenBytes           = 32
)

var (
	ErrBootstrapClosed    = errors.New("bootstrap is closed")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthenticated    = errors.New("unauthenticated")
	ErrRateLimited        = errors.New("login rate limited")
)

type Store interface {
	Bootstrap(context.Context, storage.User, storage.Session) error
	BootstrapOpen(context.Context) (bool, error)
	FindUserByUsername(context.Context, string) (storage.User, error)
	CreateSession(context.Context, storage.Session) error
	LookupSession(context.Context, []byte) (storage.Session, error)
	TouchSession(context.Context, []byte, time.Time) error
	RotateSessionCSRF(context.Context, []byte, []byte) error
	DeleteSession(context.Context, []byte) error
}

type Manager struct {
	store            Store
	now              func() time.Time
	random           io.Reader
	limiter          *Limiter
	secureCookies    bool
	passwordVerifier func(string, string) (bool, error)
	confirmMu        sync.Mutex
	confirmed        map[string]time.Time
}

type Option func(*Manager)

func WithClock(clock func() time.Time) Option {
	return func(m *Manager) {
		if clock != nil {
			m.now = clock
		}
	}
}
func WithRandom(random io.Reader) Option {
	return func(m *Manager) {
		if random != nil {
			m.random = random
		}
	}
}
func WithLimiter(limiter *Limiter) Option {
	return func(m *Manager) {
		if limiter != nil {
			m.limiter = limiter
		}
	}
}
func WithSecureCookies(secure bool) Option { return func(m *Manager) { m.secureCookies = secure } }

func NewManager(store Store, options ...Option) *Manager {
	m := &Manager{store: store, now: time.Now, random: rand.Reader, passwordVerifier: VerifyPassword, confirmed: make(map[string]time.Time)}
	secret := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, secret); err != nil {
		panic("auth: system entropy unavailable")
	}
	m.limiter = NewLimiter(secret, func() time.Time { return m.now() })
	for _, option := range options {
		option(m)
	}
	return m
}

type Credentials struct {
	Token     string    `json:"-"`
	CSRF      string    `json:"csrfToken"`
	UserID    string    `json:"userId"`
	Username  string    `json:"username"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Principal struct {
	UserID    string
	Username  string
	CSRFHash  []byte
	ExpiresAt time.Time
}

func (m *Manager) Bootstrap(ctx context.Context, username, password string) (*Credentials, error) {
	return m.bootstrap(ctx, username, password, nil, false)
}

// BootstrapWithSettings atomically creates the initial administrator, session,
// and normalized settings document.
func (m *Manager) BootstrapWithSettings(ctx context.Context, username, password string, settings domain.Settings, allowPublicLogger bool) (*Credentials, error) {
	return m.bootstrap(ctx, username, password, &settings, allowPublicLogger)
}

func (m *Manager) bootstrap(ctx context.Context, username, password string, settings *domain.Settings, allowPublicLogger bool) (*Credentials, error) {
	hash, err := hashPassword(password, m.random)
	if err != nil {
		return nil, err
	}
	now := m.now().UTC()
	userID, err := randomToken(m.random, 16)
	if err != nil {
		return nil, err
	}
	creds, session, err := m.newSession(userID, username, now)
	if err != nil {
		return nil, err
	}
	user := storage.User{ID: userID, Username: username, PasswordHash: hash, CreatedAt: now}
	if settings == nil {
		err = m.store.Bootstrap(ctx, user, session)
	} else {
		store, ok := m.store.(interface {
			BootstrapWithSettings(context.Context, storage.User, storage.Session, domain.Settings, ...bool) error
		})
		if !ok {
			return nil, errors.New("bootstrap settings transaction is unavailable")
		}
		err = store.BootstrapWithSettings(ctx, user, session, *settings, allowPublicLogger)
	}
	if errors.Is(err, storage.ErrBootstrapClosed) {
		return nil, ErrBootstrapClosed
	}
	if err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	return creds, nil
}

func (m *Manager) Login(ctx context.Context, remoteAddr, username, password string) (*Credentials, error) {
	attempt, retry := m.limiter.Admit(remoteAddr, username)
	if attempt == nil {
		return nil, rateLimitError{retryAfter: retry}
	}
	user, err := m.store.FindUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			_, _ = m.passwordVerifier(dummyPasswordHash, password)
			attempt.Failure()
			return nil, ErrInvalidCredentials
		}
		attempt.Cancel()
		return nil, fmt.Errorf("login lookup: %w", err)
	}
	valid, err := m.passwordVerifier(user.PasswordHash, password)
	if err != nil && !errors.Is(err, ErrPasswordLength) && !errors.Is(err, ErrPasswordEncoding) {
		attempt.Cancel()
		return nil, fmt.Errorf("verify credentials: %w", err)
	}
	if !valid {
		attempt.Failure()
		return nil, ErrInvalidCredentials
	}
	now := m.now().UTC()
	creds, session, err := m.newSession(user.ID, user.Username, now)
	if err != nil {
		attempt.Cancel()
		return nil, err
	}
	if err := m.store.CreateSession(ctx, session); err != nil {
		attempt.Cancel()
		return nil, fmt.Errorf("create login session: %w", err)
	}
	attempt.Success()
	return creds, nil
}

func (m *Manager) Authenticate(ctx context.Context, rawToken string) (*Principal, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(rawToken)
	if err != nil || len(decoded) != tokenBytes {
		return nil, ErrUnauthenticated
	}
	hash := digestToken(rawToken)
	session, err := m.store.LookupSession(ctx, hash)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrUnauthenticated
	}
	if err != nil {
		return nil, fmt.Errorf("authenticate session: %w", err)
	}
	now := m.now().UTC()
	if !now.Before(session.ExpiresAt) || !now.Before(session.CreatedAt.Add(sessionLifetime)) || now.Sub(session.LastSeenAt) >= sessionIdleTime {
		_ = m.store.DeleteSession(ctx, hash)
		return nil, ErrUnauthenticated
	}
	if now.Sub(session.LastSeenAt) >= sessionTouchInterval {
		if err := m.store.TouchSession(ctx, hash, now); err != nil {
			return nil, fmt.Errorf("touch authenticated session: %w", err)
		}
	}
	return &Principal{UserID: session.UserID, Username: session.Username, CSRFHash: append([]byte(nil), session.CSRFHash...), ExpiresAt: session.ExpiresAt}, nil
}

func (m *Manager) Logout(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	m.clearConfirmation(rawToken)
	if err := m.store.DeleteSession(ctx, digestToken(rawToken)); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}

// ConfirmPassword verifies the authenticated user's password and grants this
// exact session a short-lived, in-memory confirmation. It never creates or
// replaces a durable session.
func (m *Manager) ConfirmPassword(ctx context.Context, rawSessionToken, remoteAddr, password string) error {
	if _, err := m.Authenticate(ctx, rawSessionToken); err != nil {
		return err
	}
	hash := digestToken(rawSessionToken)
	key := string(hash)
	m.confirmMu.Lock()
	delete(m.confirmed, key)
	m.confirmMu.Unlock()
	session, err := m.store.LookupSession(ctx, hash)
	if err != nil {
		return fmt.Errorf("confirm session password: %w", err)
	}
	attempt, retry := m.limiter.Admit(remoteAddr, session.Username)
	if attempt == nil {
		return rateLimitError{retryAfter: retry}
	}
	valid, err := m.passwordVerifier(session.PasswordHash, password)
	if err != nil && !errors.Is(err, ErrPasswordLength) && !errors.Is(err, ErrPasswordEncoding) {
		attempt.Cancel()
		return fmt.Errorf("confirm session password: %w", err)
	}
	if !valid {
		attempt.Failure()
		return ErrInvalidCredentials
	}
	attempt.Success()
	m.confirmMu.Lock()
	m.confirmed[key] = m.now().UTC().Add(confirmationLifetime)
	m.confirmMu.Unlock()
	return nil
}

func (m *Manager) RecentlyConfirmed(rawSessionToken string) bool {
	key := string(digestToken(rawSessionToken))
	m.confirmMu.Lock()
	defer m.confirmMu.Unlock()
	expires, ok := m.confirmed[key]
	if !ok || !m.now().UTC().Before(expires) {
		delete(m.confirmed, key)
		return false
	}
	return true
}

// ConsumeRecentConfirmation makes sensitive settings authorization one-shot.
func (m *Manager) ConsumeRecentConfirmation(rawSessionToken string) bool {
	if !m.RecentlyConfirmed(rawSessionToken) {
		return false
	}
	m.clearConfirmation(rawSessionToken)
	return true
}

func (m *Manager) clearConfirmation(rawSessionToken string) {
	m.confirmMu.Lock()
	delete(m.confirmed, string(digestToken(rawSessionToken)))
	m.confirmMu.Unlock()
}

// RotateCSRF issues a fresh 256-bit CSRF token and atomically makes it the
// session's only current CSRF token. Raw token material is never persisted.
func (m *Manager) RotateCSRF(ctx context.Context, rawSessionToken string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(rawSessionToken)
	if err != nil || len(decoded) != tokenBytes {
		return "", ErrUnauthenticated
	}
	csrf, err := randomToken(m.random, tokenBytes)
	if err != nil {
		return "", err
	}
	if err := m.store.RotateSessionCSRF(ctx, digestToken(rawSessionToken), digestToken(csrf)); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return "", ErrUnauthenticated
		}
		return "", fmt.Errorf("rotate csrf: %w", err)
	}
	return csrf, nil
}

func (m *Manager) BootstrapOpen(ctx context.Context) (bool, error) { return m.store.BootstrapOpen(ctx) }

func (m *Manager) SessionCookie(token string) *http.Cookie {
	return &http.Cookie{Name: "helio_session", Value: token, Path: "/", HttpOnly: true, Secure: m.secureCookies, SameSite: http.SameSiteStrictMode, MaxAge: int(sessionLifetime.Seconds())}
}

func (m *Manager) ClearSessionCookie() *http.Cookie {
	cookie := m.SessionCookie("")
	cookie.MaxAge = -1
	cookie.Expires = time.Unix(1, 0).UTC()
	return cookie
}

func (m *Manager) newSession(userID, username string, now time.Time) (*Credentials, storage.Session, error) {
	token, err := randomToken(m.random, tokenBytes)
	if err != nil {
		return nil, storage.Session{}, err
	}
	csrf, err := randomToken(m.random, tokenBytes)
	if err != nil {
		return nil, storage.Session{}, err
	}
	expires := now.Add(sessionLifetime)
	return &Credentials{Token: token, CSRF: csrf, UserID: userID, Username: username, ExpiresAt: expires}, storage.Session{
		TokenHash: digestToken(token), UserID: userID, CSRFHash: digestToken(csrf), CreatedAt: now, LastSeenAt: now, ExpiresAt: expires,
	}, nil
}

func randomToken(entropy io.Reader, size int) (string, error) {
	value := make([]byte, size)
	if _, err := io.ReadFull(entropy, value); err != nil {
		return "", fmt.Errorf("generate authentication token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func digestToken(token string) []byte { sum := sha256.Sum256([]byte(token)); return sum[:] }

type rateLimitError struct{ retryAfter time.Duration }

func (e rateLimitError) Error() string { return ErrRateLimited.Error() }
func (e rateLimitError) Unwrap() error { return ErrRateLimited }
func RetryAfter(err error) time.Duration {
	var limited rateLimitError
	if errors.As(err, &limited) {
		return limited.retryAfter
	}
	return 0
}
