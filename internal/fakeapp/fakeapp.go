// Command fakeapp is a deterministic browser-acceptance server.
// It is intentionally isolated under internal/fakeapp and is never imported by cmd/helio.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/ndelanhese/helio/internal/api"
	"github.com/ndelanhese/helio/internal/config"
	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/webui"
)

const (
	addr            = "127.0.0.1:4173"
	fixedTimestamp  = "2026-07-14T15:42:00Z"
	fixedRecoveryAt = "2026-07-14T15:43:00Z"
	testAdmin       = "TEST_ADMIN"
	testPassword    = "Helio-TEST-2026!"
)

type liveSnapshot struct {
	ObservedAt       string         `json:"observedAt"`
	Status           string         `json:"status"`
	ACPowerW         float64        `json:"acPowerW"`
	EnergyTodayWh    float64        `json:"energyTodayWh"`
	EnergyLifetimeWh float64        `json:"energyLifetimeWh"`
	PV1              map[string]any `json:"pv1"`
	PV2              map[string]any `json:"pv2"`
	Grid             map[string]any `json:"grid"`
	FaultCodes       []uint16       `json:"faultCodes"`
}

type liveState struct {
	Snapshot    *liveSnapshot `json:"snapshot,omitempty"`
	LastSuccess string        `json:"lastSuccess,omitempty"`
	LastError   string        `json:"lastError,omitempty"`
	LastErrorAt string        `json:"lastErrorAt,omitempty"`
	ErrorClass  string        `json:"errorClass,omitempty"`
	Stale       bool          `json:"stale"`
}

type fakeSession struct {
	CSRF     string
	Username string
}

type fixtureServer struct {
	mu            sync.Mutex
	bootstrapOpen bool
	settings      domain.Settings
	live          liveState
	subscribers   map[chan []byte]struct{}
	sessions      map[string]fakeSession
	sessionSerial uint64
}

func main() {
	server := newFixtureServer()
	log.Printf("Helio fakeapp listening on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, server.handler()))
}

func newFixtureServer() *fixtureServer {
	s := &fixtureServer{subscribers: make(map[chan []byte]struct{}), sessions: make(map[string]fakeSession)}
	s.reset("default")
	return s
}

func (s *fixtureServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /__test/scenario", s.scenario)
	mux.HandleFunc("GET /api/v1/bootstrap/status", s.bootstrapStatus)
	mux.HandleFunc("POST /api/v1/bootstrap", s.bootstrap)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("GET /api/v1/auth/session", s.protected(false, s.session))
	mux.HandleFunc("POST /api/v1/auth/logout", s.protected(true, s.logout))
	mux.HandleFunc("GET /api/v1/live", s.protected(false, s.getLive))
	mux.HandleFunc("GET /api/v1/live/events", s.protected(false, s.events))
	mux.HandleFunc("GET /api/v1/settings", s.protected(false, s.getSettings))
	mux.HandleFunc("PUT /api/v1/settings", s.protected(true, s.putSettings))
	mux.HandleFunc("GET /api/v1/history", s.protected(false, s.history))
	mux.HandleFunc("GET /api/v1/history.csv", s.protected(false, s.historyCSV))
	mux.HandleFunc("GET /api/v1/insights", s.protected(false, s.insights))
	mux.HandleFunc("GET /api/v1/alerts", s.protected(false, s.alerts))
	mux.HandleFunc("GET /api/v1/data/backup", s.protected(false, s.backup))
	mux.HandleFunc("GET /health/components", s.components)
	mux.Handle("GET /", productionAssets())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}

func baseSnapshot(power float64, observedAt string) *liveSnapshot {
	return &liveSnapshot{
		ObservedAt: observedAt, Status: "normal", ACPowerW: power,
		EnergyTodayWh: 12340, EnergyLifetimeWh: 4567800,
		PV1:  map[string]any{"active": true, "voltageV": 267.1, "currentA": 8.0, "powerW": power},
		PV2:  map[string]any{"active": false, "voltageV": 0.0, "currentA": 0.0, "powerW": 0.0},
		Grid: map[string]any{"voltageV": 267.1, "frequencyHz": 59.97}, FaultCodes: []uint16{},
	}
}

func defaultSettings() domain.Settings {
	return domain.Settings{ActiveMPPT: []int{1}, Currency: "BRL", Latitude: -23.55, Longitude: -46.63,
		LoggerHost: "192.0.2.44", LoggerPort: 8899, LoggerSerial: "42424242", ModbusSlave: 1,
		PanelCount: 7, PanelWattage: 610, InstalledPowerW: 4270, RetentionDays: 730,
		TariffMinorPerKWh: 95, Timezone: "America/Sao_Paulo"}
}

func (s *fixtureServer) reset(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bootstrapOpen = name == "bootstrap-open"
	s.sessions = make(map[string]fakeSession)
	s.sessionSerial = 0
	if name == "default" || name == "bootstrap-open" || name == "history-gap" {
		s.settings = defaultSettings()
		s.live = liveState{Snapshot: baseSnapshot(2070, fixedTimestamp), LastSuccess: fixedTimestamp, Stale: false}
	}
	switch name {
	case "next-snapshot":
		s.live = liveState{Snapshot: baseSnapshot(2310, "2026-07-14T15:42:30Z"), LastSuccess: "2026-07-14T15:42:30Z", Stale: false}
	case "logger-outage":
		s.live.Stale = true
		s.live.LastError = "TEST logger unavailable"
		s.live.LastErrorAt = fixedRecoveryAt
		s.live.ErrorClass = "communication"
	case "recovery":
		s.live = liveState{Snapshot: baseSnapshot(2450, fixedRecoveryAt), LastSuccess: fixedRecoveryAt, Stale: false}
	}
	s.broadcastLocked(name)
}

func (s *fixtureServer) scenario(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Helio-Test-Token") != "HELIO-E2E-CONTROL-v1" {
		writeError(w, http.StatusForbidden, "forbidden", "test control token is invalid")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "scenario must be JSON")
		return
	}
	allowed := map[string]bool{"default": true, "bootstrap-open": true, "next-snapshot": true, "logger-outage": true, "recovery": true, "history-gap": true}
	if !allowed[body.Name] {
		writeError(w, http.StatusUnprocessableEntity, "invalid_scenario", "unknown deterministic scenario")
		return
	}
	if body.Name == "default" || body.Name == "bootstrap-open" || body.Name == "history-gap" {
		s.reset(body.Name)
	} else {
		s.transition(body.Name)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *fixtureServer) transition(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch name {
	case "next-snapshot":
		s.live = liveState{Snapshot: baseSnapshot(2310, "2026-07-14T15:42:30Z"), LastSuccess: "2026-07-14T15:42:30Z", Stale: false}
	case "logger-outage":
		s.live.Stale = true
		s.live.LastError = "TEST logger unavailable"
		s.live.LastErrorAt = fixedRecoveryAt
		s.live.ErrorClass = "communication"
	case "recovery":
		s.live = liveState{Snapshot: baseSnapshot(2450, fixedRecoveryAt), LastSuccess: fixedRecoveryAt, Stale: false}
	}
	s.broadcastLocked(name)
}

