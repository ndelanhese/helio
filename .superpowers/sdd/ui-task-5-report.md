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
