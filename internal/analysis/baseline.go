// Package analysis learns bounded, explainable solar-production expectations.
package analysis

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

const qualifyingCoveragePct = 80

type Bucket struct {
	Month time.Month
	Hour  int
}

type PowerHour struct {
	At     time.Time
	PowerW float64
}

type TrainingDay struct {
	Timezone       string
	Day            time.Time
	CoveragePct    float64
	InstalledWatts float64
	Hours          []PowerHour
}

type BaselineBucket struct {
	NormalizedPower float64 `json:"normalizedPower"`
	SampleCount     int     `json:"sampleCount"`
}

type Baseline struct {
	QualifyingDays int                       `json:"qualifyingDays"`
	Buckets        map[Bucket]BaselineBucket `json:"buckets"`
}

func BuildBaseline(days []TrainingDay) (Baseline, error) {
	values := make(map[Bucket][]float64)
	baseline := Baseline{Buckets: make(map[Bucket]BaselineBucket)}
	var timezone string
	for _, day := range days {
		location, err := configuredLocation(day.Timezone)
		if err != nil {
			return Baseline{}, fmt.Errorf("build baseline: %w", err)
		}
		if timezone == "" {
			timezone = day.Timezone
		} else if timezone != day.Timezone {
			return Baseline{}, errors.New("build baseline: training days must use one configured timezone")
		}
		if day.Day.IsZero() {
			return Baseline{}, errors.New("build baseline: training day date is required")
		}
		if !validCoverage(day.CoveragePct) || day.CoveragePct < qualifyingCoveragePct || !finite(day.InstalledWatts) || day.InstalledWatts <= 0 {
			continue
		}
		baseline.QualifyingDays++
		for _, hour := range day.Hours {
			if hour.At.IsZero() || !finite(hour.PowerW) || hour.PowerW < 0 {
				continue
			}
			normalized := hour.PowerW / day.InstalledWatts
			if !finite(normalized) {
				continue
			}
			normalized = clamp(normalized, 0, 1)
			local := hour.At.In(location)
			key := Bucket{Month: local.Month(), Hour: local.Hour()}
			values[key] = append(values[key], normalized)
		}
	}
	for key, samples := range values {
		kept := madClip(samples)
		if len(kept) == 0 {
			continue
		}
		baseline.Buckets[key] = BaselineBucket{NormalizedPower: median(kept), SampleCount: len(kept)}
	}
	return baseline, nil
}

func configuredLocation(name string) (*time.Location, error) {
	if name == "" || name == "Local" {
		return nil, errors.New("configured IANA timezone is required; host Local is not allowed")
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("load configured timezone %q: %w", name, err)
	}
	return location, nil
}

func madClip(values []float64) []float64 {
	center := median(values)
	deviations := make([]float64, len(values))
	for i, value := range values {
		deviations[i] = math.Abs(value - center)
	}
	mad := median(deviations)
	limit := 3 * mad
	kept := make([]float64, 0, len(values))
	for _, value := range values {
		if math.Abs(value-center) <= limit {
			kept = append(kept, value)
		}
	}
	return kept
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func validCoverage(value float64) bool { return finite(value) && value >= 0 && value <= 100 }

func clamp(value, low, high float64) float64 {
	if !finite(value) || value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