func (s *fixtureServer) broadcastLocked(name string) {
	if name == "default" || name == "bootstrap-open" || name == "history-gap" {
		return
	}
	kind := "state"
	payload := any(s.live)
	if name == "next-snapshot" || name == "recovery" {
		kind = "snapshot"
		payload = map[string]any{"kind": "snapshot", "snapshot": s.live.Snapshot, "state": s.live}
	}
	data, _ := json.Marshal(payload)
	message := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", kind, data))
	for subscriber := range s.subscribers {
		select {
		case subscriber <- message:
		default:
		}
	}
}

func (s *fixtureServer) bootstrapStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	open := s.bootstrapOpen
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]bool{"open": open})
}

func (s *fixtureServer) bootstrap(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "request origin is invalid")
		return
	}
	var body struct {
		Username string          `json:"username"`
		Password string          `json:"password"`
		Settings settingsRequest `json:"settings"`
	}
	if !decodeStrict(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Username) == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_request", "username is required")
		return
	}
	settings, err := body.Settings.domain()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_settings", err.Error())
		return
	}
	settings, err = config.ValidateSettings(settings, true)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_settings", err.Error())
		return
	}
	s.mu.Lock()
	if !s.bootstrapOpen {
		s.mu.Unlock()
		writeError(w, 409, "bootstrap_closed", "initial setup is already complete")
		return
	}
	s.settings = settings
	s.bootstrapOpen = false
	credentials := s.issueSessionLocked(w, testAdmin)
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, credentials)
}

func (s *fixtureServer) login(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "request origin is invalid")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		writeError(w, 400, "invalid_json", "invalid JSON")
		return
	}
	if body.Username != testAdmin || body.Password != testPassword {
		writeError(w, 401, "invalid_credentials", "username or password is invalid")
		return
	}
	s.mu.Lock()
	if s.bootstrapOpen {
		s.mu.Unlock()
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "username or password is invalid")
		return
	}
	credentials := s.issueSessionLocked(w, body.Username)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, credentials)
}

