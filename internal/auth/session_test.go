package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/storage"
)

func testManager(t *testing.T, now *time.Time) (*Manager, *storage.DB) {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	random := &counterReader{}
	limiter := NewLimiter([]byte("01234567890123456789012345678901"), func() time.Time { return *now })
	return NewManager(db, WithClock(func() time.Time { return *now }), WithRandom(random), WithLimiter(limiter)), db
}

type counterReader struct{ value byte }

func (r *counterReader) Read(p []byte) (int, error) {
	for i := range p {
		r.value++
		p[i] = r.value
	}
	return len(p), nil
}

var _ io.Reader = (*counterReader)(nil)

func TestBootstrapSucceedsOnceAndCreatesOpaqueSession(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	m, db := testManager(t, &now)
	creds, err := m.Bootstrap(context.Background(), "Admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if creds.Token == "" || creds.CSRF == "" || creds.ExpiresAt != now.Add(30*24*time.Hour) {
		t.Fatalf("bad credentials: %#v", creds)
	}
	if _, err := m.Bootstrap(context.Background(), "Other", "another secure password"); !errors.Is(err, ErrBootstrapClosed) {
		t.Fatalf("second bootstrap: %v", err)
	}
	if found, err := db.ContainsSessionMaterial(context.Background(), creds.Token, creds.CSRF); err != nil || found {
		t.Fatalf("raw session material stored: found=%v err=%v", found, err)
	}
}

func TestSessionAuthenticateExpiryIdleAndTouch(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	m, db := testManager(t, &now)
	creds, err := m.Bootstrap(context.Background(), "Admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Authenticate(context.Background(), creds.Token)
	if err != nil || p.Username != "Admin" {
		t.Fatalf("authenticate: %#v %v", p, err)
	}

	now = now.Add(6 * time.Minute)
	if _, err := m.Authenticate(context.Background(), creds.Token); err != nil {
		t.Fatal(err)
	}
	tokenHash := sha256.Sum256([]byte(creds.Token))
	s, err := db.LookupSession(context.Background(), tokenHash[:])
	if err != nil || !s.LastSeenAt.Equal(now) {
		t.Fatalf("last_seen=%v err=%v", s.LastSeenAt, err)
	}

	if err := db.SetSessionTimes(context.Background(), tokenHash[:], now.Add(-24*time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Authenticate(context.Background(), creds.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("idle authenticate: %v", err)
	}
	if err := db.SetSessionTimes(context.Background(), tokenHash[:], now, now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Authenticate(context.Background(), creds.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired authenticate: %v", err)
	}
}

func TestRotateCSRFReplacesSingleCurrentTokenWithoutStoringRawMaterial(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	m, db := testManager(t, &now)
	creds, err := m.Bootstrap(context.Background(), "Admin", "correct horse battery staple")
	if err != nil { t.Fatal(err) }

	fresh, err := m.RotateCSRF(context.Background(), creds.Token)
	if err != nil { t.Fatal(err) }
	if fresh == "" || fresh == creds.CSRF {
		t.Fatalf("csrf was not freshly rotated")
	}
	tokenHash := sha256.Sum256([]byte(creds.Token))
	session, err := db.LookupSession(context.Background(), tokenHash[:])
	if err != nil { t.Fatal(err) }
	want := sha256.Sum256([]byte(fresh))
	if string(session.CSRFHash) != string(want[:]) {
		t.Fatal("database does not contain the fresh CSRF digest")
	}
	if found, err := db.ContainsSessionMaterial(context.Background(), creds.Token, fresh); err != nil || found {
		t.Fatalf("raw authentication material stored: found=%v err=%v", found, err)
	}
}

func TestSessionCookieSecurityFlags(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	m, _ := testManager(t, &now)
	m.secureCookies = true
	c := m.SessionCookie("opaque")
	if c.Name != "helio_session" || c.Value != "opaque" || c.Path != "/" || !c.HttpOnly || !c.Secure || c.SameSite != 3 {
		t.Fatalf("cookie=%#v", c)
	}
}

func TestSessionLogoutRevokesTokenAndClearsCookie(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	m, _ := testManager(t, &now)
	creds, err := m.Bootstrap(context.Background(), "Admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Logout(context.Background(), creds.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Authenticate(context.Background(), creds.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("authenticate after logout: %v", err)
	}
	cookie := m.ClearSessionCookie()
	if cookie.Value != "" || cookie.MaxAge != -1 || !cookie.Expires.Before(now) {
		t.Fatalf("clear cookie=%#v", cookie)
	}
}

func TestLimiterSuccessfulLoginResetsAndSixthAttemptLimited(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	m, _ := testManager(t, &now)
	if _, err := m.Bootstrap(context.Background(), "Admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err := m.Login(context.Background(), "203.0.113.8:4000", "Admin", "wrong password value"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("failure %d: %v", i+1, err)
		}
	}
	if _, err := m.Login(context.Background(), "203.0.113.8", "ADMIN", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := m.Login(context.Background(), "203.0.113.8:4000", "Admin", "wrong password value"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("post-success failure %d: %v", i+1, err)
		}
	}
	if _, err := m.Login(context.Background(), "203.0.113.8:5000", "admin", "wrong password value"); RetryAfter(err) != 15*time.Minute {
		t.Fatalf("sixth login: %v", err)
	}
}
