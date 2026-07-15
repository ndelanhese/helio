//go:build smoke

package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/storage"
	_ "modernc.org/sqlite"
)

const markerStatus = "smoke-fixture"

func main() {
	if len(os.Args) != 3 {
		fatal(errors.New("usage: smokefixture seed|validate DATABASE"))
	}
	var err error
	switch os.Args[1] {
	case "seed":
		err = seed(os.Args[2])
	case "validate":
		err = validate(os.Args[2])
	default:
		err = errors.New("unknown smokefixture command")
	}
	if err != nil {
		fatal(err)
	}
}

func seed(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := storage.Open(ctx, path)
	if err != nil {
		return err
	}
	repository := storage.NewTelemetryRepository(db, time.UTC)
	from := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Minute)
	for index, power := range []float64{4321, 4322} {
		at := from.Add(time.Duration(index) * time.Minute)
		if err := repository.SaveMinute(ctx, domain.TelemetrySnapshot{
			ObservedAt: at, ACPowerW: power, EnergyTodayWh: 9876 + float64(index),
			EnergyLifetimeWh: 123456 + float64(index), Status: markerStatus,
		}); err != nil {
			_ = db.Close()
			return err
		}
	}
	if err := db.Close(); err != nil {
		return err
	}
	fmt.Println(from.Format(time.RFC3339))
	fmt.Println(from.Add(3 * time.Minute).Format(time.RFC3339))
	return nil
}

func validate(path string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(contents, []byte("SQLite format 3\x00")) {
		return errors.New("backup does not have a SQLite header")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(abs)+"?mode=ro&_pragma=query_only%281%29")
	if err != nil {
		return err
	}
	defer db.Close()
	var integrity string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("SQLite integrity check: %s", integrity)
	}
	for label, query := range map[string]string{
		"fixture telemetry": `SELECT count(*) FROM telemetry_minute WHERE status='smoke-fixture' AND ac_power_w IN (4321,4322)`,
		"administrator":     `SELECT count(*) FROM users`,
		"persisted session": `SELECT count(*) FROM sessions`,
		"backup audit":      `SELECT count(*) FROM action_audit WHERE action='data.backup'`,
	} {
		var count int
		if err := db.QueryRow(query).Scan(&count); err != nil {
			return fmt.Errorf("validate %s: %w", label, err)
		}
		minimum := 1
		if label == "fixture telemetry" {
			minimum = 2
		}
		if count < minimum {
			return fmt.Errorf("validate %s: count=%d", label, count)
		}
	}
	var settings string
	if err := db.QueryRow(`SELECT value_json FROM settings WHERE key='system'`).Scan(&settings); err != nil {
		return err
	}
	if !strings.Contains(settings, `"tariffMinorPerKWh":96`) {
		return errors.New("updated settings are absent from backup")
	}
	return nil
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, "smoke fixture:", err)
	os.Exit(1)
}
