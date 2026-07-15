# UI Task 5 Report — Explainable Insights and Settings

## Outcome

Implemented the authenticated `/settings` surface and refined Insights confidence coverage against the actual API contracts. Settings uses the existing Solar editorial system across five ordered sections: System, Connection, Appearance, Data, and About. The page derives installed capacity from editable panel count and wattage, validates the full settings document, shows process/database/logger/weather independently, preserves failed form state, supports the existing three-way theme provider, and downloads a consistent authenticated database backup through a short-lived Blob URL.

Logger host, serial, port, or Modbus identity changes require the current password. Because the settings PUT deliberately rejects unknown fields, the UI verifies that password through the existing login endpoint, stores the returned rotated CSRF token only in memory, and then sends the settings-only PUT. Passwords and auth tokens are never included in settings, URLs, local storage, or backup URLs. Helio copy remains explicit that the product reads the logger/inverter and exposes no inverter-write control.

## RED evidence

1. `npm --prefix web test -- --run InsightsPage SettingsPage`
   - Insights passed, while Settings failed to resolve the intentionally absent `SettingsPage` module.
2. `npm --prefix web test -- --run SettingsPage -t "associates a strict retention error"`
   - Failed because the Data retention error was not rendered or associated with its input.

## GREEN evidence

- Focused Insights/Settings: 20 tests passed, covering low/medium/high confidence, evidence/window/trends, open/resolved alerts, insufficient history, loading/retry, 401/409/422/network outcomes, double submit, CSRF, no secret persistence, health separation, theme selection, backup success/failure, and prohibited controls/claims.
- Full frontend: `npm --prefix web test -- --run` — 22 files, 156 tests passed.
- TypeScript: `npm --prefix web run typecheck` — passed.
- Biome: `npm --prefix web run lint` — passed with no warnings.
- Production: `npm --prefix web run build` — passed, generated a lazy Settings chunk, and rebuilt `internal/webui/dist`.

## Responsive browser smoke

Real Chromium with intercepted responses matching the backend DTOs, locale `pt-BR`, and timezone `America/Sao_Paulo` covered 375×812, 768×1024, and 1440×900 in both light and dark themes. Every run rendered all five settings sections with zero horizontal overflow, every visible form/nav/button target measured at least 44px, and there were zero browser console warnings/errors or page errors.

## Contract notes

- The displayed `4,27 kWp` is always derived from the current `7 × 610 W` form values; `installedPowerW` is never submitted as saved truth.
- Settings success invalidates only settings, live, and component-health query keys, after the mutation succeeds.
- A 422 maps safe server validation to its field and retains every entered value; conflict/network/server failures retain the complete form and restore the save action.
- Backup uses same-origin authenticated fetch, response Blob, server filename when present, a temporary object URL, and immediate URL revocation after starting the browser download.
- Insights retains health → conclusion → confidence → evidence → observation-window order, includes trend coverage, distinguishes active alerts from recoveries, and makes no unsupported household or grid-flow claim.
- Expansion controls and secrets remain absent from the v0.1 Settings surface.

## Concerns

None blocking. Task 6 browser journeys and fake backend remain intentionally out of scope; this task performed a direct Chromium responsive smoke only.

## Review remediation — 2026-07-15

- Nonqualifying insight days now use the explicit conclusion `Telemetria insuficiente para comparar` and never render a below-reference or within-reference conclusion. The card retains confidence and evidence as explanations of the telemetry limitation.
- Current-password confirmation is the only request allowed to suppress the global 401 handler. A rejected password remains inline on `/settings`, preserving dirty values and the password in component memory; settings PUT and backup 401 responses still use the global unauthorized path.
- Settings now synchronizes refreshed server documents without identity-key remounts. Pristine forms adopt new values, dirty forms preserve edits and expose accessible Load/Keep conflict choices, and a successful PUT rebases both the editor and query cache. Tests cover nonidentity and identity updates plus a refresh racing an in-flight save.
- Settings currency validation imports the shared `HELIO_ISO_4217_SET`. The Settings model accepts every shared code and rejects `ZZZ`; the existing currency drift test compares that shared list exactly with `internal/config/validation.go`.
- Retention is inside the single settings form, so Enter from the retention field submits without nested forms.
- `web/e2e/task5-responsive.spec.ts` is reproducible Playwright coverage for Settings and Insights at 375×812, 768×1024, and 1440×900 in light and dark. It checks headings through roles, form controls through labels, horizontal overflow, 44px controls, nonqualifying copy, keyboard submission with wrong-password preservation, and the deterministic backup filename.

### Review RED/GREEN evidence

- RED: the nonqualifying insight test observed a below-reference conclusion; the credential-confirmation client test invoked the global 401 handler; four settings refresh tests observed stale/remounted state; `ZZZ` passed Settings validation; and the retention input had no owning form.
- GREEN focused: `npm --prefix web test -- --run src/features/settings src/features/insights/InsightsPage.test.tsx src/api/client.test.ts --reporter=dot` — 4 files, 34 tests passed.
- GREEN browser: `npm --prefix web run test:e2e -- task5-responsive.spec.ts` — 8 tests passed.

### Final verification

- `npm --prefix web test -- --run --reporter=dot` — 23 files, 166 tests passed. The suite still emits the pre-existing unhandled `/health/components` MSW diagnostics from `NowPage.test.tsx`; there were no test failures.
- `npm --prefix web run typecheck` — passed.
- `npm --prefix web run lint` — 90 files checked with no warnings or errors.
- `npm --prefix web run build` — passed and rebuilt the embedded `internal/webui/dist` assets.
- `npm --prefix web run test:e2e` — all 11 Chromium tests passed, including the existing live responsive tests and the 8 committed Task 5 scenarios.
- `git diff --check` — passed.
