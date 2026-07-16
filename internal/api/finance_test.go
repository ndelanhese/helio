package api_test

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

const validCycleJSON = `{"readingStart":"2026-07-01T00:00:00Z","readingEnd":"2026-07-15T00:00:00Z","activeConsumptionKWh":150,"injectedKWh":20,"creditsUsedKWh":10,"creditBalanceKWh":10,"totalPaidMinor":12345}`

func TestCreateCycleRequiresCSRFAndReturnsProjection(t *testing.T) {
	f := newFixture(t)
	cookie, csrf := bootstrap(t, f)
	approveFinanceTariff(t, f, cookie, csrf)

	missingCSRF := request(t, f.handler, http.MethodPost, "/api/v1/finance/cycles", validCycleJSON, cookie, "")
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing csrf: %d %s", missingCSRF.Code, missingCSRF.Body.String())
	}
	response := request(t, f.handler, http.MethodPost, "/api/v1/finance/cycles", validCycleJSON, cookie, csrf)
	if response.Code != http.StatusCreated || response.Header().Get("Cache-Control") != "no-store" || !containsJSON(response.Body.String(), `"isEstimate":true`) {
		t.Fatalf("create cycle: %d %s", response.Code, response.Body.String())
	}
}

func TestProposalApprovalIsAudited(t *testing.T) {
	f := newFixture(t)
	cookie, csrf := bootstrap(t, f)
	proposal, err := f.finance.CreateProposal(context.Background(), domain.TariffProposal{
		Distributor: "COPEL", EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), EffectiveTo: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		ConsumptionTEMicrosPerKWh: 100_000, ConsumptionTUSDMicrosPerKWh: 100_000,
		CompensationTEMicrosPerKWh: 100_000, AvailabilityKWh: 30, ParserVersion: "test", RetrievedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, f.handler, http.MethodPost, "/api/v1/finance/tariff-proposals/"+strconv.FormatInt(proposal.ID, 10)+"/approve", `{}`, cookie, csrf)
	if response.Code != http.StatusCreated {
		t.Fatalf("approve: %d %s", response.Code, response.Body.String())
	}
	backup := request(t, f.handler, http.MethodGet, "/api/v1/data/backup", "", cookie, "")
	path := filepath.Join(t.TempDir(), "audit.db")
	if err := os.WriteFile(path, backup.Body.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM action_audit WHERE action='tariff.approve'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("tariff approval audits=%d", count)
	}
}

func approveFinanceTariff(t *testing.T, f fixture, cookie *http.Cookie, csrf string) {
	t.Helper()
	proposal, err := f.finance.CreateProposal(context.Background(), domain.TariffProposal{
		Distributor: "COPEL", EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), EffectiveTo: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		ConsumptionTEMicrosPerKWh: 100_000, ConsumptionTUSDMicrosPerKWh: 100_000,
		CompensationTEMicrosPerKWh: 100_000, AvailabilityKWh: 30, ParserVersion: "test", RetrievedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, f.handler, http.MethodPost, "/api/v1/finance/tariff-proposals/"+strconv.FormatInt(proposal.ID, 10)+"/approve", `{}`, cookie, csrf)
	if response.Code != http.StatusCreated {
		t.Fatalf("approve tariff proposal: %d %s", response.Code, response.Body.String())
	}
}

func containsJSON(body, fragment string) bool { return strings.Contains(body, fragment) }
