package finance_test

import (
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/finance"
)

func TestCalculateAppliesAvailabilityFloorAndSeparatesCIP(t *testing.T) {
	got, err := finance.Calculate(tariff(100, 389503, 538944), cycle(79, 0, 0, 0))
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	if got.ConsumptionMinor != 9284 {
		t.Errorf("ConsumptionMinor = %d, want 9284", got.ConsumptionMinor)
	}
	if got.CIPMinor != 2556 {
		t.Errorf("CIPMinor = %d, want 2556", got.CIPMinor)
	}
	if got.TotalMinor != 11840 {
		t.Errorf("TotalMinor = %d, want 11840", got.TotalMinor)
	}
}

func TestCounterfactualRemovesOnlyCompensation(t *testing.T) {
	got, err := finance.Calculate(tariff(100, 389503, 538944), cycle(322, 243, 243, 1878))
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	if got.TotalMinor+got.CompensationMinor != got.WithoutSolarCompensationMinor {
		t.Errorf("counterfactual = %d, want total (%d) plus compensation (%d)", got.WithoutSolarCompensationMinor, got.TotalMinor, got.CompensationMinor)
	}
}

func TestCalculateRejectsCreditsAboveConsumption(t *testing.T) {
	_, err := finance.Calculate(tariff(100, 389503, 538944), cycle(10, 20, 11, 0))
	if err == nil {
		t.Fatal("Calculate() error = nil, want credit-use validation error")
	}
}

func tariff(availability int, teMicros, tusdMicros int64) domain.TariffVersion {
	return domain.TariffVersion{
		Distributor:                  "CEMIG",
		EffectiveFrom:                time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		EffectiveTo:                  time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC),
		ConsumptionTEMicrosPerKWh:    teMicros,
		ConsumptionTUSDMicrosPerKWh:  tusdMicros,
		CompensationTEMicrosPerKWh:   teMicros,
		CompensationTUSDMicrosPerKWh: tusdMicros,
		AvailabilityKWh:              availability,
		CIPMinor:                     2556,
	}
}

func cycle(consumption, injected, creditsUsed, balance int64) domain.BillingCycle {
	return domain.BillingCycle{
		ReadingStart:         time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		ReadingEnd:           time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC),
		ActiveConsumptionKWh: consumption,
		InjectedKWh:          injected,
		CreditsUsedKWh:       creditsUsed,
		CreditBalanceKWh:     balance,
	}
}
