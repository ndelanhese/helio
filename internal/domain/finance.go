package domain

import (
	"fmt"
	"time"
)

// TariffVersion is an approved, immutable tariff schedule. Monetary rates use
// micro-reais per kWh and amounts use centavos; neither uses floating point.
type TariffVersion struct {
	ID                                                       int64
	Distributor                                              string
	EffectiveFrom, EffectiveTo                               time.Time
	ConsumptionTEMicrosPerKWh, ConsumptionTUSDMicrosPerKWh   int64
	CompensationTEMicrosPerKWh, CompensationTUSDMicrosPerKWh int64
	FlagMicrosPerKWh                                         int64
	AvailabilityKWh                                          int
	CIPMinor                                                 int64
	SourceURL                                                string
	RetrievedAt, ApprovedAt                                  time.Time
}

// TariffProposal is a discovered or manually entered tariff candidate. It is
// mutable only until ApprovedAt is set.
type TariffProposal struct {
	ID                                                       int64
	Distributor                                              string
	EffectiveFrom, EffectiveTo                               time.Time
	ConsumptionTEMicrosPerKWh, ConsumptionTUSDMicrosPerKWh   int64
	CompensationTEMicrosPerKWh, CompensationTUSDMicrosPerKWh int64
	FlagMicrosPerKWh                                         int64
	AvailabilityKWh                                          int
	CIPMinor                                                 int64
	SourceURL, ParserVersion                                 string
	RetrievedAt, ApprovedAt                                  time.Time
}

// BillingCycle records the authoritative period and amounts from one bill.
type BillingCycle struct {
	ID                                                                  int64
	ReadingStart, ReadingEnd                                            time.Time
	ActiveConsumptionKWh, InjectedKWh, CreditsUsedKWh, CreditBalanceKWh int64
	TotalPaidMinor, FlagChargeMinor, TariffVersionID                    int64
}

// CreditLot tracks credits by origin and expiry without inventing a complete
// schedule when only an aggregate balance is known.
type CreditLot struct {
	ID, OriginCycleID int64
	AvailableKWh      int64
	ExpiresAt         time.Time
	IsPartial         bool
}

// FinancialProjection contains the integer-centavo breakdown of a bill
// estimate or reconciliation.
type FinancialProjection struct {
	ID, BillingCycleID, TariffVersionID                                         int64
	ConsumptionMinor, CompensationMinor, FlagMinor, FlagChargeMinor, TaxesMinor int64
	CIPMinor, TotalMinor, WithoutSolarCompensationMinor                         int64
	IsEstimate                                                                  bool
	CalculatedAt                                                                time.Time
}

func ValidateTariffVersion(t TariffVersion) error {
	if t.Distributor == "" {
		return fmt.Errorf("distributor is required")
	}
	if t.EffectiveFrom.IsZero() || t.EffectiveTo.IsZero() {
		return fmt.Errorf("effective dates are required")
	}
	if t.EffectiveTo.Before(t.EffectiveFrom) {
		return fmt.Errorf("effective end must not precede start")
	}
	if t.ConsumptionTEMicrosPerKWh < 0 || t.ConsumptionTUSDMicrosPerKWh < 0 ||
		t.CompensationTEMicrosPerKWh < 0 || t.CompensationTUSDMicrosPerKWh < 0 ||
		t.FlagMicrosPerKWh < 0 || t.CIPMinor < 0 {
		return fmt.Errorf("tariff amounts must be nonnegative")
	}
	if t.AvailabilityKWh != 30 && t.AvailabilityKWh != 50 && t.AvailabilityKWh != 100 {
		return fmt.Errorf("availability must be 30, 50, or 100 kWh")
	}
	return nil
}

func ValidateBillingCycle(c BillingCycle) error {
	if c.ReadingStart.IsZero() || c.ReadingEnd.IsZero() {
		return fmt.Errorf("reading dates are required")
	}
	if !c.ReadingEnd.After(c.ReadingStart) {
		return fmt.Errorf("reading end must follow reading start")
	}
	if c.ActiveConsumptionKWh < 0 || c.InjectedKWh < 0 || c.CreditsUsedKWh < 0 ||
		c.CreditBalanceKWh < 0 || c.TotalPaidMinor < 0 || c.FlagChargeMinor < 0 || c.TariffVersionID < 0 {
		return fmt.Errorf("billing cycle amounts must be nonnegative")
	}
	if c.CreditsUsedKWh > c.ActiveConsumptionKWh {
		return fmt.Errorf("credits used cannot exceed active consumption")
	}
	return nil
}
