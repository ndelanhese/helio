package domain_test

import (
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

func TestValidateTariffVersion(t *testing.T) {
	valid := domain.TariffVersion{
		Distributor:                 "COPEL",
		EffectiveFrom:               date("2026-06-24"),
		EffectiveTo:                 date("2027-06-23"),
		ConsumptionTEMicrosPerKWh:   389503,
		ConsumptionTUSDMicrosPerKWh: 538944,
		AvailabilityKWh:             100,
	}
	if err := domain.ValidateTariffVersion(valid); err != nil {
		t.Fatalf("ValidateTariffVersion(valid) error = %v", err)
	}

	valid.AvailabilityKWh = 99
	if err := domain.ValidateTariffVersion(valid); err == nil {
		t.Fatal("ValidateTariffVersion accepted an invalid availability floor")
	}
}

func TestValidateBillingCycle(t *testing.T) {
	valid := domain.BillingCycle{
		ReadingStart:         date("2026-06-24"),
		ReadingEnd:           date("2026-07-23"),
		ActiveConsumptionKWh: 322,
		InjectedKWh:          243,
		CreditsUsedKWh:       243,
		CreditBalanceKWh:     1878,
		TotalPaidMinor:       11840,
	}
	if err := domain.ValidateBillingCycle(valid); err != nil {
		t.Fatalf("ValidateBillingCycle(valid) error = %v", err)
	}

	valid.ReadingEnd = valid.ReadingStart
	if err := domain.ValidateBillingCycle(valid); err == nil {
		t.Fatal("ValidateBillingCycle accepted a zero-length cycle")
	}
}

func date(value string) time.Time {
	parsed, err := time.Parse(time.DateOnly, value)
	if err != nil {
		panic(err)
	}
	return parsed
}
