package api

import (
	"math"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

func TestGeneratedValueMinorUsesExactCheckedRounding(t *testing.T) {
	for _, test := range []struct {
		name   string
		wh     float64
		tariff int64
		want   int64
	}{
		{"below half", 4, 100, 0},
		{"exact half rounds up", 5, 100, 1},
		{"ordinary decimal", 12_340, 95, 1_172},
		{"maximum configured tariff", 288_000, 1_000_000_000, 288_000_000_000},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := generatedValueMinor(test.wh, test.tariff)
			if err != nil || got != test.want {
				t.Fatalf("minor=%d err=%v want=%d", got, err, test.want)
			}
		})
	}
	for _, invalid := range []struct {
		wh     float64
		tariff int64
	}{{-1, 95}, {1e20, 1_000_000_000}, {1, -1}} {
		if _, err := generatedValueMinor(invalid.wh, invalid.tariff); err == nil {
			t.Fatalf("expected checked arithmetic error for %#v", invalid)
		}
	}
}

func TestSummarizeTrendReportsCurrentPriorDeltaAndCoverage(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	points := []domain.AggregatePoint{
		{At: start, PeakPowerW: 100, CoveragePct: 100}, {At: start.AddDate(0, 0, 1), PeakPowerW: 100, CoveragePct: 80},
		{At: start.AddDate(0, 0, 2), PeakPowerW: 120, CoveragePct: 100}, {At: start.AddDate(0, 0, 3), PeakPowerW: 140, CoveragePct: 100},
	}
	trend := summarizeTrend(points, start, start.AddDate(0, 0, 4), time.UTC, func(point domain.AggregatePoint) float64 { return point.PeakPowerW })
	if trend.Direction != "up" || trend.Previous != 100 || trend.Current != 130 || trend.Delta != 30 || trend.DeltaPct != 30 || trend.CoveragePct != 95 || trend.WindowDays != 4 {
		t.Fatalf("trend=%#v", trend)
	}
}

func TestSummarizeTrendCountsMissingCalendarDaysAsZeroCoverage(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	points := []domain.AggregatePoint{
		{At: start, PeakPowerW: 100, CoveragePct: 100},
		{At: start.AddDate(0, 0, 2), PeakPowerW: 100, CoveragePct: 100},
		{At: start.AddDate(0, 0, 4), PeakPowerW: 130, CoveragePct: 100},
		{At: start.AddDate(0, 0, 6), PeakPowerW: 130, CoveragePct: 100},
	}
	trend := summarizeTrend(points, start, start.AddDate(0, 0, 7), time.UTC, func(point domain.AggregatePoint) float64 { return point.PeakPowerW })
	if trend.WindowDays != 7 || math.Abs(trend.CoveragePct-(400.0/7)) > 1e-9 || trend.Previous != 100 || trend.Current != 130 || trend.Direction != "up" {
		t.Fatalf("calendar trend=%#v", trend)
	}
}

func TestInsightEvidenceUsesStableLocalizedWhitelist(t *testing.T) {
	got := insightEvidence([]domain.Evidence{
		{Code: "history_days", Label: "Qualifying history days", Value: 12, Unit: "days"},
		{Code: "future_private_model", Label: "SECRET INTERNAL MODEL", Value: 1, Unit: "internal"},
	})
	if len(got) != 1 || got[0].Code != "history_days" || got[0].Label != "Dias qualificáveis no histórico" || got[0].Unit != "dias" {
		t.Fatalf("localized evidence=%#v", got)
	}
}
