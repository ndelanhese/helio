package api

import (
	"errors"
	"math"
	"net"
	"net/http"
	"strconv"

	"github.com/ndelanhese/helio/internal/auth"
)

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	if a.dependencies.Auth == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "authentication is unavailable")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	remote := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		remote = host
	}
	credentials, err := a.dependencies.Auth.Login(r.Context(), remote, body.Username, body.Password)
	if errors.Is(err, auth.ErrRateLimited) {
		seconds := int(math.Ceil(auth.RetryAfter(err).Seconds()))
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many login attempts")
		return
	}
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "username or password is invalid")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "login failed")
		return
	}
	http.SetCookie(w, a.dependencies.Auth.SessionCookie(credentials.Token))
	writeJSON(w, http.StatusOK, credentials)
}

func (a *API) session(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	cookie, err := r.Cookie("helio_session")
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	csrf, err := a.dependencies.Auth.RotateCSRF(r.Context(), cookie.Value)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "session could not be refreshed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"userId": principal.UserID, "username": principal.Username, "expiresAt": principal.ExpiresAt.UTC(), "csrfToken": csrf})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie("helio_session")
	if cookie != nil {
		if err := a.dependencies.Auth.Logout(r.Context(), cookie.Value); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "logout failed")
			return
		}
	}
	http.SetCookie(w, a.dependencies.Auth.ClearSessionCookie())
	w.WriteHeader(http.StatusNoContent)
}
