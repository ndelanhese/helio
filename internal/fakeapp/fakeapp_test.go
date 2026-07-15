package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/api"
)

func TestHistoryCSVHeaderMatchesProductionContract(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/api/v1/history.csv?from=2026-07-14T00:00:00Z&to=2026-07-15T00:00:00Z", nil)

	newFixtureServer().historyCSV(recorder, request)

	if got, want := recorder.Body.String(), api.HistoryCSVHeader+"\n"; !strings.HasPrefix(got, want) {
		t.Fatalf("fake CSV header drifted from production: got %q, want prefix %q", got, want)
	}
}

func TestHistoryCSVRejectsProductionOverLimitRange(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/api/v1/history.csv?from=2026-01-01T00:00:00Z&to=2026-02-02T00:00:00Z", nil)
	newFixtureServer().historyCSV(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "CSV history cannot exceed 31 days") {
		t.Fatalf("over-limit CSV=%d %s", recorder.Code, recorder.Body.String())
	}
}

func TestFakeappPasswordConfirmationMatchesSensitiveSettingsContract(t *testing.T) {
	server := newFixtureServer()
	now := time.Date(2026, 7, 14, 15, 42, 0, 0, time.UTC)
	server.now = func() time.Time { return now }
	handler := server.handler()
	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"TEST_ADMIN","password":"Helio-TEST-2026!"}`))
	login.Host = "helio.test"
	login.Header.Set("Origin", "http://helio.test")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, login)
	var credentials struct {
		CSRF string `json:"csrfToken"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &credentials); err != nil {
		t.Fatal(err)
	}
	cookies := loginRec.Result().Cookies()
	if loginRec.Code != http.StatusOK || len(cookies) != 1 {
		t.Fatalf("login=%d cookies=%v", loginRec.Code, cookies)
	}

	put := func(host string) *httptest.ResponseRecorder {
		body := strings.Replace(settingsRequestJSON(), `"loggerHost":"192.0.2.44"`, `"loggerHost":"`+host+`"`, 1)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
		req.Host = "helio.test"
		req.Header.Set("Origin", "http://helio.test")
		req.Header.Set("X-CSRF-Token", credentials.CSRF)
		req.AddCookie(cookies[0])
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := put("192.0.2.55"); rec.Code != http.StatusForbidden {
		t.Fatalf("unconfirmed=%d %s", rec.Code, rec.Body.String())
	}
	confirm := func(password string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/confirm-password", strings.NewReader(`{"password":"`+password+`"}`))
		req.Host = "helio.test"
		req.Header.Set("Origin", "http://helio.test")
		req.Header.Set("X-CSRF-Token", credentials.CSRF)
		req.AddCookie(cookies[0])
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := confirm(testPassword); rec.Code != http.StatusNoContent || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("confirm=%d cookies=%v", rec.Code, rec.Result().Cookies())
	}
	if rec := confirm("wrong-after-success"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong confirmation=%d %s", rec.Code, rec.Body.String())
	}
	if rec := put("192.0.2.55"); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong password retained confirmation=%d %s", rec.Code, rec.Body.String())
	}
	if rec := confirm(testPassword); rec.Code != http.StatusNoContent {
		t.Fatalf("second confirmation=%d %s", rec.Code, rec.Body.String())
	}
	now = now.Add(5*time.Minute + time.Nanosecond)
	if rec := put("192.0.2.55"); rec.Code != http.StatusForbidden {
		t.Fatalf("expired confirmation=%d %s", rec.Code, rec.Body.String())
	}
	if rec := confirm(testPassword); rec.Code != http.StatusNoContent {
		t.Fatalf("third confirmation=%d %s", rec.Code, rec.Body.String())
	}
	if rec := put("192.0.2.55"); rec.Code != http.StatusOK {
		t.Fatalf("confirmed=%d %s", rec.Code, rec.Body.String())
	}
	if rec := put("192.0.2.66"); rec.Code != http.StatusForbidden {
		t.Fatalf("confirmation was reusable=%d %s", rec.Code, rec.Body.String())
	}
}

func settingsRequestJSON() string {
	return `{"loggerHost":"192.0.2.44","loggerSerial":"42424242","loggerPort":8899,"modbusSlave":1,"panelCount":7,"panelWattage":610,"activeMPPT":[1],"latitude":-10,"longitude":-20,"timezone":"America/Sao_Paulo","currency":"BRL","tariffMinorPerKWh":95,"retentionDays":730}`
}
