package storage

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"time"
)

type calendarSummary struct {
	energyWh, peakPowerW, coverageWeight float64
	productiveMinutes                    int
	minutes                              float64
}

// rebuildCalendarSummaries reassigns every permanent hourly bucket to the new
// local calendar. The caller owns the transaction containing settings/audit.
func rebuildCalendarSummaries(ctx context.Context, tx *sql.Tx, location *time.Location) error {
	rows, err := tx.QueryContext(ctx, `SELECT hour,energy_wh,peak_power_w,coverage_pct,productive_minutes FROM hourly_summary ORDER BY unixepoch(hour)`)
	if err != nil {
		return fmt.Errorf("query hourly summaries for calendar rebuild: %w", err)
	}
	days := make(map[string]*calendarSummary)
	for rows.Next() {
		var key string
		var energy, peak, coverage float64
		var productive int
		if err := rows.Scan(&key, &energy, &peak, &coverage, &productive); err != nil {
			rows.Close()
			return fmt.Errorf("scan calendar rebuild hour: %w", err)
		}
		at, err := time.Parse(time.RFC3339, key)
		if err != nil {
			rows.Close()
			return fmt.Errorf("parse calendar rebuild hour: %w", err)
		}
		dayKey := at.In(location).Format("2006-01-02")
		summary := days[dayKey]
		if summary == nil {
			start, err := time.ParseInLocation("2006-01-02", dayKey, location)
			if err != nil {
				rows.Close()
				return fmt.Errorf("parse calendar rebuild day: %w", err)
			}
			summary = &calendarSummary{minutes: start.AddDate(0, 0, 1).Sub(start).Minutes()}
			days[dayKey] = summary
		}
		summary.energyWh += energy
		summary.peakPowerW = math.Max(summary.peakPowerW, peak)
		summary.coverageWeight += coverage * 60
		summary.productiveMinutes += productive
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close calendar rebuild hours: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate calendar rebuild hours: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM daily_summary`); err != nil {
		return fmt.Errorf("clear daily summaries: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM monthly_summary`); err != nil {
		return fmt.Errorf("clear monthly summaries: %w", err)
	}
	dayKeys := sortedCalendarKeys(days)
	months := make(map[string]*calendarSummary)
	for _, key := range dayKeys {
		day := days[key]
		coverage := float64(0)
		if day.minutes > 0 {
			coverage = day.coverageWeight / day.minutes
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO daily_summary(day,energy_wh,peak_power_w,productive_minutes,coverage_pct) VALUES(?,?,?,?,?)`, key, day.energyWh, day.peakPowerW, day.productiveMinutes, coverage); err != nil {
			return fmt.Errorf("insert rebuilt daily summary: %w", err)
		}
		monthKey := key[:7]
		month := months[monthKey]
		if month == nil {
			month = &calendarSummary{}
			months[monthKey] = month
		}
		month.energyWh += day.energyWh
		month.peakPowerW = math.Max(month.peakPowerW, day.peakPowerW)
		month.productiveMinutes += day.productiveMinutes
		month.coverageWeight += coverage * day.minutes
		month.minutes += day.minutes
	}
	for _, key := range sortedCalendarKeys(months) {
		month := months[key]
		coverage := float64(0)
		if month.minutes > 0 {
			coverage = month.coverageWeight / month.minutes
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO monthly_summary(month,energy_wh,peak_power_w,productive_minutes,coverage_pct) VALUES(?,?,?,?,?)`, key, month.energyWh, month.peakPowerW, month.productiveMinutes, coverage); err != nil {
			return fmt.Errorf("insert rebuilt monthly summary: %w", err)
		}
	}
	return nil
}

func sortedCalendarKeys(values map[string]*calendarSummary) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
