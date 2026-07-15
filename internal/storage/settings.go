package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/ndelanhese/helio/internal/config"
	"github.com/ndelanhese/helio/internal/domain"
)

const settingsVersion = 1

type settingsEnvelope struct {
	Version  int             `json:"version"`
	Settings domain.Settings `json:"settings"`
}

// PutSettings validates and stores the complete normalized settings document
// in one upsert. The optional flag is the same explicit public-host override
// accepted by config.ValidateSettings.
func (db *DB) PutSettings(ctx context.Context, settings domain.Settings, allowPublicLogger ...bool) error {
	return putSettings(ctx, db.sql, settings, allowPublicLogger...)
}

// ApplySettings commits settings and their required administrative audit row
// in the same SQLite transaction.
func (db *DB) ApplySettings(ctx context.Context, settings domain.Settings, actorUserID string, allowPublicLogger bool) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin settings update: %w", err)
	}
	defer tx.Rollback()
	var previousPayload string
	previousTimezone := ""
	if err := tx.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key='system'`).Scan(&previousPayload); err == nil {
		var previous settingsEnvelope
		if err := json.Unmarshal([]byte(previousPayload), &previous); err != nil {
			return fmt.Errorf("decode previous settings: %w", err)
		}
		previousTimezone = previous.Settings.Timezone
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read previous settings: %w", err)
	}
	if err := putSettings(ctx, tx, settings, allowPublicLogger); err != nil {
		return err
	}
	detail := map[string]any{"fields": []string{"loggerHost", "loggerPort", "modbusSlave", "panelCount", "panelWattage", "activeMPPT", "latitude", "longitude", "timezone", "currency", "tariffMinorPerKWh", "retentionDays"}}
	if err := insertAudit(ctx, tx, actorUserID, "settings.update", detail); err != nil {
		return err
	}
	if previousTimezone != "" && previousTimezone != settings.Timezone {
		location, err := time.LoadLocation(settings.Timezone)
		if err != nil {
			return fmt.Errorf("load settings timezone: %w", err)
		}
		if err := rebuildCalendarSummaries(ctx, tx, location); err != nil {
			return err
		}
		if err := invalidateTimezoneDerivedEvidence(ctx, tx, actorUserID, previousTimezone, settings.Timezone); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit settings update: %w", err)
	}
	return nil
}

func invalidateTimezoneDerivedEvidence(ctx context.Context, tx *sql.Tx, actorUserID, previousTimezone, nextTimezone string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM daily_analysis`); err != nil {
		return fmt.Errorf("invalidate daily analysis after timezone change: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM alert_rule_state WHERE rule='persistent_underproduction'`); err != nil {
		return fmt.Errorf("reset underproduction state after timezone change: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM alerts WHERE rule='persistent_underproduction'`); err != nil {
		return fmt.Errorf("invalidate underproduction alerts after timezone change: %w", err)
	}
	detail := map[string]any{"previousTimezone": previousTimezone, "timezone": nextTimezone}
	if err := insertAudit(ctx, tx, actorUserID, "analysis.invalidate_timezone", detail); err != nil {
		return fmt.Errorf("audit timezone evidence invalidation: %w", err)
	}
	return nil
}

func putSettings(ctx context.Context, db execer, settings domain.Settings, allowPublicLogger ...bool) error {
	normalized, err := config.ValidateSettings(settings, allowPublicLogger...)
	if err != nil {
		return fmt.Errorf("validate settings: %w", err)
	}
	payload, err := json.Marshal(settingsEnvelope{Version: settingsVersion, Settings: normalized})
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO settings(key, value_json, updated_at) VALUES('system', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json, updated_at=excluded.updated_at`,
		string(payload), formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("put settings: %w", err)
	}
	return nil
}

// GetSettings loads and strictly decodes the versioned settings document.
func (db *DB) GetSettings(ctx context.Context, allowPublicLogger ...bool) (domain.Settings, error) {
	var payload string
	err := db.sql.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key='system'`).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Settings{}, ErrNotFound
	}
	if err != nil {
		return domain.Settings{}, fmt.Errorf("get settings: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewBufferString(payload))
	decoder.DisallowUnknownFields()
	var envelope settingsEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return domain.Settings{}, fmt.Errorf("decode settings: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return domain.Settings{}, fmt.Errorf("decode settings: %w", err)
	}
	if envelope.Version != settingsVersion {
		return domain.Settings{}, fmt.Errorf("unsupported settings version %d", envelope.Version)
	}
	normalized, err := config.ValidateSettings(envelope.Settings, allowPublicLogger...)
	if err != nil {
		return domain.Settings{}, fmt.Errorf("validate stored settings: %w", err)
	}
	if !reflect.DeepEqual(envelope.Settings, normalized) {
		return domain.Settings{}, fmt.Errorf("stored settings are not canonical")
	}
	return normalized, nil
}
