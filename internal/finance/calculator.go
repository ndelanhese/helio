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

	billed := max(c.ActiveConsumptionKWh, int64(t.AvailabilityKWh))
	consumption := microsToMinor(billed, t.ConsumptionTEMicrosPerKWh+t.ConsumptionTUSDMicrosPerKWh)
	compensation := microsToMinor(c.CreditsUsedKWh, t.CompensationTEMicrosPerKWh+t.CompensationTUSDMicrosPerKWh)
	flag := microsToMinor(billed, t.FlagMicrosPerKWh)

	return domain.FinancialProjection{
		ConsumptionMinor:              consumption,
		CompensationMinor:             compensation,
		FlagMinor:                     flag,
		FlagChargeMinor:               c.FlagChargeMinor,
		CIPMinor:                      t.CIPMinor,
		TotalMinor:                    consumption - compensation + flag + c.FlagChargeMinor + t.CIPMinor,
		WithoutSolarCompensationMinor: consumption + flag + c.FlagChargeMinor + t.CIPMinor,
		IsEstimate:                    true,
	}, nil
}
