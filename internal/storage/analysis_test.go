package storage

import (
	"context"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

func TestAnalysisRoundTripAndReplacement(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, t.TempDir()+"/analysis.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewAnalysisRepository(db)
	first := domain.DailyAnalysis{Day: "2026-04-10", ExpectedWh: 4200, ActualWh: 3900, Ratio: 3900.0 / 4200, Confidence: domain.ConfidenceMedium, Qualifying: true, Evidence: []domain.Evidence{{Code: "weather_stale", Label: "Weather data is stale", Value: 1, Unit: "boolean"}}, AnalyzedAt: time.Date(2026, 4, 11, 1, 0, 0, 0, time.UTC)}
	if err := repo.Save(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ExpectedWh = 4000
	second.Confidence = domain.ConfidenceHigh
	second.AnalyzedAt = second.AnalyzedAt.Add(time.Hour)
	if err := repo.Save(ctx, second); err != nil {
		t.Fatal(err)
	}
	got, ok, err := repo.Load(ctx, first.Day)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.ExpectedWh != 4000 || got.Confidence != domain.ConfidenceHigh || len(got.Evidence) != 1 || got.AnalyzedAt != second.AnalyzedAt {
		t.Fatalf("round trip = %+v, ok=%v", got, ok)
	}
}

func TestAnalysisRejectsInvalidPersistedOrInputValues(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, t.TempDir()+"/analysis-invalid.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewAnalysisRepository(db)
	if err := repo.Save(ctx, domain.DailyAnalysis{Day: "bad", Confidence: domain.Confidence("certain"), AnalyzedAt: time.Now()}); err == nil {
		t.Fatal("invalid analysis accepted")
	}
}
