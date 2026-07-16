package tariffs_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/tariffs"
)

func TestParseCopelFixtureCreatesCandidate(t *testing.T) {
	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	got, err := tariffs.ParseCopelGroupB(fixture(t, "copel-group-b.html"), tariffs.Selection{Class: "B1", Subclass: "residential"}, now)
	if err != nil {
		t.Fatalf("ParseCopelGroupB() error = %v", err)
	}
	if got.Candidate.Distributor != "COPEL" {
		t.Errorf("Candidate.Distributor = %q, want COPEL", got.Candidate.Distributor)
	}
	if got.Candidate.ConsumptionTEMicrosPerKWh != 389503 || got.Candidate.ConsumptionTUSDMicrosPerKWh != 538944 {
		t.Errorf("candidate rates = (%d, %d), want (389503, 538944)", got.Candidate.ConsumptionTEMicrosPerKWh, got.Candidate.ConsumptionTUSDMicrosPerKWh)
	}
}

func TestParseCopelGroupBRejectsMissingDateRangeAndZeroRates(t *testing.T) {
	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	page := []byte(`<table><tr><td>B1</td><td>Residencial</td><td>0,000000</td><td>0,538944</td></tr></table>`)
	if _, err := tariffs.ParseCopelGroupB(page, tariffs.Selection{Class: "B1", Subclass: "residential"}, now); err == nil {
		t.Fatal("ParseCopelGroupB() error = nil for absent dates and zero rate")
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return contents
}
