package domain

import "time"

type HistoryPoint struct {
	At     time.Time `json:"at"`
	PowerW float64   `json:"powerW"`
}

type HourlySummary struct {
	Hour        string  `json:"hour"`
	EnergyWh    float64 `json:"energyWh"`
	PeakPowerW  float64 `json:"peakPowerW"`
	CoveragePct float64 `json:"coveragePct"`
}

type DailySummary struct {
	Day               string  `json:"day"`
	EnergyWh          float64 `json:"energyWh"`
	PeakPowerW        float64 `json:"peakPowerW"`
	CoveragePct       float64 `json:"coveragePct"`
	ProductiveMinutes int     `json:"productiveMinutes"`
}

type MonthlySummary struct {
	Month             string  `json:"month"`
	EnergyWh          float64 `json:"energyWh"`
	PeakPowerW        float64 `json:"peakPowerW"`
	CoveragePct       float64 `json:"coveragePct"`
	ProductiveMinutes int     `json:"productiveMinutes"`
}
