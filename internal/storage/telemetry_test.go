package storage

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

func telemetryRepository(t *testing.T, loc *time.Location) (*DB, *TelemetryRepository) {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, NewTelemetryRepository(db, loc)
}

func snapshot(at time.Time, power, lifetime float64) domain.TelemetrySnapshot {
	return domain.TelemetrySnapshot{
		ObservedAt: at, Status: "normal", ACPowerW: power,
		EnergyTodayWh: power / 10, EnergyLifetimeWh: lifetime,
		PV1:  domain.MPPT{Active: true, VoltageV: 400, CurrentA: 2, PowerW: 800},
		PV2:  domain.MPPT{Active: true, VoltageV: 390, CurrentA: 1, PowerW: 390},
		Grid: domain.Grid{VoltageV: 230, FrequencyHz: 60}, FaultCodes: []uint16{4, 9},
	}
}

func closeEnough(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %.12f, want %.12f", got, want)
	}
}

func TestTelemetryMinuteConsolidationAndHistoryGaps(t *testing.T) {
	ctx := context.Background()
	db, repo := telemetryRepository(t, time.FixedZone("odd", -3*60*60-30*60))
	base := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)

	for _, s := range []domain.TelemetrySnapshot{
		snapshot(base.Add(5*time.Second), 100, 1000),
		snapshot(base.Add(45*time.Second), 200, 900),
		snapshot(base.Add(2*time.Minute), 300, 1200),
		snapshot(base.Add(3*time.Minute), 500, 1300),
	} {
		if err := repo.SaveMinute(ctx, s); err != nil {
			t.Fatal(err)
		}
	}

	points, err := repo.History(ctx, base, base.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 3 {
		t.Fatalf("history length=%d, want 3", len(points))
	}
	for i, minute := range []int{0, 2, 3} {
		want := base.Add(time.Duration(minute) * time.Minute)
		if !points[i].At.Equal(want) {
			t.Fatalf("point %d at %s, want %s", i, points[i].At, want)
		}
	}
	if points[0].PowerW != 200 {
		t.Fatalf("consolidated power=%v, want later observation power 200", points[0].PowerW)
	}
	var lifetime float64
	var faults string
	if err := db.sql.QueryRowContext(ctx, `SELECT energy_lifetime_wh, fault_codes_json FROM telemetry_minute WHERE observed_at=?`, formatTime(base)).Scan(&lifetime, &faults); err != nil {
		t.Fatal(err)
	}
	if lifetime != 1000 {
		t.Fatalf("lifetime=%v, want maximum 1000", lifetime)
	}
	var decoded []uint16
	if err := json.Unmarshal([]byte(faults), &decoded); err != nil || len(decoded) != 2 {
		t.Fatalf("fault codes=%q: %v", faults, err)
	}
}

func TestTelemetryMinuteKeepsLatestSourceObservationOutOfOrder(t *testing.T) {
	ctx := context.Background()
	db, repo := telemetryRepository(t, time.UTC)
	minute := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	newer := snapshot(minute.Add(45*time.Second), 200, 1000)
	older := snapshot(minute.Add(5*time.Second), 100, 1100)
	if err := repo.SaveMinute(ctx, newer); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveMinute(ctx, older); err != nil {
		t.Fatal(err)
	}

	var source string
	var power, lifetime float64
	if err := db.sql.QueryRowContext(ctx, `
		SELECT observed_at_utc, ac_power_w, energy_lifetime_wh
		FROM telemetry_minute WHERE observed_at=?`, formatTime(minute)).Scan(&source, &power, &lifetime); err != nil {
		t.Fatal(err)
	}
	if source != formatTime(newer.ObservedAt) || power != 200 || lifetime != 1100 {
		t.Fatalf("after older source=%q power=%v lifetime=%v", source, power, lifetime)
	}

	latest := snapshot(minute.Add(55*time.Second), 300, 900)
	if err := repo.SaveMinute(ctx, latest); err != nil {
		t.Fatal(err)
	}
	// Equal source timestamps are deterministic: the first payload wins.
	tie := snapshot(latest.ObservedAt, 999, 1200)
	if err := repo.SaveMinute(ctx, tie); err != nil {
		t.Fatal(err)
	}
	if err := db.sql.QueryRowContext(ctx, `
		SELECT observed_at_utc, ac_power_w, energy_lifetime_wh
		FROM telemetry_minute WHERE observed_at=?`, formatTime(minute)).Scan(&source, &power, &lifetime); err != nil {
		t.Fatal(err)
	}
	if source != formatTime(latest.ObservedAt) || power != 300 || lifetime != 1200 {
		t.Fatalf("after newer/tie source=%q power=%v lifetime=%v", source, power, lifetime)
	}
}

