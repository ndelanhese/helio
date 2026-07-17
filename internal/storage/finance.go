package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/finance"
)

// FinanceRepository persists approved tariffs and reconciled billing cycles.
type FinanceRepository struct {
	db         *DB
	locationMu sync.RWMutex
	location   *time.Location
}

// NewFinanceRepository uses the configured billing location when supplied.
// UTC remains the default for callers that have not configured a site yet.
func NewFinanceRepository(db *DB, locations ...*time.Location) *FinanceRepository {
	location := time.UTC
	if len(locations) > 0 && locations[0] != nil {
		location = locations[0]
	}
	return &FinanceRepository{db: db, location: location}
}

// SetLocation updates the configured billing calendar for subsequent saves.
func (r *FinanceRepository) SetLocation(location *time.Location) {
	if location == nil {
		location = time.UTC
	}
	r.locationMu.Lock()
	r.location = location
	r.locationMu.Unlock()
}

func (r *FinanceRepository) Location() *time.Location {
	r.locationMu.RLock()
	defer r.locationMu.RUnlock()
	if r.location == nil {
		return time.UTC
	}
	return r.location
}

// CreateProposal stores a tariff candidate that can later be approved.
func (r *FinanceRepository) CreateProposal(ctx context.Context, proposal domain.TariffProposal) (domain.TariffProposal, error) {
	if err := validateProposal(proposal); err != nil {
		return domain.TariffProposal{}, err
	}
	result, err := r.db.sql.ExecContext(ctx, `
		INSERT INTO tariff_proposals(
			distributor, effective_from, effective_to, consumption_te_micros_per_kwh,
			consumption_tusd_micros_per_kwh, compensation_te_micros_per_kwh,
			compensation_tusd_micros_per_kwh, flag_micros_per_kwh, availability_kwh,
			cip_minor, source_url, parser_version, retrieved_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		proposal.Distributor, formatTime(proposal.EffectiveFrom), formatTime(proposal.EffectiveTo),
		proposal.ConsumptionTEMicrosPerKWh, proposal.ConsumptionTUSDMicrosPerKWh,
		proposal.CompensationTEMicrosPerKWh, proposal.CompensationTUSDMicrosPerKWh,
		proposal.FlagMicrosPerKWh, proposal.AvailabilityKWh, proposal.CIPMinor, proposal.SourceURL,
		proposal.ParserVersion, formatTime(proposal.RetrievedAt))
	if err != nil {
		return domain.TariffProposal{}, fmt.Errorf("create tariff proposal: %w", err)
	}
	proposal.ID, err = result.LastInsertId()
	if err != nil {
		return domain.TariffProposal{}, fmt.Errorf("read tariff proposal id: %w", err)
	}
	return proposal, nil
}

// HasApprovedTariff reports whether a user-approved schedule exists. Pending
// source candidates deliberately do not count as an approved fallback.
func (r *FinanceRepository) HasApprovedTariff(ctx context.Context) (bool, error) {
	var exists int
	err := r.db.sql.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tariff_versions)`).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check approved tariffs: %w", err)
	}
	return exists != 0, nil
}

// UpdateTariff is intentionally rejected: approved tariff versions are immutable.
func (r *FinanceRepository) UpdateTariff(context.Context, int64, domain.TariffVersion) error {
	return ErrImmutable
}

