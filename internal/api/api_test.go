package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	handler  http.Handler
	db       *storage.DB
	dbDir    string
	repo     *storage.TelemetryRepository
	hub      *collector.Hub
	shutdown context.CancelFunc
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	dbDir := t.TempDir()
	db, err := storage.Open(context.Background(), filepath.Join(dbDir, "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	hub := collector.NewHub()
	repo := storage.NewTelemetryRepository(db, time.UTC)
	manager := auth.NewManager(db)
	shutdownContext, shutdown := context.WithCancel(context.Background())
	return fixture{handler: api.New(api.Dependencies{
		Auth: manager, Store: db, History: repo, Hub: hub,
		Latest:          func() collector.State { return collector.State{} },
		Now:             func() time.Time { return time.Date(2026, 7, 14, 15, 4, 5, 0, time.UTC) },
		ShutdownContext: shutdownContext,
		ApplySettings: func(ctx context.Context, settings domain.Settings, actor string) error {
			return db.ApplySettings(ctx, settings, actor, false)
		},
	}), db: db, dbDir: dbDir, repo: repo, hub: hub, shutdown: shutdown}
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
	var sessionBody struct {
		CSRFToken string `json:"csrfToken"`
		Username  string `json:"username"`
	}
	if err := json.Unmarshal(session.Body.Bytes(), &sessionBody); err != nil {
		t.Fatal(err)
	}
	if session.Code != http.StatusOK || sessionBody.Username != "Admin" || sessionBody.CSRFToken == "" || sessionBody.CSRFToken == csrf || session.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("session: %d %s", session.Code, session.Body.String())
	}
	stale := request(t, f.handler, http.MethodPost, "/api/v1/auth/logout", "", cookie, csrf)
	if stale.Code != http.StatusForbidden {
		t.Fatalf("stale csrf remained valid: %d", stale.Code)
	}
	logout := request(t, f.handler, http.MethodPost, "/api/v1/auth/logout", "", cookie, sessionBody.CSRFToken)
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
	if csv.Code != http.StatusOK || !strings.HasPrefix(csv.Body.String(), api.HistoryCSVHeader+"\n") {
		t.Fatalf("csv: %d %q", csv.Code, csv.Body.String())
	}
	backup := request(t, f.handler, http.MethodGet, "/api/v1/data/backup", "", cookie, "")
	if backup.Code != http.StatusOK || backup.Header().Get("Content-Type") != "application/vnd.sqlite3" || !bytes.HasPrefix(backup.Body.Bytes(), []byte("SQLite format 3\x00")) {
		t.Fatalf("backup: %d %q", backup.Code, backup.Header())
	}
}

