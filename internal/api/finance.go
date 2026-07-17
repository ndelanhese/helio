package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ndelanhese/helio/internal/auth"
	"github.com/ndelanhese/helio/internal/domain"
)

type financeSummaryResponse struct {
	LatestProjection *financialProjectionResponse `json:"latestProjection"`
	Cycles           []billingCycleResponse       `json:"cycles"`
	CreditBalanceKWh int64                        `json:"creditBalanceKWh"`
	NextCreditExpiry *string                      `json:"nextCreditExpiry"`
}

type billingCycleResponse struct {
	ID                   int64  `json:"id"`
	ReadingStart         string `json:"readingStart"`
	ReadingEnd           string `json:"readingEnd"`
	ActiveConsumptionKWh int64  `json:"activeConsumptionKWh"`
	InjectedKWh          int64  `json:"injectedKWh"`
	CreditsUsedKWh       int64  `json:"creditsUsedKWh"`
	CreditBalanceKWh     int64  `json:"creditBalanceKWh"`
	TotalPaidMinor       int64  `json:"totalPaidMinor"`
	FlagChargeMinor      int64  `json:"flagChargeMinor"`
	TariffVersionID      int64  `json:"tariffVersionId"`
}

type financialProjectionResponse struct {
	ID                            int64        `json:"id"`
	BillingCycleID                int64        `json:"billingCycleId"`
	TariffVersionID               int64        `json:"tariffVersionId"`
	ConsumptionMinor              int64        `json:"consumptionMinor"`
	CompensationMinor             int64        `json:"compensationMinor"`
	FlagMinor                     int64        `json:"flagMinor"`
	FlagChargeMinor               int64        `json:"flagChargeMinor"`
	TaxesMinor                    int64        `json:"taxesMinor"`
	CIPMinor                      int64        `json:"cipMinor"`
	TotalMinor                    int64        `json:"totalMinor"`
	WithoutSolarCompensationMinor int64        `json:"withoutSolarCompensationMinor"`
	IsEstimate                    bool         `json:"isEstimate"`
	CalculatedAt                  string       `json:"calculatedAt"`
	DisplayTotal                  string       `json:"displayTotal"`
	DisplayWithoutSolar           string       `json:"displayWithoutSolar"`
	DisplayRows                   []displayRow `json:"displayRows"`
}
type displayRow struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type tariffProposalResponse struct {
	ID                           int64           `json:"id"`
	Distributor                  string          `json:"distributor"`
	EffectiveFrom                string          `json:"effectiveFrom"`
	EffectiveTo                  string          `json:"effectiveTo"`
	ConsumptionTEMicrosPerKWh    int64           `json:"consumptionTEMicrosPerKWh"`
	ConsumptionTUSDMicrosPerKWh  int64           `json:"consumptionTUSDMicrosPerKWh"`
	CompensationTEMicrosPerKWh   int64           `json:"compensationTEMicrosPerKWh"`
	CompensationTUSDMicrosPerKWh int64           `json:"compensationTUSDMicrosPerKWh"`
	FlagMicrosPerKWh             int64           `json:"flagMicrosPerKWh"`
	AvailabilityKWh              int             `json:"availabilityKWh"`
	CIPMinor                     int64           `json:"cipMinor"`
	SourceURL                    string          `json:"sourceUrl"`
	ParserVersion                string          `json:"parserVersion"`
	RetrievedAt                  string          `json:"retrievedAt"`
	ApprovedAt                   *string         `json:"approvedAt"`
	DisplayRates                 []tariffRateRow `json:"displayRates"`
}
type tariffRateRow struct {
	Label    string `json:"label"`
	Approved string `json:"approved"`
	Proposal string `json:"proposal"`
	Delta    string `json:"delta"`
}

