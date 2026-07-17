# Helio Finance and Billing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build one-UC financial tracking with approved versioned tariffs, manual bill reconciliation, credit lots, and an explainable projected bill.

**Architecture:** Finance remains separate from inverter telemetry. Domain types use exact integer money; the calculator is pure; storage makes tariff approval and bill/lot changes transactional; API exposes authenticated records; React renders server-calculated totals.

**Tech Stack:** Go, SQLite/WAL, Chi, React, TypeScript, TanStack Query, Vitest, Playwright.

## Global Constraints

- One generating UC only. No beneficiary UC, transfer, allocation, bill upload, or parser.
- Amounts use centavos; rates use micro-reais per kWh. No floating-point money.
- Tariff versions immutable after approval. A source refresh creates a proposal only.
- Billing uses reading start/end in configured timezone.
- No meter means projected values must say `estimate`; no real-time consumption, import, export, or self-consumption.
- CIP, penalties, interest, installments, and services stay outside solar compensation.
- Existing session, CSRF, same-origin, audit, read-only inverter, and local-first boundaries apply.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/domain/finance.go` | Tariff, bill cycle, credit lot, projection types and validation. |
| `internal/finance/calculator.go` | Pure tariff calculation. |
| `internal/storage/migrations/0008_finance.sql` | Finance tables and indexes. |
| `internal/storage/finance.go` | Transactional proposal, bill-cycle, and credit-lot repository. |
| `internal/tariffs/copel.go` | Fixture-tested Copel candidate parser. |
| `internal/tariffs/service.go` | Approved-tariff fallback and proposal refresh state. |
| `internal/api/finance.go` | Authenticated finance routes. |
| `web/src/features/finance/*` | Tariff approval, bill entry, and finance page. |

## Task 1: Domain types and SQLite schema

**Files:**
- Create: `internal/domain/finance.go`
- Create: `internal/domain/finance_test.go`
- Create: `internal/storage/migrations/0008_finance.sql`
- Modify: `internal/storage/migrate_test.go`

**Interfaces:**
- Produces `domain.TariffVersion`, `domain.TariffProposal`, `domain.BillingCycle`, `domain.CreditLot`, `domain.FinancialProjection`.
- Produces `ValidateTariffVersion(TariffVersion) error` and `ValidateBillingCycle(BillingCycle) error`.

- [ ] **Step 1: Write failing tests**

```go
func TestValidateTariffVersion(t *testing.T) {
  valid := domain.TariffVersion{Distributor: "COPEL", EffectiveFrom: date("2026-06-24"), EffectiveTo: date("2027-06-23"), ConsumptionTEMicrosPerKWh: 389503, ConsumptionTUSDMicrosPerKWh: 538944, AvailabilityKWh: 100}
  require.NoError(t, domain.ValidateTariffVersion(valid))
  valid.AvailabilityKWh = 99
  require.ErrorContains(t, domain.ValidateTariffVersion(valid), "availability")
}
func TestFinanceMigrationCreatesTables(t *testing.T) {
  db := openMigratedDB(t)
  requireTable(t, db, "tariff_versions")
  requireTable(t, db, "credit_lots")
}
```

- [ ] **Step 2: Verify failure**

Run: `rtk go test ./internal/domain ./internal/storage -run 'TestValidateTariffVersion|TestFinanceMigrationCreatesTables' -count=1`

Expected: FAIL because finance types and migration do not exist.

- [ ] **Step 3: Add minimal types and schema**

```go
type TariffVersion struct {
  ID int64
  Distributor string
  EffectiveFrom, EffectiveTo time.Time
  ConsumptionTEMicrosPerKWh, ConsumptionTUSDMicrosPerKWh int64
  CompensationTEMicrosPerKWh, CompensationTUSDMicrosPerKWh int64
  FlagMicrosPerKWh int64
  AvailabilityKWh int
  CIPMinor int64
  SourceURL string
  RetrievedAt, ApprovedAt time.Time
}
type BillingCycle struct {
  ID int64
  ReadingStart, ReadingEnd time.Time
  ActiveConsumptionKWh, InjectedKWh, CreditsUsedKWh, CreditBalanceKWh int64
  TotalPaidMinor, TariffVersionID int64
}
```

Add tables `tariff_proposals`, `tariff_versions`, `billing_cycles`, `credit_lots`, and `bill_reconciliations`. Use nonnegative `CHECK` constraints, foreign keys, and indexes on tariff effective dates, cycle end, and lot expiry. Approved tariff rows have no update trigger; proposal rows remain mutable only until approval.

- [ ] **Step 4: Verify pass**

Run: `rtk go test ./internal/domain ./internal/storage -run 'TestValidateTariffVersion|TestFinanceMigrationCreatesTables' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/finance.go internal/domain/finance_test.go internal/storage/migrations/0008_finance.sql internal/storage/migrate_test.go
git commit -m "feat: add finance domain and schema"
```

## Task 2: Pure bill projection calculator

**Files:**
- Create: `internal/finance/calculator.go`
- Create: `internal/finance/calculator_test.go`

**Interfaces:**
- Consumes Task 1 types.
- Produces `finance.Calculate(domain.TariffVersion, domain.BillingCycle) (domain.FinancialProjection, error)`.

- [ ] **Step 1: Write failing tests**

```go
func TestCalculateAppliesAvailabilityFloorAndSeparatesCIP(t *testing.T) {
  got, err := finance.Calculate(tariff(100, 389503, 538944), cycle(79, 0, 0, 0))
  require.NoError(t, err)
  require.Equal(t, int64(9284), got.ConsumptionMinor)
  require.Equal(t, int64(2556), got.CIPMinor)
  require.Equal(t, int64(11840), got.TotalMinor)
}
func TestCounterfactualRemovesOnlyCompensation(t *testing.T) {
  got, err := finance.Calculate(tariff(100, 389503, 538944), cycle(322, 243, 243, 1878))
  require.NoError(t, err)
  require.Equal(t, got.TotalMinor+got.CompensationMinor, got.WithoutSolarCompensationMinor)
}
```

- [ ] **Step 2: Verify failure**

Run: `rtk go test ./internal/finance -run 'TestCalculateAppliesAvailabilityFloorAndSeparatesCIP|TestCounterfactualRemovesOnlyCompensation' -count=1`

Expected: FAIL because package `finance` does not exist.

- [ ] **Step 3: Implement exact calculation**

```go
func microsToMinor(kwh, micros int64) int64 { return kwh * micros / 10_000 }
func Calculate(t domain.TariffVersion, c domain.BillingCycle) (domain.FinancialProjection, error) {
  if err := domain.ValidateTariffVersion(t); err != nil { return domain.FinancialProjection{}, err }
  if err := domain.ValidateBillingCycle(c); err != nil { return domain.FinancialProjection{}, err }
  billed := max(c.ActiveConsumptionKWh, int64(t.AvailabilityKWh))
  consumption := microsToMinor(billed, t.ConsumptionTEMicrosPerKWh+t.ConsumptionTUSDMicrosPerKWh+t.FlagMicrosPerKWh)
  compensation := microsToMinor(c.CreditsUsedKWh, t.CompensationTEMicrosPerKWh+t.CompensationTUSDMicrosPerKWh)
  return domain.FinancialProjection{ConsumptionMinor: consumption, CompensationMinor: compensation, CIPMinor: t.CIPMinor, TotalMinor: consumption-compensation+t.CIPMinor, WithoutSolarCompensationMinor: consumption+t.CIPMinor, IsEstimate: true}, nil
}
```

Expose component fields. Reject credit use above consumption for initial GD profile. Do not derive billed injection from inverter production.

- [ ] **Step 4: Verify pass**

Run: `rtk go test ./internal/finance ./internal/domain -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/finance internal/domain/finance.go
git commit -m "feat: calculate bill projections"
```

## Task 3: Transactional tariff, cycle, and credit-lot storage

**Files:**
- Create: `internal/storage/finance.go`
- Create: `internal/storage/finance_test.go`
- Modify: `internal/storage/db_test.go`

**Interfaces:**
- Consumes Tasks 1-2.
- Produces `CreateProposal`, `ApproveProposal`, `SaveCycle`, `LatestProjection`, and `ListCycles`.

- [ ] **Step 1: Write failing tests**

```go
func TestApproveProposalMakesImmutableVersion(t *testing.T) {
  proposal := storeProposal(t, repo, candidate("2026-06-24", "2027-06-23"))
  approved, err := repo.ApproveProposal(ctx, proposal.ID, "user-1")
  require.NoError(t, err)
  require.ErrorIs(t, repo.UpdateTariff(ctx, approved.ID, approved), storage.ErrImmutable)
}
func TestSaveCycleConsumesLotsByExpiry(t *testing.T) {
  seedLots(t, repo, lot("2028-01-01", 100), lot("2029-01-01", 100))
  _, _, err := repo.SaveCycle(ctx, cycleWithCredits(120), "user-1")
  require.NoError(t, err)
  require.Equal(t, []int64{0, 80}, remainingLots(t, repo))
}
```

- [ ] **Step 2: Verify failure**

Run: `rtk go test ./internal/storage -run 'TestApproveProposalMakesImmutableVersion|TestSaveCycleConsumesLotsByExpiry' -count=1`

Expected: FAIL because `FinanceRepository` does not exist.

- [ ] **Step 3: Implement transaction**

```go
type FinanceRepository struct { db *DB }
func (r *FinanceRepository) ApproveProposal(context.Context, int64, string) (domain.TariffVersion, error)
func (r *FinanceRepository) SaveCycle(context.Context, domain.BillingCycle, string) (domain.BillingCycle, domain.FinancialProjection, error)
func (r *FinanceRepository) LatestProjection(context.Context, time.Time) (domain.FinancialProjection, bool, error)
func (r *FinanceRepository) ListCycles(context.Context, int) ([]domain.BillingCycle, error)
```

In one transaction, load approved tariff for reading end, calculate projection, insert cycle and reconciliation, consume lots ordered `expires_at ASC, id ASC`, and add existing action audit rows. A positive reported balance difference becomes an `unknown` lot; a negative difference returns validation error. Never mutate an approved tariff.

- [ ] **Step 4: Verify pass**

Run: `rtk go test ./internal/storage -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/finance.go internal/storage/finance_test.go internal/storage/db_test.go
git commit -m "feat: persist finance cycles and credits"
```

## Task 4: Official tariff candidate adapter

**Files:**
- Create: `internal/tariffs/copel.go`
- Create: `internal/tariffs/copel_test.go`
- Create: `internal/tariffs/service.go`
- Create: `internal/tariffs/service_test.go`
- Create: `internal/tariffs/testdata/copel-group-b.html`
- Modify: `internal/app/app.go`

**Interfaces:**
- Consumes Task 3 proposal writer.
- Produces `tariffs.Service.Refresh(context.Context) (tariffs.Status, error)`.
- Never approves tariff data.

- [ ] **Step 1: Write failing source tests**

```go
func TestParseCopelFixtureCreatesCandidate(t *testing.T) {
  got, err := tariffs.ParseCopelGroupB(fixture(t, "copel-group-b.html"), tariffs.Selection{Class: "B1", Subclass: "residential"}, now)
  require.NoError(t, err)
  require.Equal(t, "COPEL", got.Candidate.Distributor)
}
func TestRefreshKeepsApprovedTariffWhenFetchFails(t *testing.T) {
  status, err := tariffs.NewService(failingFetcher{}, repo, clock).Refresh(ctx)
  require.Error(t, err)
  require.Equal(t, "stale", status.State)
}
```

- [ ] **Step 2: Verify failure**

Run: `rtk go test ./internal/tariffs -run 'TestParseCopelFixtureCreatesCandidate|TestRefreshKeepsApprovedTariffWhenFetchFails' -count=1`

Expected: FAIL because package `tariffs` does not exist.

- [ ] **Step 3: Implement parser and controlled fetch**

Define `Fetcher.Fetch(context.Context, string) ([]byte, error)`. Enforce HTTPS, fixed official Copel/ANEEL hosts, 64 KiB response cap, nonempty effective date range, and positive selected rates. Persist only valid candidates. On failure return `stale` when an approved schedule exists, otherwise `unavailable`. Wire daily refresh through existing jobs and report only state/time in component health.

- [ ] **Step 4: Verify pass**

Run: `rtk go test ./internal/tariffs ./internal/app -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tariffs internal/app/app.go
git commit -m "feat: add tariff proposals"
```

## Task 5: Authenticated finance API

**Files:**
- Create: `internal/api/finance.go`
- Create: `internal/api/finance_test.go`
- Modify: `internal/api/api.go`
- Modify: `internal/api/dto.go`
- Modify: `internal/app/app.go`
- Modify: `docs/api.md`

**Interfaces:**
- Consumes Task 3 repository via `FinanceStore`.
- Produces `GET /api/v1/finance/summary`, `GET/POST /api/v1/finance/cycles`, `GET /api/v1/finance/tariff-proposals`, and `POST /api/v1/finance/tariff-proposals/{id}/approve`.

- [ ] **Step 1: Write failing HTTP tests**

```go
func TestCreateCycleRequiresCSRFAndReturnsProjection(t *testing.T) {
  response := authedCSRFRequest(t, http.MethodPost, "/api/v1/finance/cycles", validCycleJSON)
  require.Equal(t, http.StatusCreated, response.Code)
  require.Contains(t, response.Body.String(), "\"isEstimate\":true")
}
func TestProposalApprovalIsAudited(t *testing.T) {
  response := authedCSRFRequest(t, http.MethodPost, "/api/v1/finance/tariff-proposals/1/approve", "{}")
  require.Equal(t, http.StatusCreated, response.Code)
  requireAudit(t, "tariff.approve")
}
```

- [ ] **Step 2: Verify failure**

Run: `rtk go test ./internal/api -run 'TestCreateCycleRequiresCSRFAndReturnsProjection|TestProposalApprovalIsAudited' -count=1`

Expected: FAIL with missing routes.

- [ ] **Step 3: Implement DTOs and handlers**

Use pointer DTO fields. Reject unknown, absent, negative, fractional kWh/money, and reversed reading dates with `422 invalid_finance_cycle`. Return RFC3339 dates and centavos as JSON integers. Require CSRF for both POST routes. All GET routes remain no-store through existing middleware.

```go
type FinanceStore interface {
  SaveCycle(context.Context, domain.BillingCycle, string) (domain.BillingCycle, domain.FinancialProjection, error)
  ListCycles(context.Context, int) ([]domain.BillingCycle, error)
  LatestProjection(context.Context, time.Time) (domain.FinancialProjection, bool, error)
  ListTariffProposals(context.Context) ([]domain.TariffProposal, error)
  ApproveProposal(context.Context, int64, string) (domain.TariffVersion, error)
}
```

- [ ] **Step 4: Verify pass**

Run: `rtk go test ./internal/api ./internal/app -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/finance.go internal/api/finance_test.go internal/api/api.go internal/api/dto.go internal/app/app.go docs/api.md
git commit -m "feat: expose finance API"
```

## Task 6: Finance UI, E2E, and documentation

**Files:**
- Create: `web/src/features/finance/FinancePage.tsx`
- Create: `web/src/features/finance/FinancePage.test.tsx`
- Create: `web/src/features/finance/BillingCycleForm.tsx`
- Create: `web/src/features/finance/BillingCycleForm.test.tsx`
- Create: `web/src/features/finance/TariffProposalCard.tsx`
- Modify: `web/src/api/types.ts`
- Modify: `web/src/api/client.ts`
- Modify: `web/src/api/queries.ts`
- Modify: `web/src/app/navigation.ts`
- Modify: `web/src/app/router.tsx`
- Create: `web/e2e/finance.spec.ts`
- Modify: `README.md`
- Modify: `docs/privacy.md`

**Interfaces:**
- Consumes Task 5 JSON. UI performs no money calculation.

- [ ] **Step 1: Write failing UI and E2E tests**

```tsx
it("labels a no-meter projection as estimate", async () => {
  render(<FinancePage />, { wrapper: testQueryClient() })
  expect(await screen.findByText("Projeção estimada sem medidor")).toBeVisible()
})
```

```ts
test("approves tariff and records a completed bill", async ({ page }) => {
  await login(page)
  await page.goto("/finance")
  await page.getByRole("button", { name: "Aprovar tarifa" }).click()
  await page.getByLabel("Consumo ativo (kWh)").fill("322")
  await page.getByRole("button", { name: "Salvar fatura" }).click()
  await expect(page.getByText("Real versus projetado")).toBeVisible()
})
```

- [ ] **Step 2: Verify failure**

Run: `rtk npm --prefix web test -- --run src/features/finance/FinancePage.test.tsx && rtk npm --prefix web run test:e2e -- finance.spec.ts`

Expected: FAIL because finance UI and E2E fixture do not exist.

- [ ] **Step 3: Implement route and accessible UI**

Render server-provided component rows, projected total, counterfactual, source URL/freshness, credit balance, and next expiry. Form contains exactly six bill fields plus reading start/end. Proposal card displays changed rates and requires explicit approval. If no approved tariff exists, show setup guidance instead of savings. Seed deterministic approved/proposed tariff data for E2E.

- [ ] **Step 4: Verify release checks**

Run: `rtk go test ./... && rtk npm --prefix web test -- --run && rtk npm --prefix web run lint && rtk npm --prefix web run build && rtk npm --prefix web run test:e2e -- finance.spec.ts`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/api web/src/app web/src/features/finance web/e2e/finance.spec.ts README.md docs/api.md docs/privacy.md
git commit -m "feat: add finance dashboard"
```

## Plan Self-Review

- Finance persistence, exact calculation, official candidate approval, six-field bill entry, credit lots, source failure, no-meter labeling, API, UI, privacy, and E2E all map to Tasks 1-6.
- QR discovery, SOFAR temperature profiles, grid-meter hardware, and weather stations are deliberately separate executable plans. Each has independent hardware and security validation.
- Type names are introduced before use: `TariffVersion`, `BillingCycle`, `FinancialProjection`, `FinanceRepository`, and `FinanceStore`.
- No task relies on bill PDF parsing or on automatic tariff approval.

