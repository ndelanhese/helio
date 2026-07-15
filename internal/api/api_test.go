package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/api"
	"github.com/ndelanhese/helio/internal/auth"
	"github.com/ndelanhese/helio/internal/collector"
	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/storage"
)

type fixture struct {
	handler http.Handler
	db      *storage.DB
	repo    *storage.TelemetryRepository
	hub     *collector.Hub
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	hub := collector.NewHub()
	repo := storage.NewTelemetryRepository(db, time.UTC)
	manager := auth.NewManager(db)
	return fixture{handler: api.New(api.Dependencies{
		Auth: manager, Store: db, History: repo, Hub: hub,
		Latest: func() collector.State { return collector.State{} },
		Now:    func() time.Time { return time.Date(2026, 7, 14, 15, 4, 5, 0, time.UTC) },
	}), db: db, repo: repo, hub: hub}
}

func request(t *testing.T, h http.Handler, method, target, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.RemoteAddr = "192.0.2.10:4321"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Origin", "http://"+req.Host)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const settingsJSON = `"settings":{"loggerHost":"192.168.1.50","loggerSerial":"123","panelCount":7,"panelWattage":610,"activeMPPT":[1],"latitude":-23.5,"longitude":-46.6,"timezone":"America/Sao_Paulo","currency":"BRL","tariffMinorPerKWh":95}`

func bootstrap(t *testing.T, f fixture) (*http.Cookie, string) {
	t.Helper()
	rec := request(t, f.handler, http.MethodPost, "/api/v1/bootstrap", `{"username":"Admin","password":"correct horse battery staple",`+settingsJSON+`}`, nil, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap: %d %s", rec.Code, rec.Body.String())
	}
	var response struct {
		CSRFToken string `json:"csrfToken"`
		Token     string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Token != "" || response.CSRFToken == "" {
		t.Fatalf("private token leaked or csrf absent: %#v", response)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "helio_session" {
			return c, response.CSRFToken
		}
	}
	t.Fatal("session cookie missing")
	return nil, ""
}

func TestBootstrapIsAtomicStrictAndCloses(t *testing.T) {
	f := newFixture(t)
	status := request(t, f.handler, http.MethodGet, "/api/v1/bootstrap/status", "", nil, "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"open":true`) {
		t.Fatalf("status: %d %s", status.Code, status.Body.String())
	}
	bad := request(t, f.handler, http.MethodPost, "/api/v1/bootstrap", `{"username":"Admin","password":"correct horse battery staple",`+settingsJSON+`,"unknown":true}`, nil, "")
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", bad.Code)
	}
	open, _ := f.db.BootstrapOpen(context.Background())
	if !open {
		t.Fatal("invalid bootstrap partially committed")
	}
	bootstrap(t, f)
	closed := request(t, f.handler, http.MethodPost, "/api/v1/bootstrap", `{"username":"Other","password":"correct horse battery staple",`+settingsJSON+`}`, nil, "")
	if closed.Code != http.StatusConflict {
		t.Fatalf("second bootstrap: %d", closed.Code)
	}
}

func TestAuthTopologyCSRFAndRequestMetadata(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{"/api/v1/live", "/api/v1/settings", "/api/v1/data/backup"} {
		rec := request(t, f.handler, http.MethodGet, path, "", nil, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: %d", path, rec.Code)
		}
		if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Request-ID") == "" {
			t.Fatalf("metadata missing on %s", path)
		}
	}
	cookie, csrf := bootstrap(t, f)
	missing := request(t, f.handler, http.MethodPut, "/api/v1/settings", `{`+settingsJSON+`}`, cookie, "")
	if missing.Code != http.StatusForbidden {
		t.Fatalf("missing csrf: %d", missing.Code)
	}
	session := request(t, f.handler, http.MethodGet, "/api/v1/auth/session", "", cookie, "")
	if session.Code != http.StatusOK || !strings.Contains(session.Body.String(), `"username":"Admin"`) {
		t.Fatalf("session: %d %s", session.Code, session.Body.String())
	}
	logout := request(t, f.handler, http.MethodPost, "/api/v1/auth/logout", "", cookie, csrf)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s", logout.Code, logout.Body.String())
	}
}

