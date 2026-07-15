package domain

import "time"

type Confidence string

const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

func (c Confidence) Valid() bool {
	return c == ConfidenceLow || c == ConfidenceMedium || c == ConfidenceHigh
}

type Evidence struct {
	Code  string  `json:"code"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

type AnalysisResult struct {
	ExpectedWh float64    `json:"expectedWh"`
	ActualWh   float64    `json:"actualWh"`
	Ratio      float64    `json:"ratio"`
	Confidence Confidence `json:"confidence"`
	Evidence   []Evidence `json:"evidence"`
	Qualifying bool       `json:"qualifying"`
}

type DailyAnalysis struct {
	Day        string     `json:"day"`
	ExpectedWh float64    `json:"expectedWh"`
	ActualWh   float64    `json:"actualWh"`
	Ratio      float64    `json:"ratio"`
	Confidence Confidence `json:"confidence"`
	Evidence   []Evidence `json:"evidence"`
	Qualifying bool       `json:"qualifying"`
	AnalyzedAt time.Time  `json:"analyzedAt"`
}
