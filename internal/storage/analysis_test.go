package storage

import (
	"context"
	"database/sql"
	"path/filepath"
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
	older := second
	older.ExpectedWh = 1
	older.AnalyzedAt = first.AnalyzedAt.Add(-time.Hour)
	if err := repo.Save(ctx, older); err != nil {
		t.Fatal(err)
	}
	equal := second
	if err := repo.Save(ctx, equal); err != nil {
		t.Fatal(err)
	}
	got, ok, err := repo.Load(ctx, first.Day)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.ExpectedWh != 4000 || got.Confidence != domain.ConfidenceHigh || len(got.Evidence) != 1 || got.AnalyzedAt != second.AnalyzedAt {
		t.Fatalf("round trip = %+v, ok=%v", got, ok)
	}
	var historyRows int
	if err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM daily_summary`).Scan(&historyRows); err != nil {
		t.Fatal(err)
	}
	if historyRows != 0 {
		t.Fatalf("analysis fabricated %d daily history rows", historyRows)
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

func TestAnalysisMissingLoadAndMalformedStoredRow(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, t.TempDir()+"/analysis-malformed.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewAnalysisRepository(db)
	if got, ok, err := repo.Load(ctx, "2026-04-01"); err != nil || ok || got.Day != "" {
		t.Fatalf("missing load = %+v, ok=%v, err=%v", got, ok, err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO daily_analysis(day,expected_wh,actual_wh,ratio,confidence,evidence_json,qualifying,updated_at)
		VALUES('2026-04-01',1,1,1,'high','not-json',1,'2026-04-02T00:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := repo.Load(ctx, "2026-04-01"); err == nil || ok {
		t.Fatalf("malformed stored row returned ok=%v err=%v", ok, err)
	}
}

func TestAnalysisMigrationPreservesExistingHistory(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, t.TempDir()+"/analysis-upgrade.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO daily_summary(day,energy_wh,peak_power_w,productive_minutes,coverage_pct)
		VALUES('2026-03-31',1234,900,60,95)`); err != nil {
		t.Fatal(err)
	}
	analysis := domain.DailyAnalysis{Day: "2026-03-31", ExpectedWh: 1300, ActualWh: 1234, Ratio: 1234.0 / 1300, Confidence: domain.ConfidenceMedium, Qualifying: true, AnalyzedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)}
	if err := NewAnalysisRepository(db).Save(ctx, analysis); err != nil {
		t.Fatal(err)
	}
	var energy, peak, coverage float64
	if err := db.sql.QueryRowContext(ctx, `SELECT energy_wh,peak_power_w,coverage_pct FROM daily_summary WHERE day='2026-03-31'`).Scan(&energy, &peak, &coverage); err != nil {
		t.Fatal(err)
	}
	if energy != 1234 || peak != 900 || coverage != 95 {
		t.Fatalf("history mutated: energy=%v peak=%v coverage=%v", energy, peak, coverage)
	}
}

func TestAnalysisMigrationFromV3PreservesExistingHistory(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "analysis-v3.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:3] {
		if _, err := raw.ExecContext(ctx, migration.sql); err != nil {
			t.Fatal(err)
		}
		if _, err := raw.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at) VALUES(?,CURRENT_TIMESTAMP)`, migration.version); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := raw.ExecContext(ctx, `INSERT INTO daily_summary(day,energy_wh,peak_power_w,productive_minutes,coverage_pct)
		VALUES('2026-03-31',1234,900,60,95)`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var energy float64
	if err := db.sql.QueryRowContext(ctx, `SELECT energy_wh FROM daily_summary WHERE day='2026-03-31'`).Scan(&energy); err != nil || energy != 1234 {
		t.Fatalf("history after v3 upgrade: energy=%v err=%v", energy, err)
	}
	if got, ok, err := NewAnalysisRepository(db).Load(ctx, "2026-03-31"); err != nil || ok || got.Day != "" {
		t.Fatalf("analysis unexpectedly fabricated during upgrade: %+v ok=%v err=%v", got, ok, err)
	}
}