func (s *fixtureServer) issueSessionLocked(w http.ResponseWriter, username string) map[string]any {
	s.sessionSerial++
	token := fmt.Sprintf("TEST-OPAQUE-%06d", s.sessionSerial)
	csrf := fmt.Sprintf("TEST-CSRF-%06d", s.sessionSerial)
	s.sessions[token] = fakeSession{CSRF: csrf, Username: username}
	http.SetCookie(w, &http.Cookie{Name: "helio_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: time.Date(2026, 7, 15, 15, 43, 0, 0, time.UTC)})
	return map[string]any{"csrfToken": csrf, "expiresAt": "2026-07-15T15:43:00Z", "userId": "TEST-USER", "username": username}
}

func (s *fixtureServer) protected(csrf bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		open := s.bootstrapOpen
		cookie, err := r.Cookie("helio_session")
		session, authenticated := s.sessions[cookieValue(cookie, err)]
		s.mu.Unlock()
		if !authenticated {
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		if open {
			writeError(w, http.StatusServiceUnavailable, "bootstrap_required", "initial setup is required")
			return
		}
		if csrf && (r.Header.Get("X-CSRF-Token") != session.CSRF || r.Header.Get("Origin") != "http://"+r.Host) {
			writeError(w, http.StatusForbidden, "forbidden", "request origin or CSRF token is invalid")
			return
		}
		next(w, r)
	}
}

func cookieValue(cookie *http.Cookie, err error) string {
	if err != nil || cookie == nil {
		return ""
	}
	return cookie.Value
}

func (s *fixtureServer) session(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie("helio_session")
	s.mu.Lock()
	session := s.sessions[cookie.Value]
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"csrfToken": session.CSRF, "expiresAt": "2026-07-15T15:43:00Z", "userId": "TEST-USER", "username": session.Username})
}

func (s *fixtureServer) logout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie("helio_session")
	s.mu.Lock()
	delete(s.sessions, cookie.Value)
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "helio_session", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

func (s *fixtureServer) getLive(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	state := s.live
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, state)
}

func (s *fixtureServer) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	updates := make(chan []byte, 4)
	s.mu.Lock()
	s.subscribers[updates] = struct{}{}
	initial, _ := json.Marshal(s.live)
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.subscribers, updates); s.mu.Unlock() }()
	fmt.Fprintf(w, "event: state\ndata: %s\n\n", initial)
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case message := <-updates:
			_, _ = w.Write(message)
			flusher.Flush()
		}
	}
}

func (s *fixtureServer) getSettings(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	settings := s.settings
	settings.ActiveMPPT = append([]int(nil), s.settings.ActiveMPPT...)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, settings)
}

func (s *fixtureServer) putSettings(w http.ResponseWriter, r *http.Request) {
	var body settingsRequest
	if !decodeStrict(w, r, &body) {
		return
	}
	settings, err := body.domain()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_settings", err.Error())
		return
	}
	settings, err = config.ValidateSettings(settings, true)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_settings", err.Error())
		return
	}
	s.mu.Lock()
	s.settings = settings
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, settings)
}

func (s *fixtureServer) history(w http.ResponseWriter, r *http.Request) {
	from, err := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	if err != nil {
		writeError(w, 422, "invalid_range", "invalid range")
		return
	}
	to := r.URL.Query().Get("to")
	resolution := r.URL.Query().Get("resolution")
	var points []map[string]any
	if resolution == "minute" {
		for _, sample := range []struct {
			minute int
			power  float64
		}{{540, 620}, {541, 760}, {542, 880}, {600, 1540}, {601, 1710}, {602, 1810}} {
			points = append(points, map[string]any{"at": from.Add(time.Duration(sample.minute) * time.Minute).UTC().Format(time.RFC3339), "powerW": sample.power})
		}
	} else {
		step := map[string]time.Duration{"hour": time.Hour, "day": 24 * time.Hour, "month": 30 * 24 * time.Hour}[resolution]
		for index := 0; index < 4; index++ {
			points = append(points, map[string]any{"at": from.Add(time.Duration(index) * step).UTC().Format(time.RFC3339), "coveragePct": 92.0, "energyWh": 2100.0, "peakPowerW": 2300.0, "productiveMinutes": 50})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from.UTC().Format(time.RFC3339), "to": to, "resolution": resolution, "points": points})
}