type tariffVersionResponse struct {
	ID                           int64  `json:"id"`
	Distributor                  string `json:"distributor"`
	EffectiveFrom                string `json:"effectiveFrom"`
	EffectiveTo                  string `json:"effectiveTo"`
	ConsumptionTEMicrosPerKWh    int64  `json:"consumptionTEMicrosPerKWh"`
	ConsumptionTUSDMicrosPerKWh  int64  `json:"consumptionTUSDMicrosPerKWh"`
	CompensationTEMicrosPerKWh   int64  `json:"compensationTEMicrosPerKWh"`
	CompensationTUSDMicrosPerKWh int64  `json:"compensationTUSDMicrosPerKWh"`
	FlagMicrosPerKWh             int64  `json:"flagMicrosPerKWh"`
	AvailabilityKWh              int    `json:"availabilityKWh"`
	CIPMinor                     int64  `json:"cipMinor"`
	SourceURL                    string `json:"sourceUrl"`
	RetrievedAt                  string `json:"retrievedAt"`
	ApprovedAt                   string `json:"approvedAt"`
}

func (a *API) financeSummary(w http.ResponseWriter, r *http.Request) {
	store, ok := a.financeStore(w)
	if !ok {
		return
	}
	projection, exists, err := store.LatestProjection(r.Context(), a.dependencies.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "finance summary could not be loaded")
		return
	}
	cycles, err := store.ListCycles(r.Context(), 12)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "finance cycles could not be loaded")
		return
	}
	balance, expiry, err := store.CreditSummary(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "credit summary could not be loaded")
		return
	}
	response := financeSummaryResponse{Cycles: cycleResponses(cycles), CreditBalanceKWh: balance}
	if expiry != nil {
		value := rfc3339(*expiry)
		response.NextCreditExpiry = &value
	}
	if exists {
		item := projectionResponse(projection)
		response.LatestProjection = &item
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *API) financeCycles(w http.ResponseWriter, r *http.Request) {
	store, ok := a.financeStore(w)
	if !ok {
		return
	}
	cycles, err := store.ListCycles(r.Context(), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "finance cycles could not be loaded")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cycles": cycleResponses(cycles)})
}

func (a *API) createFinanceCycle(w http.ResponseWriter, r *http.Request) {
	store, ok := a.financeStore(w)
	if !ok {
		return
	}
	var body billingCycleDTO
	if err := decodeBillingCycle(w, r, &body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body exceeds 64 KiB")
			return
		}
		writeError(w, http.StatusUnprocessableEntity, "invalid_finance_cycle", "billing cycle must contain only valid fields")
		return
	}
	location := time.UTC
	var err error
	if a.dependencies.BillingLocation != nil {
		location, err = a.dependencies.BillingLocation(r.Context())
		if err != nil || location == nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "billing calendar is unavailable")
			return
		}
	}
	cycle, err := body.domain(location)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_finance_cycle", err.Error())
		return
	}
	principal, ok := auth.PrincipalFromRequest(r)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error", "finance actor is unavailable")
		return
	}
	saved, projection, err := store.SaveCycle(r.Context(), cycle, principal.UserID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_finance_cycle", "billing cycle could not be saved")
		return
	}
	// The component breakdown remains an estimate even when the entered bill
	// total is authoritative: Helio has no meter-level import/export data.
	projection.IsEstimate = true
	writeJSON(w, http.StatusCreated, map[string]any{"cycle": cycleResponse(saved), "projection": projectionResponse(projection)})
}

func (a *API) tariffProposals(w http.ResponseWriter, r *http.Request) {
	store, ok := a.financeStore(w)
	if !ok {
		return
	}
	proposals, err := store.ListTariffProposals(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "tariff proposals could not be loaded")
		return
	}
	approved, _, err := store.LatestTariff(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "approved tariff could not be loaded")
		return
	}
	items := make([]tariffProposalResponse, 0, len(proposals))
	for _, proposal := range proposals {
		item := proposalResponse(proposal)
		item.DisplayRates = tariffRateRows(proposal, approved)
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposals": items})
}