// ApproveProposal atomically marks a proposal approved and snapshots it as a
// tariff version for future reconciliations.
func (r *FinanceRepository) ApproveProposal(ctx context.Context, proposalID int64, actorUserID string) (domain.TariffVersion, error) {
	tx, err := r.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return domain.TariffVersion{}, fmt.Errorf("begin tariff approval: %w", err)
	}
	defer tx.Rollback()

	proposal, err := loadProposal(ctx, tx, proposalID)
	if err != nil {
		return domain.TariffVersion{}, err
	}
	if !proposal.ApprovedAt.IsZero() {
		return domain.TariffVersion{}, ErrImmutable
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE tariff_proposals SET approved_at=?, approved_by=? WHERE id=?`, formatTime(now), actorUserID, proposalID); err != nil {
		return domain.TariffVersion{}, fmt.Errorf("approve tariff proposal: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO tariff_versions(
			proposal_id, distributor, effective_from, effective_to, consumption_te_micros_per_kwh,
			consumption_tusd_micros_per_kwh, compensation_te_micros_per_kwh,
			compensation_tusd_micros_per_kwh, flag_micros_per_kwh, availability_kwh,
			cip_minor, source_url, retrieved_at, approved_at, approved_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		proposal.ID, proposal.Distributor, formatTime(proposal.EffectiveFrom), formatTime(proposal.EffectiveTo),
		proposal.ConsumptionTEMicrosPerKWh, proposal.ConsumptionTUSDMicrosPerKWh,
		proposal.CompensationTEMicrosPerKWh, proposal.CompensationTUSDMicrosPerKWh,
		proposal.FlagMicrosPerKWh, proposal.AvailabilityKWh, proposal.CIPMinor, proposal.SourceURL,
		formatTime(proposal.RetrievedAt), formatTime(now), actorUserID)
	if err != nil {
		return domain.TariffVersion{}, fmt.Errorf("create approved tariff version: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return domain.TariffVersion{}, fmt.Errorf("read tariff version id: %w", err)
	}
	if err := insertAudit(ctx, tx, actorUserID, "tariff.approve", map[string]any{"proposalID": proposalID, "tariffVersionID": id}); err != nil {
		return domain.TariffVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.TariffVersion{}, fmt.Errorf("commit tariff approval: %w", err)
	}
	return tariffFromProposal(proposal, id, now), nil
}

// SaveCycle records a bill and its calculated reconciliation in one transaction.
func (r *FinanceRepository) SaveCycle(ctx context.Context, cycle domain.BillingCycle, actorUserID string) (domain.BillingCycle, domain.FinancialProjection, error) {
	if err := domain.ValidateBillingCycle(cycle); err != nil {
		return domain.BillingCycle{}, domain.FinancialProjection{}, fmt.Errorf("validate billing cycle: %w", err)
	}
	tx, err := r.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return domain.BillingCycle{}, domain.FinancialProjection{}, fmt.Errorf("begin billing cycle: %w", err)
	}
	defer tx.Rollback()

	tariff, err := loadTariffAt(ctx, tx, cycle.ReadingEnd, r.Location())
	if err != nil {
		return domain.BillingCycle{}, domain.FinancialProjection{}, err
	}
	cycle.TariffVersionID = tariff.ID
	projection, err := finance.Calculate(tariff, cycle)
	if err != nil {
		return domain.BillingCycle{}, domain.FinancialProjection{}, fmt.Errorf("calculate billing projection: %w", err)
	}
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO billing_cycles(
			reading_start, reading_end, active_consumption_kwh, injected_kwh, credits_used_kwh,
			credit_balance_kwh, total_paid_minor, flag_charge_minor, tariff_version_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		formatTime(cycle.ReadingStart), formatTime(cycle.ReadingEnd), cycle.ActiveConsumptionKWh,
		cycle.InjectedKWh, cycle.CreditsUsedKWh, cycle.CreditBalanceKWh, cycle.TotalPaidMinor, cycle.FlagChargeMinor,
		cycle.TariffVersionID, formatTime(now))
	if err != nil {
		return domain.BillingCycle{}, domain.FinancialProjection{}, fmt.Errorf("insert billing cycle: %w", err)
	}
	cycle.ID, err = result.LastInsertId()
	if err != nil {
		return domain.BillingCycle{}, domain.FinancialProjection{}, fmt.Errorf("read billing cycle id: %w", err)
	}
	projection.BillingCycleID = cycle.ID
	projection.TariffVersionID = tariff.ID
	projection.IsEstimate = false
	projection.CalculatedAt = now
	projectionResult, err := tx.ExecContext(ctx, `
		INSERT INTO bill_reconciliations(
			billing_cycle_id, projection_consumption_minor, projection_compensation_minor,
			projection_flag_minor, projection_taxes_minor, projection_cip_minor, projection_total_minor,
			without_solar_compensation_minor, is_estimate, calculated_at, reconciled_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		projection.BillingCycleID, projection.ConsumptionMinor, projection.CompensationMinor,
		projection.FlagMinor, projection.TaxesMinor, projection.CIPMinor, projection.TotalMinor,
		projection.WithoutSolarCompensationMinor, boolToInt(projection.IsEstimate), formatTime(now), formatTime(now))
	if err != nil {
		return domain.BillingCycle{}, domain.FinancialProjection{}, fmt.Errorf("insert bill reconciliation: %w", err)
	}
	projection.ID, err = projectionResult.LastInsertId()
	if err != nil {
		return domain.BillingCycle{}, domain.FinancialProjection{}, fmt.Errorf("read bill reconciliation id: %w", err)
	}
	if cycle.InjectedKWh > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_lots(origin_cycle_id, available_kwh, expires_at, is_partial, created_at) VALUES(?, ?, ?, 0, ?)`, cycle.ID, cycle.InjectedKWh, formatTime(cycle.ReadingEnd.AddDate(5, 0, 0)), formatTime(now)); err != nil {
			return domain.BillingCycle{}, domain.FinancialProjection{}, fmt.Errorf("insert injected credit lot: %w", err)
		}
	}
	if err := consumeCreditLots(ctx, tx, cycle.CreditsUsedKWh); err != nil {
		return domain.BillingCycle{}, domain.FinancialProjection{}, err
	}
	remaining, err := remainingCreditLots(ctx, tx)
	if err != nil {
		return domain.BillingCycle{}, domain.FinancialProjection{}, err
	}
	if difference := cycle.CreditBalanceKWh - remaining; difference < 0 {
		return domain.BillingCycle{}, domain.FinancialProjection{}, fmt.Errorf("validate reported credit balance: %w", errors.New("reported balance is less than available lots"))
	} else if difference > 0 {
		// The bill provides only an aggregate. Preserve that uncertainty rather
		// than inventing a lot-by-lot history; Brazilian credit validity is five years.
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_lots(origin_cycle_id, available_kwh, expires_at, is_partial, created_at) VALUES(NULL, ?, ?, 1, ?)`, difference, formatTime(cycle.ReadingEnd.AddDate(5, 0, 0)), formatTime(now)); err != nil {
			return domain.BillingCycle{}, domain.FinancialProjection{}, fmt.Errorf("insert unknown credit lot: %w", err)
		}
	}
	if err := insertAudit(ctx, tx, actorUserID, "billing_cycle.save", map[string]any{"billingCycleID": cycle.ID, "tariffVersionID": tariff.ID}); err != nil {
		return domain.BillingCycle{}, domain.FinancialProjection{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.BillingCycle{}, domain.FinancialProjection{}, fmt.Errorf("commit billing cycle: %w", err)
	}
	return cycle, projection, nil
}

