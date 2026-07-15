package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunAcceptsReadyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}))
	t.Cleanup(server.Close)

	if err := run(context.Background(), server.URL); err != nil {
		t.Fatalf("run returned an error for a ready response: %v", err)
	}
}

func TestRunRejectsUnhealthyResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{name: "non-200", statusCode: http.StatusServiceUnavailable, body: `{"status":"ready"}`},
		{name: "malformed JSON", statusCode: http.StatusOK, body: `{"status":`},
		{name: "missing status", statusCode: http.StatusOK, body: `{}`},
		{name: "wrong status", statusCode: http.StatusOK, body: `{"status":"degraded"}`},
		{name: "unknown field", statusCode: http.StatusOK, body: `{"status":"ready","detail":"unexpected"}`},
		{name: "trailing JSON", statusCode: http.StatusOK, body: `{"status":"ready"}{}`},
		{name: "oversized body", statusCode: http.StatusOK, body: fmt.Sprintf(`{"status":"ready","padding":"%s"}`, strings.Repeat("x", maxResponseBodyBytes))},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.statusCode)
				_, _ = w.Write([]byte(test.body))
			}))
			t.Cleanup(server.Close)

			if err := run(context.Background(), server.URL); err == nil {
				t.Fatal("run returned nil for an unhealthy response")
			}
		})
	}
}

func TestRunHonorsContextTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	started := time.Now()
	if err := run(ctx, server.URL); err == nil {
		t.Fatal("run returned nil after the probe timed out")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("run did not promptly honor the context timeout: %v", elapsed)
	}
}

func TestRunDoesNotFollowRedirects(t *testing.T) {
	redirectTargetReached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectTargetReached = true
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}))
	t.Cleanup(target.Close)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	t.Cleanup(server.Close)

	if err := run(context.Background(), server.URL); err == nil {
		t.Fatal("run returned nil for a redirect")
	}
	if redirectTargetReached {
		t.Fatal("run followed a redirect away from the configured health endpoint")
	}
}
