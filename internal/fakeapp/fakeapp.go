// Command fakeapp is a deterministic browser-acceptance server.
// It is intentionally isolated under internal/fakeapp and is never imported by cmd/helio.
package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

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

type fixtureServer struct {
	mu            sync.Mutex
	bootstrapOpen bool
	authenticated bool
	settings      map[string]any
	live          liveState
	subscribers   map[chan []byte]struct{}
}

func main() {
	server := newFixtureServer()
	log.Printf("Helio fakeapp listening on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, server.handler()))
}

func newFixtureServer() *fixtureServer {
	s := &fixtureServer{subscribers: make(map[chan []byte]struct{})}
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
	mux.HandleFunc("GET /api/v1/auth/session", s.session)
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	mux.HandleFunc("GET /api/v1/live", s.getLive)
	mux.HandleFunc("GET /api/v1/live/events", s.events)
	mux.HandleFunc("GET /api/v1/settings", s.getSettings)
	mux.HandleFunc("PUT /api/v1/settings", s.putSettings)
	mux.HandleFunc("GET /api/v1/history", s.history)
	mux.HandleFunc("GET /api/v1/history.csv", s.historyCSV)
	mux.HandleFunc("GET /api/v1/insights", s.insights)
	mux.HandleFunc("GET /api/v1/alerts", s.alerts)
	mux.HandleFunc("GET /api/v1/data/backup", s.backup)
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

func defaultSettings() map[string]any {
	return map[string]any{
		"activeMPPT": []int{1}, "currency": "BRL", "latitude": -23.55, "longitude": -46.63,
		"loggerHost": "192.0.2.44", "loggerPort": 8899, "loggerSerial": "42424242", "modbusSlave": 1,
		"panelCount": 7, "panelWattage": 610, "retentionDays": 730, "tariffMinorPerKWh": 95,
		"timezone": "America/Sao_Paulo",
	}
}

func (s *fixtureServer) reset(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bootstrapOpen = name == "bootstrap-open"
	s.authenticated = !s.bootstrapOpen
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
	s.reset(body.Name)
	w.WriteHeader(http.StatusNoContent)
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
	var body map[string]any
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		writeError(w, 400, "invalid_json", "invalid JSON")
		return
	}
	s.mu.Lock()
	if !s.bootstrapOpen {
		s.mu.Unlock()
		writeError(w, 409, "bootstrap_closed", "initial setup is already complete")
		return
	}
	if settings, ok := body["settings"].(map[string]any); ok {
		s.settings = settings
	}
	s.bootstrapOpen, s.authenticated = false, true
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, credentials())
}

func (s *fixtureServer) login(w http.ResponseWriter, r *http.Request) {
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
	s.authenticated = true
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, credentials())
}

func credentials() map[string]any {
	return map[string]any{"csrfToken": "TEST-CSRF", "expiresAt": "2026-07-15T15:43:00Z", "userId": "TEST-USER", "username": testAdmin}
}

func (s *fixtureServer) session(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	authenticated := s.authenticated
	s.mu.Unlock()
	if !authenticated {
		writeError(w, 401, "unauthorized", "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, credentials())
}

func (s *fixtureServer) logout(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.authenticated = false
	s.mu.Unlock()
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
	settings := cloneMap(s.settings)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, settings)
}

func (s *fixtureServer) putSettings(w http.ResponseWriter, r *http.Request) {
	var settings map[string]any
	if json.NewDecoder(r.Body).Decode(&settings) != nil {
		writeError(w, 400, "invalid_json", "invalid JSON")
		return
	}
	s.mu.Lock()
	s.settings = settings
	saved := cloneMap(settings)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, saved)
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

func (s *fixtureServer) historyCSV(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="helio-history.csv"`)
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"timestamp", "power_w", "energy_today_wh", "status"})
	_ = writer.Write([]string{fixedTimestamp, "2070", "12340", "normal"})
	writer.Flush()
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

func cloneMap(value map[string]any) map[string]any {
	encoded, _ := json.Marshal(value)
	var result map[string]any
	_ = json.Unmarshal(encoded, &result)
	return result
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