// LatestProjection returns the most recent reconciliation no later than at.
func (r *FinanceRepository) LatestProjection(ctx context.Context, at time.Time) (domain.FinancialProjection, bool, error) {
	row := r.db.sql.QueryRowContext(ctx, projectionSelect+` WHERE c.reading_end <= ? ORDER BY c.reading_end DESC, c.id DESC LIMIT 1`, formatTime(at))
	projection, err := scanProjection(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.FinancialProjection{}, false, nil
	}
	if err != nil {
		return domain.FinancialProjection{}, false, fmt.Errorf("load latest projection: %w", err)
	}
	return projection, true, nil
}

func (r *FinanceRepository) CreditSummary(ctx context.Context) (int64, *time.Time, error) {
	var balance int64
	var expiry sql.NullString
	err := r.db.sql.QueryRowContext(ctx, `SELECT COALESCE(SUM(available_kwh), 0), MIN(CASE WHEN available_kwh > 0 THEN expires_at END) FROM credit_lots`).Scan(&balance, &expiry)
	if err != nil {
		return 0, nil, fmt.Errorf("credit summary: %w", err)
	}
	if !expiry.Valid {
		return balance, nil, nil
	}
	value, err := parseTime(expiry.String)
	if err != nil {
		return 0, nil, err
	}
	return balance, &value, nil
}

func (r *FinanceRepository) LatestTariff(ctx context.Context) (domain.TariffVersion, bool, error) {
	row := r.db.sql.QueryRowContext(ctx, `SELECT id, distributor, effective_from, effective_to, consumption_te_micros_per_kwh, consumption_tusd_micros_per_kwh, compensation_te_micros_per_kwh, compensation_tusd_micros_per_kwh, flag_micros_per_kwh, availability_kwh, cip_minor, source_url, retrieved_at, approved_at FROM tariff_versions ORDER BY approved_at DESC, id DESC LIMIT 1`)
	var t domain.TariffVersion
	var from, to, retrieved, approved string
	err := row.Scan(&t.ID, &t.Distributor, &from, &to, &t.ConsumptionTEMicrosPerKWh, &t.ConsumptionTUSDMicrosPerKWh, &t.CompensationTEMicrosPerKWh, &t.CompensationTUSDMicrosPerKWh, &t.FlagMicrosPerKWh, &t.AvailabilityKWh, &t.CIPMinor, &t.SourceURL, &retrieved, &approved)
	if errors.Is(err, sql.ErrNoRows) {
		return t, false, nil
	}
	if err != nil {
		return t, false, err
	}
	var parseErr error
	t.EffectiveFrom, parseErr = parseTime(from)
	if parseErr == nil {
		t.EffectiveTo, parseErr = parseTime(to)
	}
	if parseErr == nil {
		t.RetrievedAt, parseErr = parseTime(retrieved)
	}
	if parseErr == nil {
		t.ApprovedAt, parseErr = parseTime(approved)
	}
	return t, true, parseErr
}

