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

const (
	maximumIntegratedGap = 90 * time.Second
	pruneBatchSize       = 10_000
)

// TelemetryRepository stores telemetry using local calendar buckets while all
// raw observations remain normalized to UTC.
type TelemetryRepository struct {
	db       *DB
	location *time.Location
}

type telemetryQueryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func NewTelemetryRepository(db *DB, location *time.Location) *TelemetryRepository {
	if location == nil {
		location = time.UTC
	}
	return &TelemetryRepository{db: db, location: location}
}

func formatTime(at time.Time) string {
	return at.UTC().Format(time.RFC3339Nano)
}

func localHour(at time.Time, loc *time.Location) time.Time {
	local := at.In(loc)
	// Subtract within the instant's actual offset so repeated wall-clock hours at
	// a daylight-saving transition retain distinct RFC3339 bucket keys.
	return local.Add(-time.Duration(local.Minute())*time.Minute -
		time.Duration(local.Second())*time.Second - time.Duration(local.Nanosecond()))
}

func (r *TelemetryRepository) SaveMinute(ctx context.Context, s domain.TelemetrySnapshot) error {
	if s.ObservedAt.IsZero() {
		return errors.New("save telemetry minute: observed time is required")
	}
	faults, err := json.Marshal(s.FaultCodes)
	if err != nil {
		return fmt.Errorf("marshal telemetry fault codes: %w", err)
	}
	observedAt := s.ObservedAt.In(r.location).Truncate(time.Minute).UTC()
	_, err = r.db.sql.ExecContext(ctx, `
		INSERT INTO telemetry_minute(
			observed_at, ac_power_w, energy_today_wh, energy_lifetime_wh,
			pv1_voltage_v, pv1_current_a, pv1_power_w,
			pv2_active, pv2_voltage_v, pv2_current_a, pv2_power_w,
			grid_voltage_v, grid_frequency_hz, status, fault_codes_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(observed_at) DO UPDATE SET
			ac_power_w=excluded.ac_power_w,
			energy_today_wh=excluded.energy_today_wh,
			energy_lifetime_wh=MAX(telemetry_minute.energy_lifetime_wh, excluded.energy_lifetime_wh),
			pv1_voltage_v=excluded.pv1_voltage_v,
			pv1_current_a=excluded.pv1_current_a,
			pv1_power_w=excluded.pv1_power_w,
			pv2_active=excluded.pv2_active,
			pv2_voltage_v=excluded.pv2_voltage_v,
			pv2_current_a=excluded.pv2_current_a,
			pv2_power_w=excluded.pv2_power_w,
			grid_voltage_v=excluded.grid_voltage_v,
			grid_frequency_hz=excluded.grid_frequency_hz,
			status=excluded.status,
			fault_codes_json=excluded.fault_codes_json`,
		formatTime(observedAt), s.ACPowerW, s.EnergyTodayWh, s.EnergyLifetimeWh,
		s.PV1.VoltageV, s.PV1.CurrentA, s.PV1.PowerW,
		boolInt(s.PV2.Active), s.PV2.VoltageV, s.PV2.CurrentA, s.PV2.PowerW,
		s.Grid.VoltageV, s.Grid.FrequencyHz, s.Status, string(faults),
	)
	if err != nil {
		return fmt.Errorf("save telemetry minute: %w", err)
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (r *TelemetryRepository) SaveEvent(ctx context.Context, observedAt time.Time, kind string, payload any) error {
	if observedAt.IsZero() || kind == "" {
		return errors.New("save telemetry event: observed time and kind are required")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal telemetry event: %w", err)
	}
	if _, err := r.db.sql.ExecContext(ctx,
		`INSERT INTO telemetry_events(observed_at, kind, payload_json) VALUES (?, ?, ?)`,
		formatTime(observedAt), kind, string(encoded),
	); err != nil {
		return fmt.Errorf("save telemetry event: %w", err)
	}
	return nil
}

func (r *TelemetryRepository) History(ctx context.Context, from, to time.Time) ([]domain.HistoryPoint, error) {
	return history(ctx, r.db.sql, from, to)
}

func history(ctx context.Context, queryer telemetryQueryer, from, to time.Time) ([]domain.HistoryPoint, error) {
	if !from.Before(to) {
		return nil, errors.New("telemetry history: from must be before to")
	}
	rows, err := queryer.QueryContext(ctx, `
		SELECT observed_at, ac_power_w
		FROM telemetry_minute
		WHERE observed_at >= ? AND observed_at < ?
		ORDER BY observed_at`, formatTime(from), formatTime(to))
	if err != nil {
		return nil, fmt.Errorf("query telemetry history: %w", err)
	}
	defer rows.Close()
	points := make([]domain.HistoryPoint, 0)
	for rows.Next() {
		var raw string
		var point domain.HistoryPoint
		if err := rows.Scan(&raw, &point.PowerW); err != nil {
			return nil, fmt.Errorf("scan telemetry history: %w", err)
		}
		point.At, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return nil, fmt.Errorf("parse telemetry history time: %w", err)
		}
		point.At = point.At.UTC()
		points = append(points, point)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate telemetry history: %w", err)
	}
	return points, nil
}

func (r *TelemetryRepository) AggregateHour(ctx context.Context, from, to time.Time) (domain.HourlySummary, error) {
	if !from.Before(to) {
		return domain.HourlySummary{}, errors.New("aggregate hour: from must be before to")
	}
	tx, err := r.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return domain.HourlySummary{}, fmt.Errorf("begin hourly aggregate: %w", err)
	}
	defer tx.Rollback()
	points, err := history(ctx, tx, from, to)
	if err != nil {
		return domain.HourlySummary{}, err
	}
	summary := domain.HourlySummary{Hour: localHour(from, r.location).Format(time.RFC3339)}
	for i, point := range points {
		summary.PeakPowerW = math.Max(summary.PeakPowerW, point.PowerW)
		if i == 0 {
			continue
		}
		gap := point.At.Sub(points[i-1].At)
		if gap > 0 && gap <= maximumIntegratedGap {
			summary.EnergyWh += (points[i-1].PowerW + point.PowerW) * gap.Hours() / 2
		}
	}
	expectedMinutes := int(math.Ceil(to.Sub(from).Minutes()))
	if expectedMinutes > 0 {
		summary.CoveragePct = math.Min(100, float64(len(points))*100/float64(expectedMinutes))
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO hourly_summary(hour, energy_wh, peak_power_w, coverage_pct)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(hour) DO UPDATE SET energy_wh=excluded.energy_wh,
		peak_power_w=excluded.peak_power_w, coverage_pct=excluded.coverage_pct`,
		summary.Hour, summary.EnergyWh, summary.PeakPowerW, summary.CoveragePct)
	if err != nil {
		return domain.HourlySummary{}, fmt.Errorf("upsert hourly summary: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.HourlySummary{}, fmt.Errorf("commit hourly aggregate: %w", err)
	}
	return summary, nil
}

func (r *TelemetryRepository) AggregateDay(ctx context.Context, from, to time.Time) (domain.DailySummary, error) {
	if !from.Before(to) {
		return domain.DailySummary{}, errors.New("aggregate day: from must be before to")
	}
	local := from.In(r.location)
	summary := domain.DailySummary{Day: local.Format("2006-01-02")}

	tx, err := r.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return domain.DailySummary{}, fmt.Errorf("begin daily aggregate: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT hour, energy_wh, peak_power_w, coverage_pct FROM hourly_summary`)
	if err != nil {
		return domain.DailySummary{}, fmt.Errorf("query hourly summaries: %w", err)
	}
	var coverageWeight, totalWeight float64
	for rows.Next() {
		var key string
		var energy, peak, coverage float64
		if err := rows.Scan(&key, &energy, &peak, &coverage); err != nil {
			rows.Close()
			return domain.DailySummary{}, fmt.Errorf("scan hourly summary: %w", err)
		}
		hour, err := time.Parse(time.RFC3339, key)
		if err != nil {
			rows.Close()
			return domain.DailySummary{}, fmt.Errorf("parse hourly summary key: %w", err)
		}
		if hour.Before(to) && hour.Add(time.Hour).After(from) {
			weight := math.Min(to.Sub(hour).Minutes(), hour.Add(time.Hour).Sub(from).Minutes())
			weight = math.Min(weight, to.Sub(from).Minutes())
			if weight > 0 {
				summary.EnergyWh += energy
				summary.PeakPowerW = math.Max(summary.PeakPowerW, peak)
				coverageWeight += coverage * weight
				totalWeight += weight
			}
		}
	}
	if err := rows.Close(); err != nil {
		return domain.DailySummary{}, fmt.Errorf("close hourly summaries: %w", err)
	}
	if err := rows.Err(); err != nil {
		return domain.DailySummary{}, fmt.Errorf("iterate hourly summaries: %w", err)
	}
	if totalWeight > 0 {
		summary.CoveragePct = coverageWeight / totalWeight
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM telemetry_minute
		WHERE observed_at >= ? AND observed_at < ? AND ac_power_w > 0`,
		formatTime(from), formatTime(to)).Scan(&summary.ProductiveMinutes); err != nil {
		return domain.DailySummary{}, fmt.Errorf("count productive minutes: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO daily_summary(day, energy_wh, peak_power_w, productive_minutes, coverage_pct)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(day) DO UPDATE SET energy_wh=excluded.energy_wh,
		peak_power_w=excluded.peak_power_w, productive_minutes=excluded.productive_minutes,
		coverage_pct=excluded.coverage_pct`,
		summary.Day, summary.EnergyWh, summary.PeakPowerW, summary.ProductiveMinutes, summary.CoveragePct)
	if err != nil {
		return domain.DailySummary{}, fmt.Errorf("upsert daily summary: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.DailySummary{}, fmt.Errorf("commit daily aggregate: %w", err)
	}
	return summary, nil
}

func (r *TelemetryRepository) AggregateMonth(ctx context.Context, inMonth time.Time) (domain.MonthlySummary, error) {
	local := inMonth.In(r.location)
	monthKey := local.Format("2006-01")
	summary := domain.MonthlySummary{Month: monthKey}
	tx, err := r.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return domain.MonthlySummary{}, fmt.Errorf("begin monthly aggregate: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		SELECT day, energy_wh, peak_power_w, productive_minutes, coverage_pct
		FROM daily_summary WHERE day >= ? AND day < ? ORDER BY day`, monthKey+"-01", nextMonth(local).Format("2006-01-02"))
	if err != nil {
		return domain.MonthlySummary{}, fmt.Errorf("query daily summaries: %w", err)
	}
	var weightedCoverage, totalMinutes float64
	for rows.Next() {
		var day string
		var energy, peak, coverage float64
		var productive int
		if err := rows.Scan(&day, &energy, &peak, &productive, &coverage); err != nil {
			rows.Close()
			return domain.MonthlySummary{}, fmt.Errorf("scan daily summary: %w", err)
		}
		start, err := time.ParseInLocation("2006-01-02", day, r.location)
		if err != nil {
			rows.Close()
			return domain.MonthlySummary{}, fmt.Errorf("parse daily summary key: %w", err)
		}
		minutes := start.AddDate(0, 0, 1).Sub(start).Minutes()
		summary.EnergyWh += energy
		summary.PeakPowerW = math.Max(summary.PeakPowerW, peak)
		summary.ProductiveMinutes += productive
		weightedCoverage += coverage * minutes
		totalMinutes += minutes
	}
	if err := rows.Close(); err != nil {
		return domain.MonthlySummary{}, fmt.Errorf("close daily summaries: %w", err)
	}
	if err := rows.Err(); err != nil {
		return domain.MonthlySummary{}, fmt.Errorf("iterate daily summaries: %w", err)
	}
	if totalMinutes > 0 {
		summary.CoveragePct = weightedCoverage / totalMinutes
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO monthly_summary(month, energy_wh, peak_power_w, productive_minutes, coverage_pct)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(month) DO UPDATE SET energy_wh=excluded.energy_wh,
		peak_power_w=excluded.peak_power_w, productive_minutes=excluded.productive_minutes,
		coverage_pct=excluded.coverage_pct`,
		summary.Month, summary.EnergyWh, summary.PeakPowerW, summary.ProductiveMinutes, summary.CoveragePct)
	if err != nil {
		return domain.MonthlySummary{}, fmt.Errorf("upsert monthly summary: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.MonthlySummary{}, fmt.Errorf("commit monthly aggregate: %w", err)
	}
	return summary, nil
}

func nextMonth(at time.Time) time.Time {
	return time.Date(at.Year(), at.Month()+1, 1, 0, 0, 0, 0, at.Location())
}

func (r *TelemetryRepository) PruneBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	var total int64
	for {
		result, err := r.db.sql.ExecContext(ctx, `
			DELETE FROM telemetry_minute WHERE observed_at IN (
				SELECT observed_at FROM telemetry_minute
				WHERE observed_at < ? ORDER BY observed_at LIMIT ?
			)`, formatTime(cutoff), pruneBatchSize)
		if err != nil {
			return total, fmt.Errorf("prune telemetry minutes: %w", err)
		}
		deleted, err := result.RowsAffected()
		if err != nil {
			return total, fmt.Errorf("count pruned telemetry minutes: %w", err)
		}
		total += deleted
		if deleted < pruneBatchSize {
			return total, nil
		}
	}
}