// createSettingsTariffProposal turns the legacy per-kWh setting into a
// reviewable local proposal. It is intentionally not approved automatically.
func (a *API) createSettingsTariffProposal(w http.ResponseWriter, r *http.Request) {
	store, ok := a.financeStore(w)
	if !ok {
		return
	}
	settings, err := a.dependencies.Store.GetSettings(r.Context(), a.dependencies.AllowPublicLogger)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "configured tariff is unavailable")
		return
	}
	if settings.TariffMinorPerKWh <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_tariff_proposal", "configure a tariff greater than zero before using it")
		return
	}
	rate := settings.TariffMinorPerKWh * 10_000
	proposals, err := store.ListTariffProposals(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "tariff proposals could not be loaded")
		return
	}
	for _, proposal := range proposals {
		if proposal.SourceURL == "/settings" && proposal.ApprovedAt.IsZero() && proposal.ConsumptionTEMicrosPerKWh == rate && proposal.CompensationTEMicrosPerKWh == rate {
			writeJSON(w, http.StatusOK, proposalResponse(proposal))
			return
		}
	}
	now := a.dependencies.Now().UTC()
	proposal, err := store.CreateProposal(r.Context(), domain.TariffProposal{
		Distributor: "Tarifa configurada localmente", EffectiveFrom: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), EffectiveTo: time.Date(2100, 12, 31, 23, 59, 59, 0, time.UTC),
		ConsumptionTEMicrosPerKWh: rate, CompensationTEMicrosPerKWh: rate, AvailabilityKWh: 30,
		SourceURL: "/settings", ParserVersion: "manual-settings-v1", RetrievedAt: now,
	})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_tariff_proposal", "configured tariff could not be proposed")
		return
	}
	writeJSON(w, http.StatusCreated, proposalResponse(proposal))
}

func (a *API) approveTariffProposal(w http.ResponseWriter, r *http.Request) {
	store, ok := a.financeStore(w)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_tariff_proposal", "tariff proposal id must be a positive integer")
		return
	}
	principal, ok := auth.PrincipalFromRequest(r)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error", "finance actor is unavailable")
		return
	}
	tariff, err := store.ApproveProposal(r.Context(), id, principal.UserID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_tariff_proposal", "tariff proposal could not be approved")
		return
	}
	writeJSON(w, http.StatusCreated, tariffResponse(tariff))
}

func (a *API) financeStore(w http.ResponseWriter) (FinanceStore, bool) {
	if a.dependencies.Finance == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "finance is unavailable")
		return nil, false
	}
	return a.dependencies.Finance, true
}

func cycleResponses(cycles []domain.BillingCycle) []billingCycleResponse {
	items := make([]billingCycleResponse, 0, len(cycles))
	for _, cycle := range cycles {
		items = append(items, cycleResponse(cycle))
	}
	return items
}

