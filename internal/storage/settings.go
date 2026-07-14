package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	normalized, err := config.ValidateSettings(settings, allowPublicLogger...)
	if err != nil {
		return fmt.Errorf("validate settings: %w", err)
	}
	payload, err := json.Marshal(settingsEnvelope{Version: settingsVersion, Settings: normalized})
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	_, err = db.sql.ExecContext(ctx, `
		INSERT INTO settings(key, value_json, updated_at) VALUES('system', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json, updated_at=excluded.updated_at`,
		string(payload), formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("put settings: %w", err)
	}
	return nil
}

// GetSettings loads and strictly decodes the versioned settings document.
func (db *DB) GetSettings(ctx context.Context) (domain.Settings, error) {
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
	normalized, err := config.ValidateSettings(envelope.Settings, true)
	if err != nil {
		return domain.Settings{}, fmt.Errorf("validate stored settings: %w", err)
	}
	if envelope.Settings.InstalledPowerW != normalized.InstalledPowerW {
		return domain.Settings{}, fmt.Errorf("stored installed power does not match panel configuration")
	}
	return normalized, nil
}