func TestAggregateGapCoverageAndEnergyUnits(t *testing.T) {
	ctx := context.Background()
	_, repo := telemetryRepository(t, time.UTC)
	base := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		minute int
		power  float64
	}{{0, 60}, {2, 120}, {3, 180}} {
		if err := repo.SaveMinute(ctx, snapshot(base.Add(time.Duration(item.minute)*time.Minute), item.power, 1)); err != nil {
			t.Fatal(err)
		}
	}

	hour, err := repo.AggregateHour(ctx, base, base.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	closeEnough(t, hour.EnergyWh, 2.5) // only 120W -> 180W over one minute
	closeEnough(t, hour.CoveragePct, 75)
	if hour.PeakPowerW != 180 {
		t.Fatalf("peak=%v", hour.PeakPowerW)
	}

	day, err := repo.AggregateDay(ctx, base, base.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	closeEnough(t, day.EnergyWh, 2.5*4.0/60.0)
	closeEnough(t, day.CoveragePct, 75)
	if day.ProductiveMinutes != 3 {
		t.Fatalf("productive=%d, want 3", day.ProductiveMinutes)
	}
}

func TestAggregateDayCountsMissingHoursAsZeroCoverage(t *testing.T) {
	ctx := context.Background()
	db, repo := telemetryRepository(t, time.UTC)
	start := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	if _, err := db.sql.ExecContext(ctx, `
		INSERT INTO hourly_summary(hour, energy_wh, peak_power_w, coverage_pct)
		VALUES (?, 60, 100, 100)`, start.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	day, err := repo.AggregateDay(ctx, start, start.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	closeEnough(t, day.CoveragePct, 50)
}

func TestAggregateDayCoverageUsesDSTDayDuration(t *testing.T) {
	ctx := context.Background()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	db, repo := telemetryRepository(t, loc)
	for _, item := range []struct {
		day   time.Time
		hours float64
		want  float64
	}{
		{time.Date(2026, 3, 8, 0, 0, 0, 0, loc), 23, 100.0 / 23},
		{time.Date(2026, 11, 1, 0, 0, 0, 0, loc), 25, 100.0 / 25},
	} {
		if got := item.day.AddDate(0, 0, 1).Sub(item.day).Hours(); got != item.hours {
			t.Fatalf("test day duration=%v, want %v", got, item.hours)
		}
		if _, err := db.sql.ExecContext(ctx, `
			INSERT INTO hourly_summary(hour, energy_wh, peak_power_w, coverage_pct)
			VALUES (?, 60, 100, 100)`, item.day.Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
		day, err := repo.AggregateDay(ctx, item.day, item.day.AddDate(0, 0, 1))
		if err != nil {
			t.Fatal(err)
		}
		closeEnough(t, day.CoveragePct, item.want)
	}
}

func TestAggregateDayUsesTrueOverlapAndProratesHourlyEnergy(t *testing.T) {
	ctx := context.Background()
	db, repo := telemetryRepository(t, time.UTC)
	base := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	for i, item := range []struct {
		energy   float64
		coverage float64
	}{{60, 100}, {120, 50}, {180, 0}} {
		if _, err := db.sql.ExecContext(ctx, `
			INSERT INTO hourly_summary(hour, energy_wh, peak_power_w, coverage_pct)
			VALUES (?, ?, ?, ?)`, base.Add(time.Duration(i)*time.Hour).Format(time.RFC3339), item.energy, item.energy, item.coverage); err != nil {
			t.Fatal(err)
		}
	}

	day, err := repo.AggregateDay(ctx, base.Add(30*time.Minute), base.Add(150*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	closeEnough(t, day.CoveragePct, 50) // 30m*100% + 60m*50% + 30m*0%, over 120m
	closeEnough(t, day.EnergyWh, 240)   // 60/2 + 120 + 180/2
}

func TestTelemetryHistoryFractionalBoundariesUseChronologicalOrdering(t *testing.T) {
	ctx := context.Background()
	_, repo := telemetryRepository(t, time.UTC)
	at := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	if err := repo.SaveMinute(ctx, snapshot(at, 100, 1)); err != nil {
		t.Fatal(err)
	}

	points, err := repo.History(ctx, at.Add(-time.Nanosecond), at.Add(time.Nanosecond))
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || !points[0].At.Equal(at) {
		t.Fatalf("history=%v, want point at fractional [from,to) boundary", points)
	}
	points, err = repo.History(ctx, at.Add(time.Nanosecond), at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 0 {
		t.Fatalf("history=%v, minute before fractional from must be excluded", points)
	}
}

func TestAggregateLocalBucketsUpsertAndWeightedMonth(t *testing.T) {
	ctx := context.Background()
	loc := time.FixedZone("BRT", -3*60*60)
	db, repo := telemetryRepository(t, loc)
	jan31 := time.Date(2026, 1, 31, 23, 0, 0, 0, loc)
	feb1 := jan31.Add(2 * time.Hour)

	for _, start := range []time.Time{jan31.Add(-time.Hour), jan31, feb1} {
		for _, offset := range []time.Duration{0, time.Minute} {
			if err := repo.SaveMinute(ctx, snapshot(start.Add(offset), 60, 1)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := repo.AggregateHour(ctx, start, start.Add(4*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.AggregateDay(ctx, jan31.Add(-time.Hour), jan31.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AggregateDay(ctx, feb1, feb1.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	// Re-run after changing raw data: deterministic upsert, not duplicate/ignore.
	if err := repo.SaveMinute(ctx, snapshot(jan31.Add(time.Minute+30*time.Second), 180, 2)); err != nil {
		t.Fatal(err)
	}
	updated, err := repo.AggregateHour(ctx, jan31, jan31.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	closeEnough(t, updated.EnergyWh, 2)
	updatedDay, err := repo.AggregateDay(ctx, jan31.Add(-time.Hour), jan31.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	closeEnough(t, updatedDay.EnergyWh, 1+2*4.0/60.0)

	jan, err := repo.AggregateMonth(ctx, jan31)
	if err != nil {
		t.Fatal(err)
	}
	feb, err := repo.AggregateMonth(ctx, feb1)
	if err != nil {
		t.Fatal(err)
	}
	if jan.Month != "2026-01" || feb.Month != "2026-02" {
		t.Fatalf("local month keys=%q,%q", jan.Month, feb.Month)
	}
	closeEnough(t, jan.CoveragePct, 50)
	closeEnough(t, feb.CoveragePct, 50)

	var hourKey, dayKey string
	if err := db.sql.QueryRowContext(ctx, `SELECT hour FROM hourly_summary ORDER BY hour LIMIT 1`).Scan(&hourKey); err != nil {
		t.Fatal(err)
	}
	if err := db.sql.QueryRowContext(ctx, `SELECT day FROM daily_summary ORDER BY day LIMIT 1`).Scan(&dayKey); err != nil {
		t.Fatal(err)
	}
	if hourKey != "2026-01-31T22:00:00-03:00" || dayKey != "2026-01-31" {
		t.Fatalf("local keys hour=%q day=%q", hourKey, dayKey)
	}
}

func TestAggregateRepeatedLocalHourKeepsBothUTCIntervals(t *testing.T) {
	ctx := context.Background()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	db, repo := telemetryRepository(t, loc)
	// The wall-clock hour 01:00 occurs twice when daylight saving time ends.
	for _, start := range []time.Time{
		time.Date(2026, 11, 1, 5, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 1, 6, 0, 0, 0, time.UTC),
	} {
		if err := repo.SaveMinute(ctx, snapshot(start, 60, 1)); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.AggregateHour(ctx, start, start.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM hourly_summary`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("hourly summaries=%d, want both repeated local hours", count)
	}
}

func TestTelemetryEventAndRetentionPreserveSummaries(t *testing.T) {
	ctx := context.Background()
	db, repo := telemetryRepository(t, time.UTC)
	base := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	for i := range 3 {
		if err := repo.SaveMinute(ctx, snapshot(base.Add(time.Duration(i)*time.Minute), 60, 1)); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SaveEvent(ctx, base, "status", map[string]string{"from": "offline"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AggregateHour(ctx, base, base.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AggregateDay(ctx, base, base.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AggregateMonth(ctx, base); err != nil {
		t.Fatal(err)
	}

	deleted, err := repo.PruneBefore(ctx, base.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("deleted=%d, want 2", deleted)
	}
	for _, table := range []string{"hourly_summary", "daily_summary", "monthly_summary"} {
		var count int
		if err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count == 0 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	var remaining string
	if err := db.sql.QueryRowContext(ctx, `SELECT observed_at FROM telemetry_minute`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != formatTime(base.Add(2*time.Minute)) {
		t.Fatalf("remaining=%q", remaining)
	}
	var payload string
	if err := db.sql.QueryRowContext(ctx, `SELECT payload_json FROM telemetry_events`).Scan(&payload); err != nil || payload != `{"from":"offline"}` {
		t.Fatalf("payload=%q err=%v", payload, err)
	}
}

func TestSummaryHistoryUsesPersistedRowsAndLocalDSTBoundaries(t *testing.T) {
	ctx := context.Background()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	db, repo := telemetryRepository(t, loc)
	start := time.Date(2026, 3, 8, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO daily_summary(day,energy_wh,peak_power_w,productive_minutes,coverage_pct) VALUES('2026-03-08',1000,500,60,95)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `DELETE FROM telemetry_minute`); err != nil {
		t.Fatal(err)
	}
	points, err := repo.DailyHistory(ctx, start.UTC(), end.UTC(), loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || !points[0].At.Equal(start.UTC()) || points[0].EnergyWh != 1000 {
		t.Fatalf("DST daily summary: %#v", points)
	}
	if end.Sub(start) != 23*time.Hour {
		t.Fatalf("test did not cross DST: %v", end.Sub(start))
	}
}

func TestMonthlyHistoryConvertsLocalBucketStartToUTC(t *testing.T) {
	ctx := context.Background()
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	db, repo := telemetryRepository(t, loc)
	start := time.Date(2026, 2, 1, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 1, 0)
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO monthly_summary(month,energy_wh,peak_power_w,productive_minutes,coverage_pct) VALUES('2026-02',5000,900,100,90)`); err != nil {
		t.Fatal(err)
	}
	points, err := repo.MonthlyHistory(ctx, start.UTC(), end.UTC(), loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || !points[0].At.Equal(start.UTC()) || points[0].EnergyWh != 5000 {
		t.Fatalf("monthly summary: %#v", points)
	}
}

func TestRetentionPrunesAcrossBatchBoundary(t *testing.T) {
	ctx := context.Background()
	db, repo := telemetryRepository(t, time.UTC)
	_, err := db.sql.ExecContext(ctx, `
		WITH RECURSIVE seq(n) AS (
			VALUES(0) UNION ALL SELECT n+1 FROM seq WHERE n < 10000
		)
		INSERT INTO telemetry_minute(
			observed_at, ac_power_w, energy_today_wh, energy_lifetime_wh,
			pv1_voltage_v, pv1_current_a, pv1_power_w, pv2_active,
			pv2_voltage_v, pv2_current_a, pv2_power_w, grid_voltage_v,
			grid_frequency_hz, status, fault_codes_json
		)
		SELECT printf('2025-01-%05d', n), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 'offline', '[]'
		FROM seq`)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := repo.PruneBefore(ctx, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 10001 {
		t.Fatalf("deleted=%d, want 10001 across two batches", deleted)
	}
}
