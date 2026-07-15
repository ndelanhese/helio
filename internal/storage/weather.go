package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ndelanhese/helio/internal/weather"
)

type WeatherRepository struct{ db *DB }

func NewWeatherRepository(db *DB) *WeatherRepository { return &WeatherRepository{db: db} }

func (r *WeatherRepository) Upsert(ctx context.Context, hours []weather.Hour, source string, fetchedAt time.Time) error {
	if source == "" {
		return errors.New("upsert weather: source is required")
	}
	if fetchedAt.IsZero() {
		return errors.New("upsert weather: fetched time is required")
	}
	tx, err := r.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin weather upsert: %w", err)
	}
	defer tx.Rollback()
	for _, hour := range hours {
		if hour.Time.IsZero() {
			return errors.New("upsert weather: hour is required")
		}
		hourUTC := hour.Time.UTC().Truncate(time.Hour)
		_, err := tx.ExecContext(ctx, `INSERT INTO weather_hourly(hour, cloud_cover_pct, irradiance_wm2, source, fetched_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(hour) DO UPDATE SET cloud_cover_pct=excluded.cloud_cover_pct,
			irradiance_wm2=excluded.irradiance_wm2, source=excluded.source, fetched_at=excluded.fetched_at`,
			formatTime(hourUTC), hour.CloudCoverPct, hour.IrradianceWM2, source, formatTime(fetchedAt.UTC()))
		if err != nil {
			return fmt.Errorf("upsert weather hour: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit weather upsert: %w", err)
	}
	return nil
}

func (r *WeatherRepository) Load(ctx context.Context, from, to time.Time) ([]weather.Hour, time.Time, string, error) {
	rows, err := r.db.sql.QueryContext(ctx, `SELECT hour, cloud_cover_pct, irradiance_wm2, source, fetched_at
		FROM weather_hourly WHERE hour >= ? AND hour < ? ORDER BY hour`, formatTime(from), formatTime(to))
	if err != nil {
		return nil, time.Time{}, "", fmt.Errorf("query weather cache: %w", err)
	}
	defer rows.Close()
	hours := make([]weather.Hour, 0)
	var latest time.Time
	var latestSource string
	for rows.Next() {
		var rawHour, rawFetched, source string
		var hour weather.Hour
		if err := rows.Scan(&rawHour, &hour.CloudCoverPct, &hour.IrradianceWM2, &source, &rawFetched); err != nil {
			return nil, time.Time{}, "", fmt.Errorf("scan weather cache: %w", err)
		}
		hour.Time, err = time.Parse(sqliteTimeLayout, rawHour)
		if err != nil {
			return nil, time.Time{}, "", fmt.Errorf("parse weather hour: %w", err)
		}
		fetched, err := time.Parse(sqliteTimeLayout, rawFetched)
		if err != nil {
			return nil, time.Time{}, "", fmt.Errorf("parse weather fetched time: %w", err)
		}
		if fetched.After(latest) {
			latest, latestSource = fetched, source
		}
		hours = append(hours, hour)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, "", fmt.Errorf("iterate weather cache: %w", err)
	}
	return hours, latest.UTC(), latestSource, nil
}