// ListCycles returns newest billing cycles first.
func (r *FinanceRepository) ListCycles(ctx context.Context, limit int) ([]domain.BillingCycle, error) {
	if limit <= 0 || limit > 1000 {
		return nil, fmt.Errorf("cycle limit must be between 1 and 1000")
	}
	rows, err := r.db.sql.QueryContext(ctx, `SELECT id, reading_start, reading_end, active_consumption_kwh, injected_kwh, credits_used_kwh, credit_balance_kwh, total_paid_minor, flag_charge_minor, tariff_version_id FROM billing_cycles ORDER BY reading_end DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list billing cycles: %w", err)
	}
	defer rows.Close()
	cycles := make([]domain.BillingCycle, 0)
	for rows.Next() {
		cycle, err := scanCycle(rows)
		if err != nil {
			return nil, err
		}
		cycles = append(cycles, cycle)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate billing cycles: %w", err)
	}
	return cycles, nil
}

// ListTariffProposals returns newest proposed tariffs first.
func (r *FinanceRepository) ListTariffProposals(ctx context.Context) ([]domain.TariffProposal, error) {
	rows, err := r.db.sql.QueryContext(ctx, `SELECT id, distributor, effective_from, effective_to, consumption_te_micros_per_kwh, consumption_tusd_micros_per_kwh, compensation_te_micros_per_kwh, compensation_tusd_micros_per_kwh, flag_micros_per_kwh, availability_kwh, cip_minor, source_url, parser_version, retrieved_at, approved_at FROM tariff_proposals ORDER BY retrieved_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list tariff proposals: %w", err)
	}
	defer rows.Close()
	proposals := make([]domain.TariffProposal, 0)
	for rows.Next() {
		proposal, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		proposals = append(proposals, proposal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tariff proposals: %w", err)
	}
	return proposals, nil
}

func validateProposal(proposal domain.TariffProposal) error {
	if proposal.ParserVersion == "" || proposal.RetrievedAt.IsZero() {
		return errors.New("tariff parser version and retrieval time are required")
	}
	return domain.ValidateTariffVersion(domain.TariffVersion{
		Distributor: proposal.Distributor, EffectiveFrom: proposal.EffectiveFrom, EffectiveTo: proposal.EffectiveTo,
		ConsumptionTEMicrosPerKWh: proposal.ConsumptionTEMicrosPerKWh, ConsumptionTUSDMicrosPerKWh: proposal.ConsumptionTUSDMicrosPerKWh,
		CompensationTEMicrosPerKWh: proposal.CompensationTEMicrosPerKWh, CompensationTUSDMicrosPerKWh: proposal.CompensationTUSDMicrosPerKWh,
		FlagMicrosPerKWh: proposal.FlagMicrosPerKWh, AvailabilityKWh: proposal.AvailabilityKWh, CIPMinor: proposal.CIPMinor,
	})
}

func loadProposal(ctx context.Context, tx *sql.Tx, id int64) (domain.TariffProposal, error) {
	var proposal domain.TariffProposal
	var effectiveFrom, effectiveTo, retrievedAt string
	var approvedAt sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT id, distributor, effective_from, effective_to, consumption_te_micros_per_kwh, consumption_tusd_micros_per_kwh, compensation_te_micros_per_kwh, compensation_tusd_micros_per_kwh, flag_micros_per_kwh, availability_kwh, cip_minor, source_url, parser_version, retrieved_at, approved_at FROM tariff_proposals WHERE id=?`, id).Scan(&proposal.ID, &proposal.Distributor, &effectiveFrom, &effectiveTo, &proposal.ConsumptionTEMicrosPerKWh, &proposal.ConsumptionTUSDMicrosPerKWh, &proposal.CompensationTEMicrosPerKWh, &proposal.CompensationTUSDMicrosPerKWh, &proposal.FlagMicrosPerKWh, &proposal.AvailabilityKWh, &proposal.CIPMinor, &proposal.SourceURL, &proposal.ParserVersion, &retrievedAt, &approvedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TariffProposal{}, ErrNotFound
	}
	if err != nil {
		return domain.TariffProposal{}, fmt.Errorf("load tariff proposal: %w", err)
	}
	var parseErr error
	proposal.EffectiveFrom, parseErr = parseTime(effectiveFrom)
	if parseErr == nil {
		proposal.EffectiveTo, parseErr = parseTime(effectiveTo)
	}
	if parseErr == nil {
		proposal.RetrievedAt, parseErr = parseTime(retrievedAt)
	}
	if approvedAt.Valid && parseErr == nil {
		proposal.ApprovedAt, parseErr = parseTime(approvedAt.String)
	}
	if parseErr != nil {
		return domain.TariffProposal{}, fmt.Errorf("parse tariff proposal time: %w", parseErr)
	}
	return proposal, nil
}

func scanProposal(row rowScanner) (domain.TariffProposal, error) {
	var proposal domain.TariffProposal
	var effectiveFrom, effectiveTo, retrievedAt string
	var approvedAt sql.NullString
	err := row.Scan(&proposal.ID, &proposal.Distributor, &effectiveFrom, &effectiveTo, &proposal.ConsumptionTEMicrosPerKWh, &proposal.ConsumptionTUSDMicrosPerKWh, &proposal.CompensationTEMicrosPerKWh, &proposal.CompensationTUSDMicrosPerKWh, &proposal.FlagMicrosPerKWh, &proposal.AvailabilityKWh, &proposal.CIPMinor, &proposal.SourceURL, &proposal.ParserVersion, &retrievedAt, &approvedAt)
	if err != nil {
		return domain.TariffProposal{}, fmt.Errorf("scan tariff proposal: %w", err)
	}
	proposal.EffectiveFrom, err = parseTime(effectiveFrom)
	if err == nil {
		proposal.EffectiveTo, err = parseTime(effectiveTo)
	}
	if err == nil {
		proposal.RetrievedAt, err = parseTime(retrievedAt)
	}
	if err == nil && approvedAt.Valid {
		proposal.ApprovedAt, err = parseTime(approvedAt.String)
	}
	if err != nil {
		return domain.TariffProposal{}, fmt.Errorf("parse tariff proposal time: %w", err)
	}
	return proposal, nil
}

func tariffFromProposal(proposal domain.TariffProposal, id int64, approvedAt time.Time) domain.TariffVersion {
	return domain.TariffVersion{ID: id, Distributor: proposal.Distributor, EffectiveFrom: proposal.EffectiveFrom, EffectiveTo: proposal.EffectiveTo, ConsumptionTEMicrosPerKWh: proposal.ConsumptionTEMicrosPerKWh, ConsumptionTUSDMicrosPerKWh: proposal.ConsumptionTUSDMicrosPerKWh, CompensationTEMicrosPerKWh: proposal.CompensationTEMicrosPerKWh, CompensationTUSDMicrosPerKWh: proposal.CompensationTUSDMicrosPerKWh, FlagMicrosPerKWh: proposal.FlagMicrosPerKWh, AvailabilityKWh: proposal.AvailabilityKWh, CIPMinor: proposal.CIPMinor, SourceURL: proposal.SourceURL, RetrievedAt: proposal.RetrievedAt, ApprovedAt: approvedAt}
}

func loadTariffAt(ctx context.Context, tx *sql.Tx, at time.Time, location *time.Location) (domain.TariffVersion, error) {
	calendarDate := billingCalendarDate(at, location)
	var tariff domain.TariffVersion
	var effectiveFrom, effectiveTo, retrievedAt, approvedAt string
	err := tx.QueryRowContext(ctx, `SELECT id, distributor, effective_from, effective_to, consumption_te_micros_per_kwh, consumption_tusd_micros_per_kwh, compensation_te_micros_per_kwh, compensation_tusd_micros_per_kwh, flag_micros_per_kwh, availability_kwh, cip_minor, source_url, retrieved_at, approved_at FROM tariff_versions WHERE effective_from <= ? AND effective_to >= ? ORDER BY effective_from DESC, id DESC LIMIT 1`, formatTime(calendarDate), formatTime(calendarDate)).Scan(&tariff.ID, &tariff.Distributor, &effectiveFrom, &effectiveTo, &tariff.ConsumptionTEMicrosPerKWh, &tariff.ConsumptionTUSDMicrosPerKWh, &tariff.CompensationTEMicrosPerKWh, &tariff.CompensationTUSDMicrosPerKWh, &tariff.FlagMicrosPerKWh, &tariff.AvailabilityKWh, &tariff.CIPMinor, &tariff.SourceURL, &retrievedAt, &approvedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TariffVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.TariffVersion{}, fmt.Errorf("load tariff for cycle: %w", err)
	}
	var parseErr error
	tariff.EffectiveFrom, parseErr = parseTime(effectiveFrom)
	if parseErr == nil {
		tariff.EffectiveTo, parseErr = parseTime(effectiveTo)
	}
	if parseErr == nil {
		tariff.RetrievedAt, parseErr = parseTime(retrievedAt)
	}
	if parseErr == nil {
		tariff.ApprovedAt, parseErr = parseTime(approvedAt)
	}
	if parseErr != nil {
		return domain.TariffVersion{}, fmt.Errorf("parse approved tariff time: %w", parseErr)
	}
	return tariff, nil
}

func billingCalendarDate(at time.Time, location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}
	local := at.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

func consumeCreditLots(ctx context.Context, tx *sql.Tx, needed int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, available_kwh FROM credit_lots WHERE available_kwh > 0 ORDER BY expires_at ASC, id ASC`)
	if err != nil {
		return fmt.Errorf("load credit lots: %w", err)
	}
	defer rows.Close()
	for rows.Next() && needed > 0 {
		var id, available int64
		if err := rows.Scan(&id, &available); err != nil {
			return fmt.Errorf("scan credit lot: %w", err)
		}
		used := min(needed, available)
		if _, err := tx.ExecContext(ctx, `UPDATE credit_lots SET available_kwh=available_kwh-? WHERE id=?`, used, id); err != nil {
			return fmt.Errorf("consume credit lot: %w", err)
		}
		needed -= used
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate credit lots: %w", err)
	}
	if needed > 0 {
		return errors.New("credits used exceed available credit lots")
	}
	return nil
}

