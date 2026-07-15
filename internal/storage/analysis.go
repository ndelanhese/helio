package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

type AnalysisRepository struct{ db *DB }

func NewAnalysisRepository(db *DB) *AnalysisRepository { return &AnalysisRepository{db: db} }

func (r *AnalysisRepository) Save(ctx context.Context, analysis domain.DailyAnalysis) error {
	if err := validateAnalysis(analysis); err != nil {
		return fmt.Errorf("save daily analysis: %w", err)
	}
	evidence, err := json.Marshal(analysis.Evidence)
	if err != nil {
		return fmt.Errorf("marshal daily analysis evidence: %w", err)
	}
	_, err = r.db.sql.ExecContext(ctx, `INSERT INTO daily_summary(
		day,energy_wh,peak_power_w,productive_minutes,coverage_pct,expected_wh,ratio,confidence_label,analysis_evidence_json,analysis_qualifying,analyzed_at)
		VALUES(?,?,0,0,0,?,?,?,?,?,?)
		ON CONFLICT(day) DO UPDATE SET expected_wh=excluded.expected_wh,ratio=excluded.ratio,
		confidence_label=excluded.confidence_label,analysis_evidence_json=excluded.analysis_evidence_json,
		analysis_qualifying=excluded.analysis_qualifying,analyzed_at=excluded.analyzed_at
		WHERE excluded.analyzed_at >= COALESCE(daily_summary.analyzed_at,'')`,
		analysis.Day, analysis.ActualWh, analysis.ExpectedWh, analysis.Ratio, analysis.Confidence, string(evidence), analysis.Qualifying, formatTime(analysis.AnalyzedAt.UTC()))
	if err != nil {
		return fmt.Errorf("upsert daily analysis: %w", err)
	}
	return nil
}

func (r *AnalysisRepository) Load(ctx context.Context, day string) (domain.DailyAnalysis, bool, error) {
	var got domain.DailyAnalysis
	var evidenceJSON, analyzedAt string
	err := r.db.sql.QueryRowContext(ctx, `SELECT day,expected_wh,energy_wh,ratio,confidence_label,
		analysis_evidence_json,analysis_qualifying,analyzed_at FROM daily_summary
		WHERE day=? AND analyzed_at IS NOT NULL`, day).Scan(&got.Day, &got.ExpectedWh, &got.ActualWh, &got.Ratio,
		&got.Confidence, &evidenceJSON, &got.Qualifying, &analyzedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DailyAnalysis{}, false, nil
	}
	if err != nil {
		return domain.DailyAnalysis{}, false, fmt.Errorf("load daily analysis: %w", err)
	}
	if err := json.Unmarshal([]byte(evidenceJSON), &got.Evidence); err != nil {
		return domain.DailyAnalysis{}, false, fmt.Errorf("decode daily analysis evidence: %w", err)
	}
	got.AnalyzedAt, err = time.Parse(sqliteTimeLayout, analyzedAt)
	if err != nil {
		return domain.DailyAnalysis{}, false, fmt.Errorf("parse daily analysis time: %w", err)
	}
	if err := validateAnalysis(got); err != nil {
		return domain.DailyAnalysis{}, false, fmt.Errorf("validate stored daily analysis: %w", err)
	}
	return got, true, nil
}

func validateAnalysis(analysis domain.DailyAnalysis) error {
	if _, err := time.Parse("2006-01-02", analysis.Day); err != nil {
		return errors.New("day must be YYYY-MM-DD")
	}
	if !analysis.Confidence.Valid() {
		return errors.New("confidence must be low, medium, or high")
	}
	if analysis.AnalyzedAt.IsZero() {
		return errors.New("analyzed time is required")
	}
	for _, value := range []float64{analysis.ExpectedWh, analysis.ActualWh, analysis.Ratio} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return errors.New("analysis values must be finite and nonnegative")
		}
	}
	for _, evidence := range analysis.Evidence {
		if evidence.Code == "" || evidence.Label == "" || evidence.Unit == "" || math.IsNaN(evidence.Value) || math.IsInf(evidence.Value, 0) {
			return errors.New("evidence must be labeled and finite")
		}
	}
	return nil
}
