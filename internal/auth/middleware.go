package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

type principalContextKey struct{}

func PrincipalFromRequest(r *http.Request) (*Principal, bool) {
	principal, ok := r.Context().Value(principalContextKey{}).(*Principal)
	return principal, ok
}

func RequireSession(manager *Manager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("helio_session")
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		principal, err := manager.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalContextKey{}, principal)))
	})
}

func RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		principal, ok := PrincipalFromRequest(r)
		token := r.Header.Get("X-CSRF-Token")
		if !ok || token == "" || !sameOrigin(r) || subtle.ConstantTimeCompare(digestToken(token), principal.CSRFHash) != 1 {
			writeAuthError(w, http.StatusForbidden, "forbidden", "request origin or CSRF token is invalid")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RequireSameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			writeAuthError(w, http.StatusForbidden, "forbidden", "request origin is invalid")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func BootstrapGate(manager *Manager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		open, err := manager.BootstrapOpen(r.Context())
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "internal_error", "bootstrap state unavailable")
			return
		}
		if open {
			writeAuthError(w, http.StatusServiceUnavailable, "bootstrap_required", "initial setup is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	parsed, err := url.Parse(origin)
	if err != nil || origin == "" || parsed.User != nil || parsed.Host == "" || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.URL.Scheme, "https") {
		scheme = "https"
	}
	return strings.EqualFold(parsed.Scheme, scheme) && strings.EqualFold(parsed.Host, r.Host)
}

func writeAuthError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}