func remainingCreditLots(ctx context.Context, tx *sql.Tx) (int64, error) {
	var remaining int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(available_kwh), 0) FROM credit_lots`).Scan(&remaining); err != nil {
		return 0, fmt.Errorf("sum remaining credit lots: %w", err)
	}
	return remaining, nil
}

const projectionSelect = `SELECT r.id, r.billing_cycle_id, c.tariff_version_id, r.projection_consumption_minor, r.projection_compensation_minor, r.projection_flag_minor, c.flag_charge_minor, r.projection_taxes_minor, r.projection_cip_minor, r.projection_total_minor, r.without_solar_compensation_minor, r.is_estimate, r.calculated_at FROM bill_reconciliations r JOIN billing_cycles c ON c.id=r.billing_cycle_id`

type rowScanner interface{ Scan(...any) error }

func scanProjection(row rowScanner) (domain.FinancialProjection, error) {
	var projection domain.FinancialProjection
	var estimate int
	var calculatedAt string
	err := row.Scan(&projection.ID, &projection.BillingCycleID, &projection.TariffVersionID, &projection.ConsumptionMinor, &projection.CompensationMinor, &projection.FlagMinor, &projection.FlagChargeMinor, &projection.TaxesMinor, &projection.CIPMinor, &projection.TotalMinor, &projection.WithoutSolarCompensationMinor, &estimate, &calculatedAt)
	if err != nil {
		return domain.FinancialProjection{}, err
	}
	projection.IsEstimate = estimate != 0
	projection.CalculatedAt, err = parseTime(calculatedAt)
	if err != nil {
		return domain.FinancialProjection{}, fmt.Errorf("parse projection time: %w", err)
	}
	return projection, nil
}

func scanCycle(row rowScanner) (domain.BillingCycle, error) {
	var cycle domain.BillingCycle
	var start, end string
	err := row.Scan(&cycle.ID, &start, &end, &cycle.ActiveConsumptionKWh, &cycle.InjectedKWh, &cycle.CreditsUsedKWh, &cycle.CreditBalanceKWh, &cycle.TotalPaidMinor, &cycle.FlagChargeMinor, &cycle.TariffVersionID)
	if err != nil {
		return domain.BillingCycle{}, fmt.Errorf("scan billing cycle: %w", err)
	}
	cycle.ReadingStart, err = parseTime(start)
	if err == nil {
		cycle.ReadingEnd, err = parseTime(end)
	}
	if err != nil {
		return domain.BillingCycle{}, fmt.Errorf("parse billing cycle time: %w", err)
	}
	return cycle, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
