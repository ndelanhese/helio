package httpserver_test

import (
	"errors"
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

func TestAPIMountAndReadinessOnlyTracksDatabase(t *testing.T) {
	apiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/components" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"logger":"offline"}`))
			return
		}
		_, _ = w.Write([]byte("api"))
	})
	handler := httpserver.New(httpserver.Dependencies{API: apiHandler, Ready: func() error { return errors.New("db down") }})
	components := httptest.NewRecorder()
	handler.ServeHTTP(components, httptest.NewRequest(http.MethodGet, "/health/components", nil))
	if components.Code != http.StatusOK || components.Body.String() != `{"logger":"offline"}` {
		t.Fatalf("components: %d %s", components.Code, components.Body.String())
	}
	ready := httptest.NewRecorder()
	handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if ready.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready: %d", ready.Code)
	}
	api := httptest.NewRecorder()
	handler.ServeHTTP(api, httptest.NewRequest(http.MethodGet, "/api/v1/live", nil))
	if api.Body.String() != "api" {
		t.Fatalf("api not mounted: %d %s", api.Code, api.Body.String())
	}
}