func TestSettingsPresenceRangeHistoryCSVAndBackup(t *testing.T) {
	f := newFixture(t)
	cookie, csrf := bootstrap(t, f)
	zero := request(t, f.handler, http.MethodPut, "/api/v1/settings", `{"loggerHost":"192.168.1.50","loggerSerial":"123","loggerPort":0,"panelCount":7,"panelWattage":610,"activeMPPT":[1],"latitude":-23.5,"longitude":-46.6,"timezone":"America/Sao_Paulo","currency":"BRL"}`, cookie, csrf)
	if zero.Code != http.StatusUnprocessableEntity {
		t.Fatalf("explicit zero: %d %s", zero.Code, zero.Body.String())
	}
	malformed := request(t, f.handler, http.MethodPut, "/api/v1/settings", `{"loggerHost":`, cookie, csrf)
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed: %d", malformed.Code)
	}
	invalid := request(t, f.handler, http.MethodGet, "/api/v1/history?from=bad&to=bad&resolution=minute", "", cookie, "")
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid range: %d", invalid.Code)
	}
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	tooLong := request(t, f.handler, http.MethodGet, "/api/v1/history?from="+from.Format(time.RFC3339)+"&to="+from.Add(367*24*time.Hour).Format(time.RFC3339)+"&resolution=minute", "", cookie, "")
	if tooLong.Code != http.StatusUnprocessableEntity {
		t.Fatalf("long minute range: %d", tooLong.Code)
	}
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	if err := f.repo.SaveMinute(context.Background(), domain.TelemetrySnapshot{ObservedAt: now, ACPowerW: 123, EnergyTodayWh: 456, Status: "normal"}); err != nil {
		t.Fatal(err)
	}
	query := "?from=" + now.Add(-time.Minute).Format(time.RFC3339) + "&to=" + now.Add(time.Minute).Format(time.RFC3339)
	csv := request(t, f.handler, http.MethodGet, "/api/v1/history.csv"+query, "", cookie, "")
	if csv.Code != http.StatusOK || !strings.HasPrefix(csv.Body.String(), "timestamp,power_w,energy_today_wh,status\n") {
		t.Fatalf("csv: %d %q", csv.Code, csv.Body.String())
	}
	backup := request(t, f.handler, http.MethodGet, "/api/v1/data/backup", "", cookie, "")
	if backup.Code != http.StatusOK || backup.Header().Get("Content-Type") != "application/vnd.sqlite3" || !bytes.HasPrefix(backup.Body.Bytes(), []byte("SQLite format 3\x00")) {
		t.Fatalf("backup: %d %q", backup.Code, backup.Header())
	}
}

func TestSSEInitialStateSnapshotAndCancellation(t *testing.T) {
	f := newFixture(t)
	cookie, _ := bootstrap(t, f)
	server := httptest.NewServer(f.handler)
	defer server.Close()
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/live/events", nil)
	req.AddCookie(cookie)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 1024)
	n, err := resp.Body.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	initial := string(buf[:n])
	if !strings.Contains(initial, "retry: 5000") || !strings.Contains(initial, "event: state") {
		t.Fatalf("initial SSE: %q", initial)
	}
	f.hub.Publish(collector.Event{Kind: "snapshot", Snapshot: &domain.TelemetrySnapshot{ObservedAt: time.Now().UTC()}})
	n, err = resp.Body.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf[:n]), "event: snapshot") {
		t.Fatalf("snapshot SSE: %q", buf[:n])
	}
	cancel()
	_, _ = io.Copy(io.Discard, resp.Body)
}
