// Package finance calculates deterministic billing projections from approved
// tariff versions and recorded billing cycles.
package finance

import "github.com/ndelanhese/helio/internal/domain"

func microsToMinor(kWh, micros int64) int64 {
	return kWh * micros / 10_000
}

// Calculate creates an estimate using only the supplied tariff and billing
// cycle. All arithmetic stays in integer micro-reais and centavos.
func Calculate(t domain.TariffVersion, c domain.BillingCycle) (domain.FinancialProjection, error) {
	if err := domain.ValidateTariffVersion(t); err != nil {
		return domain.FinancialProjection{}, err
	}
	if err := domain.ValidateBillingCycle(c); err != nil {
		return domain.FinancialProjection{}, err
	}

	consumption := microsToMinor(c.ActiveConsumptionKWh, t.ConsumptionTEMicrosPerKWh+t.ConsumptionTUSDMicrosPerKWh)
	availability := microsToMinor(int64(t.AvailabilityKWh), t.ConsumptionTEMicrosPerKWh+t.ConsumptionTUSDMicrosPerKWh)
	billedConsumption := max(consumption, availability)
	compensation := min(microsToMinor(c.CreditsUsedKWh, t.CompensationTEMicrosPerKWh+t.CompensationTUSDMicrosPerKWh), max(billedConsumption-availability, 0))
	energyTotal := billedConsumption - compensation
	flag := microsToMinor(max(c.ActiveConsumptionKWh, int64(t.AvailabilityKWh)), t.FlagMicrosPerKWh)

	return domain.FinancialProjection{
		ConsumptionMinor:              billedConsumption,
		CompensationMinor:             compensation,
		FlagMinor:                     flag,
		FlagChargeMinor:               c.FlagChargeMinor,
		CIPMinor:                      t.CIPMinor,
		TotalMinor:                    energyTotal + flag + c.FlagChargeMinor + t.CIPMinor,
		WithoutSolarCompensationMinor: billedConsumption + flag + c.FlagChargeMinor + t.CIPMinor,
		IsEstimate:                    true,
	}, nil
}