func (s *fixtureServer) historyCSV(w http.ResponseWriter, r *http.Request) {
	from, fromErr := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	to, toErr := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	if fromErr != nil || toErr != nil || !from.Before(to) {
		writeError(w, http.StatusUnprocessableEntity, "invalid_range", "from must be before to and both must be RFC3339 timestamps")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="helio-history.csv"`)
	_, _ = w.Write([]byte(api.HistoryCSVHeader + "\n2026-07-14T12:00:00Z,2070,12340,normal\n"))
}

func (s *fixtureServer) insights(w http.ResponseWriter, _ *http.Request) {
	trend := map[string]any{"direction": "insufficient", "current": 0, "previous": 0, "delta": 0, "deltaPct": 0, "coveragePct": 42, "windowDays": 7}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": "v1", "day": "2026-07-13", "actualWh": 2000, "expectedWh": 10000, "ratio": .2, "confidence": "low", "qualifying": false,
		"evidence":          []map[string]any{{"code": "coverage", "label": "Cobertura da telemetria", "value": 42, "unit": "percent"}},
		"observationWindow": map[string]any{"qualifyingDays": 4, "minimumDays": 7}, "trends": map[string]any{"peakPower": trend, "productiveMinutes": trend},
		"generatedEnergyValue": map[string]any{"minor": 190, "currency": "BRL", "label": "valor estimado da energia gerada", "estimate": true},
	})
}

func (s *fixtureServer) alerts(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	writeJSON(w, http.StatusOK, map[string]any{"version": "v1", "state": state, "limit": 100, "alerts": []any{}})
}

func (s *fixtureServer) backup(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="helio-backup-20260714-154300.db"`)
	_, _ = w.Write([]byte("HELIO-TEST-BACKUP"))
}

func (s *fixtureServer) components(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	stale := s.live.Stale
	s.mu.Unlock()
	logger := "online"
	if stale {
		logger = "offline"
	}
	writeJSON(w, http.StatusOK, map[string]any{"collector": "running", "database": "ok", "logger": logger, "weather": "stale", "collectorUpdatedAt": fixedRecoveryAt, "databaseUpdatedAt": fixedRecoveryAt, "loggerUpdatedAt": fixedRecoveryAt, "weatherUpdatedAt": fixedTimestamp, "weatherFetchedAt": fixedTimestamp})
}

func productionAssets() http.Handler {
	assets, err := fs.Sub(webui.Assets, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean != "." {
			if file, err := assets.Open(clean); err == nil {
				_ = file.Close()
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(assets, "index.html")
		if err != nil {
			http.Error(w, "UI unavailable", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}

type settingsRequest struct {
	LoggerHost        *string  `json:"loggerHost"`
	LoggerSerial      *string  `json:"loggerSerial"`
	LoggerPort        *int     `json:"loggerPort"`
	ModbusSlave       *int     `json:"modbusSlave"`
	PanelCount        *int     `json:"panelCount"`
	PanelWattage      *int     `json:"panelWattage"`
	ActiveMPPT        *[]int   `json:"activeMPPT"`
	Latitude          *float64 `json:"latitude"`
	Longitude         *float64 `json:"longitude"`
	Timezone          *string  `json:"timezone"`
	Currency          *string  `json:"currency"`
	TariffMinorPerKWh *int64   `json:"tariffMinorPerKWh"`
	RetentionDays     *int     `json:"retentionDays"`
}

func (d settingsRequest) domain() (domain.Settings, error) {
	required := []struct {
		name    string
		present bool
	}{{"loggerHost", d.LoggerHost != nil}, {"loggerSerial", d.LoggerSerial != nil}, {"panelCount", d.PanelCount != nil}, {"panelWattage", d.PanelWattage != nil}, {"activeMPPT", d.ActiveMPPT != nil}, {"latitude", d.Latitude != nil}, {"longitude", d.Longitude != nil}, {"timezone", d.Timezone != nil}, {"currency", d.Currency != nil}}
	for _, field := range required {
		if !field.present {
			return domain.Settings{}, fmt.Errorf("%s is required", field.name)
		}
	}
	if d.LoggerPort != nil && *d.LoggerPort == 0 {
		return domain.Settings{}, fmt.Errorf("loggerPort must not be zero when provided")
	}
	if d.ModbusSlave != nil && *d.ModbusSlave == 0 {
		return domain.Settings{}, fmt.Errorf("modbusSlave must not be zero when provided")
	}
	if d.RetentionDays != nil && *d.RetentionDays == 0 {
		return domain.Settings{}, fmt.Errorf("retentionDays must not be zero when provided")
	}
	value := domain.Settings{LoggerHost: *d.LoggerHost, LoggerSerial: *d.LoggerSerial, PanelCount: *d.PanelCount,
		PanelWattage: *d.PanelWattage, ActiveMPPT: append([]int(nil), (*d.ActiveMPPT)...), Latitude: *d.Latitude,
		Longitude: *d.Longitude, Timezone: *d.Timezone, Currency: *d.Currency}
	if d.LoggerPort != nil {
		value.LoggerPort = *d.LoggerPort
	}
	if d.ModbusSlave != nil {
		value.ModbusSlave = *d.ModbusSlave
	}
	if d.TariffMinorPerKWh != nil {
		value.TariffMinorPerKWh = *d.TariffMinorPerKWh
	}
	if d.RetentionDays != nil {
		value.RetentionDays = *d.RetentionDays
	}
	return value, nil
}

func decodeStrict(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must contain one JSON value")
		return false
	}
	return true
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

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
