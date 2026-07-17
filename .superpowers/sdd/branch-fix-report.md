# Finance branch review fixes

## Resolved findings

- Finance repository billing dates now follow the persisted configured timezone on startup, bootstrap reconfiguration, and settings updates.
- Billing form submits `YYYY-MM-DD` civil dates. The API accepts those dates in the configured billing timezone while continuing to accept RFC3339 for compatibility.
- Persisted `flagChargeMinor` is now included as `FlagMinor`, the projected total, the no-solar total, and the server display rows.
- Tariff refresh now occurs before optional analysis, so analysis failures do not suppress the daily source refresh or its health status.

## Verification

- `rtk go test ./... -count=1` — 491 passing tests.
- `rtk npm test -- --run` (in `web/`) — 172 passing tests.
- `rtk npm run build` (in `web/`) — passed.
- `rtk git diff --check` — passed.

## Test-first evidence

New tests initially failed for persisted flag totals, tariff refresh after analysis failure, and civil-date browser submission. The implementation was then added and the focused and full suites passed.
