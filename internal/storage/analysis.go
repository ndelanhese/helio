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
	_, err = r.db.sql.ExecContext(ctx, `INSERT INTO daily_analysis(
		day,expected_wh,actual_wh,ratio,confidence,evidence_json,qualifying,updated_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(day) DO UPDATE SET expected_wh=excluded.expected_wh,actual_wh=excluded.actual_wh,
		ratio=excluded.ratio,confidence=excluded.confidence,evidence_json=excluded.evidence_json,
		qualifying=excluded.qualifying,updated_at=excluded.updated_at
		WHERE excluded.updated_at >= daily_analysis.updated_at`,
		analysis.Day, analysis.ExpectedWh, analysis.ActualWh, analysis.Ratio, analysis.Confidence, string(evidence), analysis.Qualifying, formatTime(analysis.AnalyzedAt.UTC()))
	if err != nil {
		return fmt.Errorf("upsert daily analysis: %w", err)
	}
	return nil
}

func (r *AnalysisRepository) Load(ctx context.Context, day string) (domain.DailyAnalysis, bool, error) {
	var got domain.DailyAnalysis
	var evidenceJSON, analyzedAt string
	err := r.db.sql.QueryRowContext(ctx, `SELECT day,expected_wh,actual_wh,ratio,confidence,
		evidence_json,qualifying,updated_at FROM daily_analysis WHERE day=?`, day).Scan(&got.Day, &got.ExpectedWh, &got.ActualWh, &got.Ratio,
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
