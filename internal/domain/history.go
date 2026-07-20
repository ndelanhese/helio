package domain

import "time"

type HistoryPoint struct {
	At                    time.Time `json:"at"`
	PowerW                float64   `json:"powerW"`
	SampleIntervalMinutes int       `json:"sampleIntervalMinutes,omitempty"`
	// Status is kept internal to aggregation. Cloud history is sampled every
	// five minutes, while the local collector records once a minute.
	Status string `json:"-"`
}

// AggregatePoint is a persisted local-calendar summary with its bucket start
// represented as an absolute UTC instant at the API boundary.
type AggregatePoint struct {
	At                time.Time `json:"at"`
	EnergyWh          float64   `json:"energyWh"`
	PeakPowerW        float64   `json:"peakPowerW"`
	CoveragePct       float64   `json:"coveragePct"`
	ProductiveMinutes int       `json:"productiveMinutes,omitempty"`
}

type HourlySummary struct {
	Hour              string  `json:"hour"`
	EnergyWh          float64 `json:"energyWh"`
	PeakPowerW        float64 `json:"peakPowerW"`
	CoveragePct       float64 `json:"coveragePct"`
	ProductiveMinutes int     `json:"productiveMinutes"`
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
