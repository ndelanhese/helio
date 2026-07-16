# Task 6 — Finance UI, E2E, and documentation

## Delivered

- Added `/finance`, server-rendered projection breakdown, credit context, proposed-tariff approval, and bill reconciliation form.
- The browser sends only raw billing inputs; all monetary totals and rows are returned by the finance API.
- Added deterministic fakeapp tariff/cycle state and a browser flow that approves a tariff then saves a bill.
- Added privacy and README coverage; regenerated navigation snapshots for the new Finance destination.

## Pencil validation

- File: `/Users/ndelanhese/.pencil/documents/516be478-60ca-4904-bea1-c12f087808cd/pencil-welcome-desktop.pen`
- Screen node: `brtj3` (`Helio Finance — verified`)
- Used Web App and Code guides plus Forest Sage / Product Data Grid styling.
- `snapshot_layout(parentId=brtj3, problemsOnly=true)`: `No layout problems.`
- Screenshot reviewed after the revision; the previous clipped draft was replaced with a 1600px screen and complete visible content.

## Verification

- `rtk npm --prefix web test -- --run src/features/finance/FinancePage.test.tsx src/features/finance/BillingCycleForm.test.tsx` — 2 passed.
- `rtk go test ./...` — 488 passed in 23 packages.
- `rtk npm --prefix web run lint` — passed.
- `rtk npm --prefix web run build` — passed.
- `rtk npm --prefix web run test:e2e` — desktop 73 passed; mobile 61 + 3 + 4 passed (4 screenshots skipped as configured).
- Full `rtk npm --prefix web test -- --run` has one unrelated existing SettingsPage race failure: expected panel wattage `620`, received `610` in `preserves the edit during a server refetch racing a successful save and rebases afterward`.

## Concern

The API requires two readings plus five numeric account inputs (seven fields total). The brief calls for “six bill fields plus reading start/end”; no sixth server-accepted billing value exists, so the UI deliberately exposes the exact server contract rather than inventing a client-only field.
