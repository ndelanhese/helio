package main

import (
	"net/http/httptest"
	"strings"
	"testing"

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
