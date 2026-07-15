package analysis

import (
	"testing"
	"time"
)

func TestBaselineQualifiesCoverageAndGroupsLocalMonthHour(t *testing.T) {
	loc := time.FixedZone("local", -3*60*60)
	days := []TrainingDay{
		{Day: time.Date(2026, 1, 2, 0, 0, 0, 0, loc), CoveragePct: 95, InstalledWatts: 4000, Hours: []PowerHour{{At: time.Date(2026, 1, 2, 12, 0, 0, 0, loc), PowerW: 2000}}},
		{Day: time.Date(2026, 1, 3, 0, 0, 0, 0, loc), CoveragePct: 79.9, InstalledWatts: 4000, Hours: []PowerHour{{At: time.Date(2026, 1, 3, 12, 0, 0, 0, loc), PowerW: 0}}},
		{Day: time.Date(2026, 1, 4, 0, 0, 0, 0, loc), CoveragePct: 100, InstalledWatts: 0, Hours: []PowerHour{{At: time.Date(2026, 1, 4, 12, 0, 0, 0, loc), PowerW: 0}}},
	}
	got := BuildBaseline(days)
	if got.QualifyingDays != 1 {
		t.Fatalf("qualifying days = %d, want 1", got.QualifyingDays)
	}
	bucket, ok := got.Buckets[Bucket{Month: time.January, Hour: 12}]
	if !ok || bucket.SampleCount != 1 || bucket.NormalizedPower != .5 {
		t.Fatalf("bucket = %+v, present %v", bucket, ok)
	}
}

func TestBaselineMADClipsOutlier(t *testing.T) {
	loc := time.UTC
	powers := []float64{400, 420, 440, 5000}
	days := make([]TrainingDay, 0, len(powers))
	for index, power := range powers {
		at := time.Date(2026, 2, index+1, 10, 0, 0, 0, loc)
		days = append(days, TrainingDay{Day: at, CoveragePct: 100, InstalledWatts: 1000, Hours: []PowerHour{{At: at, PowerW: power}}})
	}
	bucket := BuildBaseline(days).Buckets[Bucket{Month: time.February, Hour: 10}]
	if bucket.SampleCount != 3 {
		t.Fatalf("sample count = %d, want 3 after clipping", bucket.SampleCount)
	}
	if bucket.NormalizedPower != .42 {
		t.Fatalf("median normalized power = %v, want .42", bucket.NormalizedPower)
	}
}

func TestBaselineMADClipsOutlierWhenMedianDeviationIsZero(t *testing.T) {
	powers := []float64{500, 500, 500, 1000}
	days := make([]TrainingDay, 0, len(powers))
	for index, power := range powers {
		at := time.Date(2026, 2, index+1, 11, 0, 0, 0, time.UTC)
		days = append(days, TrainingDay{Day: at, CoveragePct: 100, InstalledWatts: 1000, Hours: []PowerHour{{At: at, PowerW: power}}})
	}
	bucket := BuildBaseline(days).Buckets[Bucket{Month: time.February, Hour: 11}]
	if bucket.SampleCount != 3 || bucket.NormalizedPower != .5 {
		t.Fatalf("zero-MAD clipping bucket = %+v", bucket)
	}
}

func TestBaselineMissingHoursAreNotZeroSamples(t *testing.T) {
	at := time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC)
	baseline := BuildBaseline([]TrainingDay{
		{Day: at, CoveragePct: 100, InstalledWatts: 1000, Hours: []PowerHour{{At: at, PowerW: 600}}},
		{Day: at.AddDate(0, 0, 1), CoveragePct: 100, InstalledWatts: 1000},
	})
	bucket := baseline.Buckets[Bucket{Month: time.March, Hour: 11}]
	if bucket.SampleCount != 1 || bucket.NormalizedPower != .6 {
		t.Fatalf("missing hour altered bucket: %+v", bucket)
	}
}
