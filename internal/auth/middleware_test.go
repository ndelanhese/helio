package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSessionMiddlewareReturnsJSONUnauthorized(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	m, _ := testManager(t, &now)
	h := RequireSession(m, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next called") }))
	r := httptest.NewRequest(http.MethodGet, "http://helio.local/api/v1/live", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || w.Header().Get("Content-Type") != "application/json" || !strings.Contains(w.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("response=%d %s %q", w.Code, w.Header(), w.Body.String())
	}
}

func TestCSRFRejectsMissingWrongAndCrossOrigin(t *testing.T) {
	csrf := "csrf-token"
	p := &Principal{CSRFHash: digestToken(csrf)}
	h := RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for name, values := range map[string][2]string{
		"missing": {"", "http://helio.local"}, "wrong": {"wrong", "http://helio.local"}, "cross-origin": {csrf, "http://evil.local"},
	} {
		t.Run(name, func(t *testing.T) {
			token, origin := values[0], values[1]
			r := httptest.NewRequest(http.MethodPost, "http://helio.local/api/v1/settings", nil)
			r.Host = "helio.local"
			r.Header.Set("Origin", origin)
			if token != "" {
				r.Header.Set("X-CSRF-Token", token)
			}
			r = r.WithContext(context.WithValue(r.Context(), principalContextKey{}, p))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
			}
		})
	}
}

func TestCSRFAcceptsSameOriginMutation(t *testing.T) {
	csrf := "csrf-token"
	p := &Principal{CSRFHash: digestToken(csrf)}
	h := RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	r := httptest.NewRequest(http.MethodPost, "https://helio.local/api/v1/settings", nil)
	r.Host = "helio.local"
	r.Header.Set("Origin", "https://helio.local")
	r.Header.Set("X-CSRF-Token", csrf)
	r = r.WithContext(context.WithValue(r.Context(), principalContextKey{}, p))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestBootstrapGateAndUnauthenticatedSameOrigin(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	m, _ := testManager(t, &now)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	r := httptest.NewRequest(http.MethodGet, "http://helio.local/api/v1/live", nil)
	w := httptest.NewRecorder()
	BootstrapGate(m, next).ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("open bootstrap gate status=%d", w.Code)
	}
	if _, err := m.Bootstrap(context.Background(), "Admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	BootstrapGate(m, next).ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("closed bootstrap gate status=%d", w.Code)
	}

	post := httptest.NewRequest(http.MethodPost, "http://helio.local/api/v1/auth/login", nil)
	post.Host = "helio.local"
	post.Header.Set("Origin", "http://evil.local")
	w = httptest.NewRecorder()
	RequireSameOrigin(next).ServeHTTP(w, post)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin login status=%d", w.Code)
	}
}
