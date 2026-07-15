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

type calendarSegment struct {
	dayKey     string
	duration   time.Duration
	productive int
	remainder  int64
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
		segments := splitCalendarHour(at, location, productive)
		for _, segment := range segments {
			summary := days[segment.dayKey]
			if summary == nil {
				start, err := time.ParseInLocation("2006-01-02", segment.dayKey, location)
				if err != nil {
					rows.Close()
					return fmt.Errorf("parse calendar rebuild day: %w", err)
				}
				summary = &calendarSummary{minutes: start.AddDate(0, 0, 1).Sub(start).Minutes()}
				days[segment.dayKey] = summary
			}
			fraction := float64(segment.duration) / float64(time.Hour)
			summary.energyWh += energy * fraction
			// The hourly schema has no peak timestamp, so conservatively expose
			// its peak in every overlapped local day.
			summary.peakPowerW = math.Max(summary.peakPowerW, peak)
			summary.coverageWeight += coverage * segment.duration.Minutes()
			summary.productiveMinutes += segment.productive
		}
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

func splitCalendarHour(at time.Time, location *time.Location, productive int) []calendarSegment {
	end := at.Add(time.Hour)
	segments := make([]calendarSegment, 0, 2)
	for cursor := at; cursor.Before(end); {
		local := cursor.In(location)
		dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
		nextDay := dayStart.AddDate(0, 0, 1)
		segmentEnd := end
		if nextDay.Before(segmentEnd) {
			segmentEnd = nextDay
		}
		duration := segmentEnd.Sub(cursor)
		numerator := int64(productive) * int64(duration)
		segments = append(segments, calendarSegment{dayKey: local.Format("2006-01-02"), duration: duration,
			productive: int(numerator / int64(time.Hour)), remainder: numerator % int64(time.Hour)})
		cursor = segmentEnd
	}
	allocated := 0
	for _, segment := range segments {
		allocated += segment.productive
	}
	remaining := productive - allocated
	order := make([]int, len(segments))
	for index := range order {
		order[index] = index
	}
	sort.SliceStable(order, func(left, right int) bool { return segments[order[left]].remainder > segments[order[right]].remainder })
	for index := 0; index < remaining; index++ {
		segments[order[index%len(order)]].productive++
	}
	return segments
}

func sortedCalendarKeys(values map[string]*calendarSummary) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
