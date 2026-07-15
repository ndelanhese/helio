package analysis

import (
	"testing"
	"time"
)

func TestBaselineQualifiesCoverageAndGroupsLocalMonthHour(t *testing.T) {
	loc := time.FixedZone("local", -3*60*60)
	days := []TrainingDay{
		{Timezone: "America/Sao_Paulo", Day: time.Date(2026, 1, 2, 0, 0, 0, 0, loc), CoveragePct: 95, InstalledWatts: 4000, Hours: []PowerHour{{At: time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC), PowerW: 2000}}},
		{Timezone: "America/Sao_Paulo", Day: time.Date(2026, 1, 3, 0, 0, 0, 0, loc), CoveragePct: 79.9, InstalledWatts: 4000, Hours: []PowerHour{{At: time.Date(2026, 1, 3, 15, 0, 0, 0, time.UTC), PowerW: 0}}},
		{Timezone: "America/Sao_Paulo", Day: time.Date(2026, 1, 4, 0, 0, 0, 0, loc), CoveragePct: 100, InstalledWatts: 0, Hours: []PowerHour{{At: time.Date(2026, 1, 4, 15, 0, 0, 0, time.UTC), PowerW: 0}}},
	}
	got := mustBuildBaseline(t, days)
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
		days = append(days, TrainingDay{Timezone: "UTC", Day: at, CoveragePct: 100, InstalledWatts: 1000, Hours: []PowerHour{{At: at, PowerW: power}}})
	}
	bucket := mustBuildBaseline(t, days).Buckets[Bucket{Month: time.February, Hour: 10}]
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
		days = append(days, TrainingDay{Timezone: "UTC", Day: at, CoveragePct: 100, InstalledWatts: 1000, Hours: []PowerHour{{At: at, PowerW: power}}})
	}
	bucket := mustBuildBaseline(t, days).Buckets[Bucket{Month: time.February, Hour: 11}]
	if bucket.SampleCount != 3 || bucket.NormalizedPower != .5 {
		t.Fatalf("zero-MAD clipping bucket = %+v", bucket)
	}
}

func TestBaselineMissingHoursAreNotZeroSamples(t *testing.T) {
	at := time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC)
	baseline := mustBuildBaseline(t, []TrainingDay{
		{Timezone: "UTC", Day: at, CoveragePct: 100, InstalledWatts: 1000, Hours: []PowerHour{{At: at, PowerW: 600}}},
		{Timezone: "UTC", Day: at.AddDate(0, 0, 1), CoveragePct: 100, InstalledWatts: 1000},
	})
	bucket := baseline.Buckets[Bucket{Month: time.March, Hour: 11}]
	if bucket.SampleCount != 1 || bucket.NormalizedPower != .6 {
		t.Fatalf("missing hour altered bucket: %+v", bucket)
	}
}

func TestBaselineUsesConfiguredLocationAcrossMonthAndDSTBoundaries(t *testing.T) {
	days := []TrainingDay{
		{Timezone: "America/Sao_Paulo", Day: time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC), CoveragePct: 100, InstalledWatts: 1000, Hours: []PowerHour{{At: time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), PowerW: 500}}},
		{Timezone: "America/Sao_Paulo", Day: time.Date(2018, 11, 4, 0, 0, 0, 0, time.UTC), CoveragePct: 100, InstalledWatts: 1000, Hours: []PowerHour{{At: time.Date(2018, 11, 4, 12, 0, 0, 0, time.UTC), PowerW: 600}}},
	}
	baseline := mustBuildBaseline(t, days)
	if _, ok := baseline.Buckets[Bucket{Month: time.December, Hour: 22}]; !ok {
		t.Fatalf("UTC month boundary did not map to local December 22:00: %+v", baseline.Buckets)
	}
	if _, ok := baseline.Buckets[Bucket{Month: time.November, Hour: 10}]; !ok {
		t.Fatalf("historical Sao Paulo DST did not map UTC 12:00 to local 10:00: %+v", baseline.Buckets)
	}
}

func TestBaselineRejectsMissingOrInconsistentTimezone(t *testing.T) {
	day := TrainingDay{Day: time.Now(), CoveragePct: 100, InstalledWatts: 1000}
	if _, err := BuildBaseline([]TrainingDay{day}); err == nil {
		t.Fatal("missing timezone accepted")
	}
	day.Timezone = "Local"
	if _, err := BuildBaseline([]TrainingDay{day}); err == nil {
		t.Fatal("host Local timezone accepted")
	}
	day.Timezone = "UTC"
	other := day
	other.Timezone = "America/Sao_Paulo"
	if _, err := BuildBaseline([]TrainingDay{day, other}); err == nil {
		t.Fatal("inconsistent training timezones accepted")
	}
	day.Timezone = "UTC"
	day.Day = time.Time{}
	if _, err := BuildBaseline([]TrainingDay{day}); err == nil {
		t.Fatal("training day without date accepted")
	}
}

func mustBuildBaseline(t *testing.T, days []TrainingDay) Baseline {
	t.Helper()
	baseline, err := BuildBaseline(days)
	if err != nil {
		t.Fatal(err)
	}
	return baseline
}