func cycleResponse(c domain.BillingCycle) billingCycleResponse {
	return billingCycleResponse{ID: c.ID, ReadingStart: rfc3339(c.ReadingStart), ReadingEnd: rfc3339(c.ReadingEnd), ActiveConsumptionKWh: c.ActiveConsumptionKWh, InjectedKWh: c.InjectedKWh, CreditsUsedKWh: c.CreditsUsedKWh, CreditBalanceKWh: c.CreditBalanceKWh, TotalPaidMinor: c.TotalPaidMinor, FlagChargeMinor: c.FlagChargeMinor, TariffVersionID: c.TariffVersionID}
}
func projectionResponse(p domain.FinancialProjection) financialProjectionResponse {
	return financialProjectionResponse{ID: p.ID, BillingCycleID: p.BillingCycleID, TariffVersionID: p.TariffVersionID, ConsumptionMinor: p.ConsumptionMinor, CompensationMinor: p.CompensationMinor, FlagMinor: p.FlagMinor, FlagChargeMinor: p.FlagChargeMinor, TaxesMinor: p.TaxesMinor, CIPMinor: p.CIPMinor, TotalMinor: p.TotalMinor, WithoutSolarCompensationMinor: p.WithoutSolarCompensationMinor, IsEstimate: p.IsEstimate, CalculatedAt: rfc3339(p.CalculatedAt), DisplayTotal: moneyBRL(p.TotalMinor), DisplayWithoutSolar: moneyBRL(p.WithoutSolarCompensationMinor), DisplayRows: []displayRow{{"Consumo", moneyBRL(p.ConsumptionMinor)}, {"Compensação", moneyBRL(p.CompensationMinor)}, {"Bandeira tarifária", moneyBRL(p.FlagMinor)}, {"Ajuste manual de bandeira", moneyBRL(p.FlagChargeMinor)}, {"Tributos", moneyBRL(p.TaxesMinor)}, {"CIP", moneyBRL(p.CIPMinor)}}}
}
func moneyBRL(value int64) string { return fmt.Sprintf("R$ %d,%02d", value/100, value%100) }
func tariffRateRows(p domain.TariffProposal, a domain.TariffVersion) []tariffRateRow {
	rows := []struct {
		label              string
		proposal, approved int64
	}{{"TE consumo", p.ConsumptionTEMicrosPerKWh, a.ConsumptionTEMicrosPerKWh}, {"TUSD consumo", p.ConsumptionTUSDMicrosPerKWh, a.ConsumptionTUSDMicrosPerKWh}, {"TE compensação", p.CompensationTEMicrosPerKWh, a.CompensationTEMicrosPerKWh}, {"TUSD compensação", p.CompensationTUSDMicrosPerKWh, a.CompensationTUSDMicrosPerKWh}, {"Bandeira", p.FlagMicrosPerKWh, a.FlagMicrosPerKWh}, {"CIP", p.CIPMinor, a.CIPMinor}}
	out := make([]tariffRateRow, 0, len(rows))
	for _, r := range rows {
		unit := "µR$/kWh"
		if r.label == "CIP" {
			unit = "centavos"
		}
		out = append(out, tariffRateRow{r.label, fmt.Sprintf("%d %s", r.approved, unit), fmt.Sprintf("%d %s", r.proposal, unit), fmt.Sprintf("%+d %s", r.proposal-r.approved, unit)})
	}
	return out
}
func proposalResponse(p domain.TariffProposal) tariffProposalResponse {
	response := tariffProposalResponse{ID: p.ID, Distributor: p.Distributor, EffectiveFrom: rfc3339(p.EffectiveFrom), EffectiveTo: rfc3339(p.EffectiveTo), ConsumptionTEMicrosPerKWh: p.ConsumptionTEMicrosPerKWh, ConsumptionTUSDMicrosPerKWh: p.ConsumptionTUSDMicrosPerKWh, CompensationTEMicrosPerKWh: p.CompensationTEMicrosPerKWh, CompensationTUSDMicrosPerKWh: p.CompensationTUSDMicrosPerKWh, FlagMicrosPerKWh: p.FlagMicrosPerKWh, AvailabilityKWh: p.AvailabilityKWh, CIPMinor: p.CIPMinor, SourceURL: p.SourceURL, ParserVersion: p.ParserVersion, RetrievedAt: rfc3339(p.RetrievedAt)}
	if !p.ApprovedAt.IsZero() {
		value := rfc3339(p.ApprovedAt)
		response.ApprovedAt = &value
	}
	return response
}
func tariffResponse(t domain.TariffVersion) tariffVersionResponse {
	return tariffVersionResponse{ID: t.ID, Distributor: t.Distributor, EffectiveFrom: rfc3339(t.EffectiveFrom), EffectiveTo: rfc3339(t.EffectiveTo), ConsumptionTEMicrosPerKWh: t.ConsumptionTEMicrosPerKWh, ConsumptionTUSDMicrosPerKWh: t.ConsumptionTUSDMicrosPerKWh, CompensationTEMicrosPerKWh: t.CompensationTEMicrosPerKWh, CompensationTUSDMicrosPerKWh: t.CompensationTUSDMicrosPerKWh, FlagMicrosPerKWh: t.FlagMicrosPerKWh, AvailabilityKWh: t.AvailabilityKWh, CIPMinor: t.CIPMinor, SourceURL: t.SourceURL, RetrievedAt: rfc3339(t.RetrievedAt), ApprovedAt: rfc3339(t.ApprovedAt)}
}
func rfc3339(value time.Time) string { return value.UTC().Format(time.RFC3339) }
