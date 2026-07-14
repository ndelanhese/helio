package httpserver_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ndelanhese/helio/internal/httpserver"
)

func TestHealthAndSPAFallback(t *testing.T) {
	handler := httpserver.New(httpserver.Dependencies{})
	for _, tc := range []struct{ path, contentType string }{
		{"/health/live", "application/json"},
		{"/history", "text/html; charset=utf-8"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: got %d", tc.path, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != tc.contentType {
			t.Fatalf("%s: %q", tc.path, got)
		}
	}
}
