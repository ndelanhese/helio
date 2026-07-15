package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/alerts"
)

func TestAlertRepositoryPersistsPendingStateAcrossRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "alerts.db")
	first, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := alerts.NewEngine(NewAlertRepository(first), alerts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	in := storageSunnyInput(base)
	if got, err := engine.Evaluate(ctx, in); err != nil || len(got) != 0 {
		t.Fatalf("first=%v err=%v", got, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	engine, err = alerts.NewEngine(NewAlertRepository(second), alerts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	in.At = base.Add(20 * time.Minute)
	got, err := engine.Evaluate(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Rule != alerts.RuleZeroSunnyGeneration {
		t.Fatalf("transitions=%v", got)
	}
}

func TestAlertRepositoryOpenResolveAuditsOnlyTransitions(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "alerts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := NewAlertRepository(db)
	engine, err := alerts.NewEngine(repository, alerts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	failed := alerts.Input{At: base, PollObserved: true}
	for i := 0; i < 4; i++ {
		failed.At = base.Add(time.Duration(i) * time.Second)
		if _, err := engine.Evaluate(ctx, failed); err != nil {
			t.Fatal(err)
		}
	}
	success := failed
	success.At = base.Add(5 * time.Second)
	success.PollSucceeded = true
	if _, err := engine.Evaluate(ctx, success); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Evaluate(ctx, success); err != nil {
		t.Fatal(err)
	}
	open, err := repository.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].State != "resolved" {
		t.Fatalf("alerts=%+v", open)
	}
	if got := open[0].Evidence.Values["failed_polls"]; got != 0 {
		t.Fatalf("resolved evidence failed_polls=%v, want recovery value 0", got)
	}
	var audits int
	if err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM action_audit WHERE action IN ('alert.open','alert.resolve')`).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("audits=%d err=%v", audits, err)
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT detail_json FROM action_audit WHERE action IN ('alert.open','alert.resolve') ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var encoded string
		var detail struct {
			Evidence struct {
				Values     map[string]float64   `json:"values"`
				Timestamps map[string]time.Time `json:"timestamps"`
			} `json:"evidence"`
		}
		if err := rows.Scan(&encoded); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(encoded), &detail); err != nil {
			t.Fatal(err)
		}
		if len(detail.Evidence.Values) == 0 || len(detail.Evidence.Timestamps) == 0 {
			t.Fatalf("transition audit lacks numeric/timestamp evidence: %s", encoded)
		}
	}
}

func TestAlertRepositoryListsNewestFirstWithSafeCap(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "alerts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	for index := range 105 {
		at := base.Add(time.Duration(index) * time.Minute)
		if _, err := db.sql.ExecContext(ctx, `INSERT INTO alerts(id,rule,state,severity,opened_at,resolved_at,evidence_json) VALUES(?,?,'resolved',?,?,?,?)`,
			fmt.Sprintf("alert-%03d", index), alerts.RuleLoggerOffline, alerts.SeverityCritical, formatTime(at), formatTime(at.Add(time.Second)), `{}`); err != nil {
			t.Fatal(err)
		}
	}
	records, err := NewAlertRepository(db).List(ctx, "resolved")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 100 || records[0].ID != "alert-104" || records[99].ID != "alert-005" {
		t.Fatalf("bounded newest alerts: len=%d first=%s last=%s", len(records), records[0].ID, records[len(records)-1].ID)
	}
}

func TestAlertRepositoryConcurrentOpenIsUnique(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "alerts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	engine, err := alerts.NewEngine(NewAlertRepository(db), alerts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if _, err := engine.Evaluate(ctx, alerts.Input{At: base.Add(time.Duration(i) * time.Second), PollObserved: true}); err != nil {
			t.Fatal(err)
		}
	}
	var wait sync.WaitGroup
	errors := make(chan error, 20)
	wait.Add(20)
	for range 20 {
		go func() {
			defer wait.Done()
			_, err := engine.Evaluate(ctx, alerts.Input{At: base.Add(2 * time.Second), PollObserved: true})
			errors <- err
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent evaluation: %v", err)
		}
	}
	records, err := NewAlertRepository(db).List(ctx, "open")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("open records=%d", len(records))
	}
}

func storageSunnyInput(at time.Time) alerts.Input {
	return alerts.Input{At: at, TelemetryObserved: true, TelemetryFresh: true, WeatherAvailable: true, SolarElevationDeg: 11, IrradianceWM2: 200, TelemetryCoveragePct: 80, GridVoltageV: 220, GridFrequencyHz: 60}
}
