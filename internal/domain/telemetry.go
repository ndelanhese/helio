package domain

import "time"

type MPPT struct {
	Active   bool    `json:"active"`
	VoltageV float64 `json:"voltageV"`
	CurrentA float64 `json:"currentA"`
	PowerW   float64 `json:"powerW"`
}

type Grid struct {
	VoltageV    float64 `json:"voltageV"`
	FrequencyHz float64 `json:"frequencyHz"`
}

type TelemetrySnapshot struct {
	ObservedAt       time.Time `json:"observedAt"`
	Status           string    `json:"status"`
	ACPowerW         float64   `json:"acPowerW"`
	EnergyTodayWh    float64   `json:"energyTodayWh"`
	EnergyLifetimeWh float64   `json:"energyLifetimeWh"`
	PV1              MPPT      `json:"pv1"`
	PV2              MPPT      `json:"pv2"`
	Grid             Grid      `json:"grid"`
	FaultCodes       []uint16  `json:"faultCodes"`
}