func TestHistoryResolutionReadsPersistedSummaryAfterRawRetention(t *testing.T) {
	f := newFixture(t)
	cookie, _ := bootstrap(t, f)
	base := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	for minute, power := range []float64{100, 200} {
		if err := f.repo.SaveMinute(context.Background(), domain.TelemetrySnapshot{ObservedAt: base.Add(time.Duration(minute) * time.Minute), ACPowerW: power}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.repo.AggregateHour(context.Background(), base, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.PruneBefore(context.Background(), base.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	target := "/api/v1/history?from=" + base.Format(time.RFC3339) + "&to=" + base.Add(time.Hour).Format(time.RFC3339) + "&resolution=hour"
	rec := request(t, f.handler, http.MethodGet, target, "", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("history: %d %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Points []domain.AggregatePoint `json:"points"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Points) != 1 || response.Points[0].EnergyWh != 2.5 || !response.Points[0].At.Equal(base) {
		t.Fatalf("hour points: %#v", response.Points)
	}
}

func TestDailySummaryProducerAndAPIShareConfiguredTimezone(t *testing.T) {
	f := newFixture(t)
	cookie, _ := bootstrap(t, f)
	location, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	f.repo.SetLocation(location)
	start := time.Date(2026, 1, 31, 0, 0, 0, 0, location)
	for minute := range 2 {
		if err := f.repo.SaveMinute(context.Background(), domain.TelemetrySnapshot{ObservedAt: start.Add(time.Duration(minute) * time.Minute), ACPowerW: 60}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.repo.AggregateHour(context.Background(), start, start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.AggregateDay(context.Background(), start, start.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	target := "/api/v1/history?from=" + start.UTC().Format(time.RFC3339) + "&to=" + start.AddDate(0, 0, 1).UTC().Format(time.RFC3339) + "&resolution=day"
	rec := request(t, f.handler, http.MethodGet, target, "", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("history: %d %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Points []domain.AggregatePoint `json:"points"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Points) != 1 || !response.Points[0].At.Equal(start.UTC()) {
		t.Fatalf("daily points=%#v", response.Points)
	}
}

type auditFailStore struct{ *storage.DB }

func (auditFailStore) RecordAudit(context.Context, string, string, any) error {
	return errors.New("audit unavailable")
}

type backupFailStore struct{ *storage.DB }

func (backupFailStore) Backup(context.Context, io.Writer) error { return errors.New("backup failed") }
func (backupFailStore) PrepareBackup(context.Context) (io.ReadCloser, error) {
	return nil, errors.New("backup failed")
}

func TestBackupPreparationFailureIsStructuredBeforeHeaders(t *testing.T) {
	f := newFixture(t)
	cookie, _ := bootstrap(t, f)
	handler := api.New(api.Dependencies{Auth: auth.NewManager(f.db), Store: backupFailStore{f.db}, History: f.repo, Hub: f.hub})
	rec := request(t, handler, http.MethodGet, "/api/v1/data/backup", "", cookie, "")
	if rec.Code != http.StatusInternalServerError || rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Content-Disposition") != "" {
		t.Fatalf("backup preparation failure: %d %q %s", rec.Code, rec.Header(), rec.Body.String())
	}
}

type settingsReadFailStore struct{ *storage.DB }

func (settingsReadFailStore) GetSettings(context.Context, ...bool) (domain.Settings, error) {
	return domain.Settings{}, errors.New("settings read should not be needed")
}

func TestSummaryHistoryUsesRepositoryCalendarSnapshotWithoutSeparateSettingsRead(t *testing.T) {
	f := newFixture(t)
	cookie, _ := bootstrap(t, f)
	base := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	for minute := range 2 {
		if err := f.repo.SaveMinute(context.Background(), domain.TelemetrySnapshot{ObservedAt: base.Add(time.Duration(minute) * time.Minute), ACPowerW: 60}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.repo.AggregateHour(context.Background(), base, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	handler := api.New(api.Dependencies{Auth: auth.NewManager(f.db), Store: settingsReadFailStore{f.db}, History: f.repo, Hub: f.hub})
	target := "/api/v1/history?from=" + base.Format(time.RFC3339) + "&to=" + base.Add(time.Hour).Format(time.RFC3339) + "&resolution=hour"
	rec := request(t, handler, http.MethodGet, target, "", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("summary history: %d %s", rec.Code, rec.Body.String())
	}
}

func TestComponentHealthIncludesExplicitWeatherState(t *testing.T) {
	f := newFixture(t)
	rec := request(t, f.handler, http.MethodGet, "/health/components", "", nil, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"weather":"unavailable"`) {
		t.Fatalf("components: %d %s", rec.Code, rec.Body.String())
	}
}

func TestComponentHealthWeatherUsesOnlyPublicAvailabilityEnum(t *testing.T) {
	fetched := "2026-07-14T13:00:00Z"
	for _, state := range []string{"available", "stale", "unavailable"} {
		handler := api.New(api.Dependencies{Components: func(context.Context) api.ComponentStatus {
			return api.ComponentStatus{Database: "ok", Logger: "online", Collector: "running", Weather: state, WeatherFetchedAt: fetched}
		}})
		rec := request(t, handler, http.MethodGet, "/health/components", "", nil, "")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"weather":"`+state+`"`) || !strings.Contains(rec.Body.String(), `"weatherFetchedAt":"`+fetched+`"`) {
			t.Fatalf("weather %s: %d %s", state, rec.Code, rec.Body.String())
		}
	}
}

func TestComponentHealthExposesAnalysisPipelineState(t *testing.T) {
	handler := api.New(api.Dependencies{Components: func(context.Context) api.ComponentStatus {
		return api.ComponentStatus{Database: "ok", Logger: "online", Collector: "running", Weather: "available",
			Alerts: "unavailable", AlertsError: "evaluation", Analysis: "unavailable", AnalysisError: "panic"}
	}})
	rec := request(t, handler, http.MethodGet, "/health/components", "", nil, "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"alerts":"unavailable"`) || !strings.Contains(body, `"alertsErrorClass":"evaluation"`) ||
		!strings.Contains(body, `"analysis":"unavailable"`) || !strings.Contains(body, `"analysisErrorClass":"panic"`) {
		t.Fatalf("pipeline health: %d %s", rec.Code, body)
	}
}

func TestComponentHealthExposesSanitizedFailureClassAndTimestamp(t *testing.T) {
	at := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	handler := api.New(api.Dependencies{Latest: func() collector.State {
		return collector.State{LastError: "dial 192.168.1.50 raw-frame", LastErrorAt: at, ErrorClass: "communication"}
	}})
	rec := request(t, handler, http.MethodGet, "/health/components", "", nil, "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"loggerErrorClass":"communication"`) || !strings.Contains(body, `"loggerUpdatedAt":"2026-07-14T12:00:00Z"`) {
		t.Fatalf("health=%d %s", rec.Code, body)
	}
	if strings.Contains(body, "192.168.1.50") || strings.Contains(body, "raw-frame") {
		t.Fatalf("health leaked private error: %s", body)
	}
}

func TestExportsDoNotCommitResponseWhenRequiredAuditFails(t *testing.T) {
	f := newFixture(t)
	cookie, _ := bootstrap(t, f)
	manager := auth.NewManager(f.db)
	handler := api.New(api.Dependencies{Auth: manager, Store: auditFailStore{f.db}, History: f.repo, Hub: f.hub})
	// Use the session created by bootstrap; managers share the durable session store.
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	query := "?from=" + now.Add(-time.Minute).Format(time.RFC3339) + "&to=" + now.Add(time.Minute).Format(time.RFC3339)
	for _, target := range []string{"/api/v1/history.csv" + query, "/api/v1/data/backup"} {
		rec := request(t, handler, http.MethodGet, target, "", cookie, "")
		if rec.Code != http.StatusInternalServerError || rec.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("%s audit failure: %d %q", target, rec.Code, rec.Header())
		}
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

func TestSSEStopsPromptlyWhenApplicationShutsDown(t *testing.T) {
	f := newFixture(t)
	cookie, _ := bootstrap(t, f)
	server := httptest.NewServer(f.handler)
	defer server.Close()
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/live/events", nil)
	req.AddCookie(cookie)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 512)
	if _, err := resp.Body.Read(buf); err != nil {
		t.Fatal(err)
	}
	f.shutdown()
	done := make(chan error, 1)
	go func() { _, err := resp.Body.Read(buf); done <- err }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE remained active after application shutdown")
	}
}
