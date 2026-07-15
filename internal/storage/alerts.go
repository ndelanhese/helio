package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ndelanhese/helio/internal/alerts"
)

type AlertRepository struct{ db *DB }

func NewAlertRepository(db *DB) *AlertRepository { return &AlertRepository{db: db} }

type AlertRecord struct {
	ID         string
	Rule       string
	State      string
	Severity   alerts.Severity
	OpenedAt   time.Time
	ResolvedAt *time.Time
	Evidence   alerts.Evidence
}

func (r *AlertRepository) Transact(ctx context.Context, apply func(alerts.Current) (alerts.Mutation, error)) ([]alerts.Transition, error) {
	if r == nil || r.db == nil || apply == nil {
		return nil, errors.New("alert transaction: repository and callback are required")
	}
	tx, err := r.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin alert transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	states, err := loadAlertStates(ctx, tx)
	if err != nil {
		return nil, err
	}
	open, err := loadOpenAlertRules(ctx, tx)
	if err != nil {
		return nil, err
	}
	mutation, err := apply(alerts.Current{States: states, Open: open})
	if err != nil {
		return nil, err
	}
	if err := validateAlertMutation(mutation); err != nil {
		return nil, err
	}

	for rule, state := range mutation.States {
		encoded, err := json.Marshal(state)
		if err != nil {
			return nil, fmt.Errorf("encode alert state %s: %w", rule, err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO alert_rule_state(rule,state_json,updated_at) VALUES(?,?,?)
			ON CONFLICT(rule) DO UPDATE SET state_json=excluded.state_json,updated_at=excluded.updated_at
			WHERE excluded.updated_at >= alert_rule_state.updated_at`, rule, string(encoded), formatTime(state.LastEvaluatedAt))
		if err != nil {
			return nil, fmt.Errorf("persist alert state %s: %w", rule, err)
		}
	}

	applied := make([]alerts.Transition, 0, len(mutation.Transitions))
	for _, transition := range mutation.Transitions {
		changed, err := applyAlertTransition(ctx, tx, transition)
		if err != nil {
			return nil, err
		}
		if changed {
			applied = append(applied, transition)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit alert transaction: %w", err)
	}
	return applied, nil
}

func loadAlertStates(ctx context.Context, tx *sql.Tx) (map[string]alerts.State, error) {
	rows, err := tx.QueryContext(ctx, `SELECT rule,state_json FROM alert_rule_state`)
	if err != nil {
		return nil, fmt.Errorf("query alert states: %w", err)
	}
	defer rows.Close()
	states := map[string]alerts.State{}
	for rows.Next() {
		var rule, encoded string
		var state alerts.State
		if err := rows.Scan(&rule, &encoded); err != nil {
			return nil, fmt.Errorf("scan alert state: %w", err)
		}
		if !knownAlertRule(rule) {
			return nil, fmt.Errorf("stored alert state has unknown rule %q", rule)
		}
		if err := json.Unmarshal([]byte(encoded), &state); err != nil {
			return nil, fmt.Errorf("decode alert state %s: %w", rule, err)
		}
		if err := validateAlertState(state); err != nil {
			return nil, fmt.Errorf("validate alert state %s: %w", rule, err)
		}
		states[rule] = state
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alert states: %w", err)
	}
	return states, nil
}

func loadOpenAlertRules(ctx context.Context, tx *sql.Tx) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT rule FROM alerts WHERE state='open'`)
	if err != nil {
		return nil, fmt.Errorf("query open alerts: %w", err)
	}
	defer rows.Close()
	open := map[string]bool{}
	for rows.Next() {
		var rule string
		if err := rows.Scan(&rule); err != nil {
			return nil, fmt.Errorf("scan open alert: %w", err)
		}
		if !knownAlertRule(rule) {
			return nil, fmt.Errorf("open alert has unknown rule %q", rule)
		}
		open[rule] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open alerts: %w", err)
	}
	return open, nil
}

func applyAlertTransition(ctx context.Context, tx *sql.Tx, transition alerts.Transition) (bool, error) {
	encoded, err := json.Marshal(transition.Evidence)
	if err != nil {
		return false, fmt.Errorf("encode alert evidence: %w", err)
	}
	var result sql.Result
	switch transition.Kind {
	case alerts.TransitionOpened:
		id, err := newAlertID()
		if err != nil {
			return false, err
		}
		result, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO alerts(id,rule,state,severity,opened_at,evidence_json) VALUES(?,?,'open',?,?,?)`, id, transition.Rule, transition.Severity, formatTime(transition.At), string(encoded))
		if err != nil {
			return false, fmt.Errorf("open alert %s: %w", transition.Rule, err)
		}
	case alerts.TransitionResolved:
		result, err = tx.ExecContext(ctx, `UPDATE alerts SET state='resolved',resolved_at=? WHERE rule=? AND state='open'`, formatTime(transition.At), transition.Rule)
		if err != nil {
			return false, fmt.Errorf("resolve alert %s: %w", transition.Rule, err)
		}
	default:
		return false, fmt.Errorf("unsupported alert transition %q", transition.Kind)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("inspect alert transition %s: %w", transition.Rule, err)
	}
	if changed == 0 {
		return false, nil
	}
	action := "alert.open"
	if transition.Kind == alerts.TransitionResolved {
		action = "alert.resolve"
	}
	detail := struct {
		Rule     string          `json:"rule"`
		Severity alerts.Severity `json:"severity"`
		At       time.Time       `json:"at"`
		Evidence alerts.Evidence `json:"evidence"`
	}{transition.Rule, transition.Severity, transition.At.UTC(), transition.Evidence}
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		return false, fmt.Errorf("encode alert audit: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO action_audit(occurred_at,actor_user_id,action,detail_json) VALUES(?,NULL,?,?)`, formatTime(transition.At), action, string(detailJSON)); err != nil {
		return false, fmt.Errorf("record alert audit: %w", err)
	}
	return true, nil
}

func (r *AlertRepository) List(ctx context.Context, state string) ([]AlertRecord, error) {
	if state != "" && state != "open" && state != "resolved" {
		return nil, errors.New("list alerts: state must be open or resolved")
	}
	query := `SELECT id,rule,state,severity,opened_at,resolved_at,evidence_json FROM alerts`
	args := []any{}
	if state != "" {
		query += ` WHERE state=?`
		args = append(args, state)
	}
	query += ` ORDER BY opened_at,id`
	rows, err := r.db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query alerts: %w", err)
	}
	defer rows.Close()
	records := make([]AlertRecord, 0)
	for rows.Next() {
		var record AlertRecord
		var severity, opened, evidenceJSON string
		var resolved sql.NullString
		if err := rows.Scan(&record.ID, &record.Rule, &record.State, &severity, &opened, &resolved, &evidenceJSON); err != nil {
			return nil, fmt.Errorf("scan alert: %w", err)
		}
		record.Severity = alerts.Severity(severity)
		if record.OpenedAt, err = time.Parse(sqliteTimeLayout, opened); err != nil {
			return nil, fmt.Errorf("parse alert opened time: %w", err)
		}
		if resolved.Valid {
			at, parseErr := time.Parse(sqliteTimeLayout, resolved.String)
			if parseErr != nil {
				return nil, fmt.Errorf("parse alert resolved time: %w", parseErr)
			}
			record.ResolvedAt = &at
		}
		if err := json.Unmarshal([]byte(evidenceJSON), &record.Evidence); err != nil {
			return nil, fmt.Errorf("decode alert evidence: %w", err)
		}
		if !knownAlertRule(record.Rule) || (record.Severity != alerts.SeverityWarning && record.Severity != alerts.SeverityCritical) {
			return nil, errors.New("stored alert has invalid rule or severity")
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alerts: %w", err)
	}
	return records, nil
}

func validateAlertMutation(mutation alerts.Mutation) error {
	for rule, state := range mutation.States {
		if !knownAlertRule(rule) {
			return fmt.Errorf("alert mutation has unknown rule %q", rule)
		}
		if err := validateAlertState(state); err != nil {
			return fmt.Errorf("alert mutation state %s: %w", rule, err)
		}
	}
	for _, transition := range mutation.Transitions {
		if !knownAlertRule(transition.Rule) || transition.At.IsZero() || (transition.Kind != alerts.TransitionOpened && transition.Kind != alerts.TransitionResolved) || (transition.Severity != alerts.SeverityWarning && transition.Severity != alerts.SeverityCritical) {
			return errors.New("alert mutation contains invalid transition metadata")
		}
		for _, value := range transition.Evidence.Values {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return errors.New("alert evidence must be finite")
			}
		}
		for _, at := range transition.Evidence.Timestamps {
			if at.IsZero() {
				return errors.New("alert evidence timestamps must be nonzero")
			}
		}
	}
	return nil
}

func validateAlertState(state alerts.State) error {
	if state.Consecutive < 0 {
		return errors.New("counter must be nonnegative")
	}
	if !state.PendingSince.IsZero() && !state.LastEvaluatedAt.IsZero() && state.PendingSince.After(state.LastEvaluatedAt) {
		return errors.New("pending time is after evaluation time")
	}
	if !state.LastEvidenceAt.IsZero() && !state.LastEvaluatedAt.IsZero() && state.LastEvidenceAt.After(state.LastEvaluatedAt) {
		return errors.New("evidence time is after evaluation time")
	}
	if state.LastKey != "" {
		if _, err := time.Parse("2006-01-02", state.LastKey); err != nil {
			return errors.New("last key is not a local date")
		}
	}
	return nil
}

func knownAlertRule(rule string) bool {
	for _, known := range alerts.StableRuleOrder() {
		if rule == known {
			return true
		}
	}
	return false
}

func newAlertID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate alert id: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}
