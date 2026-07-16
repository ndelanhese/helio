package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

func TestApproveProposalMakesImmutableVersion(t *testing.T) {
	ctx, db, repo := financeTestRepository(t)
	proposal := storeProposal(t, ctx, repo, candidate("2026-06-24", "2027-06-23"))

	approved, err := repo.ApproveProposal(ctx, proposal.ID, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if approved.ID == 0 || approved.ApprovedAt.IsZero() {
		t.Fatalf("approved tariff = %+v", approved)
	}
	if err := repo.UpdateTariff(ctx, approved.ID, approved); !errors.Is(err, ErrImmutable) {
		t.Fatalf("UpdateTariff() error = %v, want ErrImmutable", err)
	}

	var versions int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM tariff_versions WHERE proposal_id=?`, proposal.ID).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 1 {
		t.Fatalf("versions=%d, want 1", versions)
	}
}

func TestSaveCycleConsumesLotsByExpiry(t *testing.T) {
	ctx, db, repo := financeTestRepository(t)
	approved := approveCandidate(t, ctx, repo)
	seedLots(t, ctx, db, lot("2028-01-01", 100), lot("2029-01-01", 100))

	cycle := cycleWithCredits(120)
	cycle.TariffVersionID = approved.ID
	cycle.CreditBalanceKWh = 80
	_, projection, err := repo.SaveCycle(ctx, cycle, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if projection.BillingCycleID == 0 || projection.TariffVersionID != approved.ID {
		t.Fatalf("projection = %+v", projection)
	}
	if got := remainingLots(t, ctx, db); len(got) != 2 || got[0] != 0 || got[1] != 80 {
		t.Fatalf("remaining lots=%v, want [0 80]", got)
	}
}

func TestSaveCycleAddsUnknownLotForPositiveReportedBalance(t *testing.T) {
	ctx, db, repo := financeTestRepository(t)
	approved := approveCandidate(t, ctx, repo)
	cycle := cycleWithCredits(0)
	cycle.TariffVersionID = approved.ID
	cycle.CreditBalanceKWh = 35

	if _, _, err := repo.SaveCycle(ctx, cycle, "user-1"); err != nil {
		t.Fatal(err)
	}
	var available, partial int64
	if err := db.sql.QueryRowContext(ctx, `SELECT available_kwh, is_partial FROM credit_lots`).Scan(&available, &partial); err != nil {
		t.Fatal(err)
	}
	if available != 35 || partial != 1 {
		t.Fatalf("unknown lot=(%d, %d), want (35, 1)", available, partial)
	}
}

func TestSaveCycleRejectsNegativeReportedBalanceDifference(t *testing.T) {
	ctx, db, repo := financeTestRepository(t)
	approved := approveCandidate(t, ctx, repo)
	seedLots(t, ctx, db, lot("2028-01-01", 100))
	cycle := cycleWithCredits(0)
	cycle.TariffVersionID = approved.ID
	cycle.CreditBalanceKWh = 99

	if _, _, err := repo.SaveCycle(ctx, cycle, "user-1"); err == nil {
		t.Fatal("SaveCycle() succeeded with a negative balance difference")
	}
}

func TestLatestProjectionAndListCycles(t *testing.T) {
	ctx, _, repo := financeTestRepository(t)
	approved := approveCandidate(t, ctx, repo)
	cycle := cycleWithCredits(0)
	cycle.TariffVersionID = approved.ID
	saved, want, err := repo.SaveCycle(ctx, cycle, "user-1")
	if err != nil {
		t.Fatal(err)
	}

	got, ok, err := repo.LatestProjection(ctx, saved.ReadingEnd)
	if err != nil || !ok || got.ID != want.ID {
		t.Fatalf("LatestProjection() = (%+v, %t, %v), want projection %d", got, ok, err, want.ID)
	}
	cycles, err := repo.ListCycles(ctx, 10)
	if err != nil || len(cycles) != 1 || cycles[0].ID != saved.ID {
		t.Fatalf("ListCycles() = (%+v, %v)", cycles, err)
	}
}

func financeTestRepository(t *testing.T) (context.Context, *DB, *FinanceRepository) {
	t.Helper()
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "helio.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO users(id, username, password_hash, created_at) VALUES ('user-1', 'finance', 'hash', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	return ctx, db, NewFinanceRepository(db)
}

func candidate(from, to string) domain.TariffProposal {
	return domain.TariffProposal{
		Distributor: "COPEL", EffectiveFrom: parseFinanceDate(from), EffectiveTo: parseFinanceDate(to),
		ConsumptionTEMicrosPerKWh: 389503, ConsumptionTUSDMicrosPerKWh: 538944,
		CompensationTEMicrosPerKWh: 389503, CompensationTUSDMicrosPerKWh: 538944,
		AvailabilityKWh: 100, CIPMinor: 2556, SourceURL: "https://example.test/tariff",
		ParserVersion: "v1", RetrievedAt: parseFinanceDate(from),
	}
}

func storeProposal(t *testing.T, ctx context.Context, repo *FinanceRepository, proposal domain.TariffProposal) domain.TariffProposal {
	t.Helper()
	stored, err := repo.CreateProposal(ctx, proposal)
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func approveCandidate(t *testing.T, ctx context.Context, repo *FinanceRepository) domain.TariffVersion {
	t.Helper()
	proposal := storeProposal(t, ctx, repo, candidate("2026-06-24", "2027-06-23"))
	approved, err := repo.ApproveProposal(ctx, proposal.ID, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	return approved
}

func cycleWithCredits(used int64) domain.BillingCycle {
	return domain.BillingCycle{
		ReadingStart: parseFinanceDate("2026-07-01"), ReadingEnd: parseFinanceDate("2026-07-31"),
		ActiveConsumptionKWh: 500, CreditsUsedKWh: used,
	}
}

func seedLots(t *testing.T, ctx context.Context, db *DB, lots ...domain.CreditLot) {
	t.Helper()
	for _, credit := range lots {
		if _, err := db.sql.ExecContext(ctx, `INSERT INTO credit_lots(origin_cycle_id, available_kwh, expires_at, is_partial, created_at) VALUES(NULL, ?, ?, ?, ?)`, credit.AvailableKWh, formatTime(credit.ExpiresAt), boolToInt(credit.IsPartial), formatTime(time.Now())); err != nil {
			t.Fatal(err)
		}
	}
}

func remainingLots(t *testing.T, ctx context.Context, db *DB) []int64 {
	t.Helper()
	rows, err := db.sql.QueryContext(ctx, `SELECT available_kwh FROM credit_lots ORDER BY expires_at, id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var values []int64
	for rows.Next() {
		var value int64
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return values
}

func lot(expires string, available int64) domain.CreditLot {
	return domain.CreditLot{ExpiresAt: parseFinanceDate(expires), AvailableKWh: available}
}

func parseFinanceDate(value string) time.Time {
	at, err := time.Parse("2006-01-02", value)
	if err != nil {
		panic(err)
	}
	return at.UTC()
}
