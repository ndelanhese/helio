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

const validCycleJSON = `{"readingStart":"2026-07-01T00:00:00Z","readingEnd":"2026-07-15T00:00:00Z","activeConsumptionKWh":150,"injectedKWh":20,"creditsUsedKWh":10,"creditBalanceKWh":10,"flagChargeMinor":50,"totalPaidMinor":12345}`

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

func TestCreateCycleRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"absent field", `{"readingStart":"2026-07-01T00:00:00Z","readingEnd":"2026-07-15T00:00:00Z","activeConsumptionKWh":150,"injectedKWh":20,"creditsUsedKWh":10,"creditBalanceKWh":10}`},
		{"unknown field", validCycleJSON[:len(validCycleJSON)-1] + `,"unexpected":true}`},
		{"negative kwh", strings.Replace(validCycleJSON, `"injectedKWh":20`, `"injectedKWh":-1`, 1)},
		{"negative money", strings.Replace(validCycleJSON, `"totalPaidMinor":12345`, `"totalPaidMinor":-1`, 1)},
		{"fractional kwh", strings.Replace(validCycleJSON, `"activeConsumptionKWh":150`, `"activeConsumptionKWh":1.5`, 1)},
		{"fractional money", strings.Replace(validCycleJSON, `"totalPaidMinor":12345`, `"totalPaidMinor":12.5`, 1)},
		{"reversed dates", strings.Replace(validCycleJSON, `"readingEnd":"2026-07-15T00:00:00Z"`, `"readingEnd":"2026-06-15T00:00:00Z"`, 1)},
		{"non RFC3339 date", strings.Replace(validCycleJSON, `"readingStart":"2026-07-01T00:00:00Z"`, `"readingStart":"2026-07-01"`, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			cookie, csrf := bootstrap(t, f)
			response := request(t, f.handler, http.MethodPost, "/api/v1/finance/cycles", test.body, cookie, csrf)
			if response.Code != http.StatusUnprocessableEntity || !containsJSON(response.Body.String(), `"code":"invalid_finance_cycle"`) {
				t.Fatalf("invalid cycle: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestCreateCycleRejectsBodyOverLimitIncludingTrailingWhitespace(t *testing.T) {
	f := newFixture(t)
	cookie, csrf := bootstrap(t, f)
	body := validCycleJSON + strings.Repeat(" ", 64<<10)
	response := request(t, f.handler, http.MethodPost, "/api/v1/finance/cycles", body, cookie, csrf)
	if response.Code != http.StatusRequestEntityTooLarge || !containsJSON(response.Body.String(), `"code":"request_too_large"`) {
		t.Fatalf("oversized cycle: %d %s", response.Code, response.Body.String())
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
