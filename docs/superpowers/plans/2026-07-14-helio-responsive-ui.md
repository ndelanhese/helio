# Helio Responsive Solar Editorial UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver authenticated, responsive Now, History, Insights, and Settings UI matching approved Solar editorial light/dark direction.

**Architecture:** TanStack Router owns file routes and auth redirects; TanStack Query owns REST cache; one typed API client and one SSE adapter feed feature components. Plain CSS tokens provide visual system without runtime CSS framework; Recharts renders historical charts from real API points.

**Tech Stack:** React 19.2.7, TypeScript 7.0.2, Vite 8.1.4, TanStack Router 1.170.18, TanStack Query 5.101.2, Recharts 3.9.2, Lucide React 1.24.0, Vitest 4.1.10, Testing Library 16.3.2, jest-dom 6.9.1, MSW 2.15.0, Playwright 1.61.1.

## Global Constraints

- UI language defaults to Brazilian Portuguese; code identifiers and API fields remain English.
- Routes: `/bootstrap`, `/login`, `/`, `/history`, `/insights`, `/settings`.
- Theme values: `system`, `light`, `dark`; manual choice persists under `helio.theme.v1`.
- Desktop content maximum width `1440px`; mobile breakpoint `760px`; minimum touch target `44px`.
- Light canvas `#F3F1E8`, surface `#FCFBF5`, text `#173B2D`, muted `#68776E`, accent `#2E7D55`, sun `#E7B84B`, danger `#B4493D`.
- Dark canvas `#101714`, surface `#18221D`, text `#EEF4EE`, muted `#9DABA2`, accent `#6EC495`, sun `#F0C75E`, danger `#E47B6D`.
- Body typography: system sans stack; editorial headings: `Georgia, 'Times New Roman', serif`; tabular metrics use `font-variant-numeric: tabular-nums`.
- No invented live/history values. Loading, stale, offline, unavailable, and explicit gap states render visibly.
- Charts expose text summaries/tables and do not rely on color alone.
- Auth and CSRF tokens live in memory; only session cookie persists, managed by browser.

---

## File Structure

- `web/src/api/client.ts`, `types.ts`, `queries.ts`, `live-events.ts` — typed server boundary.
- `web/src/app/router.tsx`, `query-client.ts`, `theme.tsx` — app providers.
- `web/src/routes/*` — TanStack file routes and data guards.
- `web/src/components/layout/*` — shell, nav, status, theme control.
- `web/src/features/auth/*`, `onboarding/*`, `live/*`, `history/*`, `insights/*`, `settings/*` — cohesive feature units.
- `web/src/styles/tokens.css`, `global.css`, `components.css` — theme and responsive layout.
- `web/src/test/*` — MSW server, render helper, deterministic fixtures.
- `web/e2e/*` — user journeys against fake backend mode.

### Task 1: Typed Client, Test Harness, Theme, and Protected Shell

**Files:**
- Modify: `web/package.json`, `web/package-lock.json`, `web/vite.config.ts`, `web/src/main.tsx`, `web/src/routes/__root.tsx`
- Create: `web/src/api/types.ts`, `client.ts`, `queries.ts`, `live-events.ts`
- Create: `web/src/app/query-client.ts`, `theme.tsx`, `router.tsx`
- Create: `web/src/components/layout/AppShell.tsx`, `PrimaryNav.tsx`, `ThemeToggle.tsx`, `ConnectionBadge.tsx`
- Create: `web/src/styles/tokens.css`, `global.css`, `components.css`
- Create: `web/src/test/setup.ts`, `server.ts`, `handlers.ts`, `render.tsx`, `fixtures.ts`
- Create: `web/src/components/layout/ThemeToggle.test.tsx`, `AppShell.test.tsx`

**Interfaces:**
- Consumes: core plan API error envelope and session/bootstrap routes.
- Produces: `api.request<T>()`; query keys; `ThemeProvider`; `useTheme`; protected `AppShell`.

- [ ] **Step 1: Write theme and navigation tests**

```tsx
it('follows system until user chooses a theme', async () => {
  matchMediaMock(false)
  renderApp(<ThemeToggle />)
  expect(document.documentElement.dataset.theme).toBe('light')
  await userEvent.click(screen.getByRole('button', { name: /tema/i }))
  await userEvent.click(screen.getByRole('menuitemradio', { name: 'Escuro' }))
  expect(document.documentElement.dataset.theme).toBe('dark')
  expect(localStorage.getItem('helio.theme.v1')).toBe('dark')
})

it('marks current destination and keeps mobile targets accessible', () => {
  renderRoute('/history')
  expect(screen.getByRole('link', { name: 'Histórico' })).toHaveAttribute('aria-current', 'page')
  expect(screen.getByRole('navigation', { name: 'Principal' })).toBeVisible()
})
```

- [ ] **Step 2: Confirm UI harness fails**

Run: `npm --prefix web test -- --run ThemeToggle AppShell`

Expected: FAIL because providers/components do not exist.

- [ ] **Step 3: Implement typed boundary and visual foundation**

`ApiClient` uses relative `/api/v1`, `credentials:'same-origin'`, JSON content checks, `AbortSignal`, and typed `ApiError(code,status,message)`. Mutations read current CSRF token from in-memory auth store and send `X-CSRF-Token`; 401 clears auth query and redirects to `/login`. Query defaults: stale time 15 seconds, retry network/5xx twice, never retry 4xx.

Theme provider initializes synchronously from localStorage or `matchMedia('(prefers-color-scheme: dark)')`, listens for system changes only in system mode, writes `data-theme`, and exposes three radio choices. App shell uses top wordmark/status/theme on desktop, left or top nav based width, and fixed bottom nav on mobile with Now/History/Insights/Settings labels in Portuguese.

Define CSS variables exactly from Global Constraints plus spacing `4,8,12,16,24,32,48,64`, radius `10,16,24`, border `color-mix(in srgb, var(--text) 12%, transparent)`, shadow only for floating mobile nav. Add `prefers-reduced-motion` rule disabling nonessential transitions.

- [ ] **Step 4: Verify unit, type, lint, and production build**

Run: `npm --prefix web test -- --run && npm --prefix web run typecheck && npm --prefix web run lint && npm --prefix web run build`

Expected: all commands exit 0; generated route tree has all current routes.

- [ ] **Step 5: Commit foundation UI**

```bash
git add web
git commit -m "feat: add typed Solar editorial app shell"
```

### Task 2: Bootstrap, Login, and Onboarding

**Files:**
- Create: `web/src/routes/bootstrap.tsx`, `login.tsx`
- Create: `web/src/features/auth/LoginForm.tsx`, `LoginForm.test.tsx`
- Create: `web/src/features/onboarding/OnboardingWizard.tsx`, `steps.tsx`, `schema.ts`, `OnboardingWizard.test.tsx`
- Modify: `web/src/routes/__root.tsx`, `web/src/api/queries.ts`

**Interfaces:**
- Consumes: bootstrap status/create, login, session, settings DTOs.
- Produces: closed bootstrap redirect; authenticated route guard; one atomic onboarding payload.

- [ ] **Step 1: Write guarded flow tests**

Tests cover: no-user state redirects every path to `/bootstrap`; existing-user unauthenticated state redirects to `/login` preserving safe same-origin `redirect`; bootstrap password mismatch blocks submit; logger serial remains masked except explicit reveal; seven × 610 displays derived `4,27 kWp`; active MPPT defaults to PV1 but stays editable; server 422 maps field errors; successful bootstrap stores CSRF only in memory and navigates `/`; sixth login failure displays rate-limit countdown; local HTTP warning is visible and says not to reuse important password.

- [ ] **Step 2: Confirm flow tests fail**

Run: `npm --prefix web test -- --run LoginForm OnboardingWizard`

Expected: FAIL for missing routes/features.

- [ ] **Step 3: Implement accessible forms and five-step onboarding**

Steps: `Conta`, `Logger`, `Painéis`, `Local e tarifa`, `Revisão`. Native labels, descriptions, inline errors, password manager autocomplete (`new-password`/`current-password`), Enter behavior, focus first invalid field, and progress `<ol>`. Logger validation uses private-IP UI hint but server remains authority. Panel total derives from integers. Location accepts coordinates and IANA timezone; tariff stored in minor units per kWh. Review redacts password and logger serial. Submit exactly once while pending; failure keeps inputs.

- [ ] **Step 4: Run auth/onboarding tests and axe checks**

Run: `npm --prefix web test -- --run LoginForm OnboardingWizard && npm --prefix web run typecheck`

Expected: PASS; no automated accessibility violation in both light/dark render tests.

- [ ] **Step 5: Commit onboarding**

```bash
git add web/src/routes web/src/features/auth web/src/features/onboarding web/src/api
git commit -m "feat: build secure bootstrap and onboarding UI"
```

### Task 3: Live Now Dashboard

**Files:**
- Create: `web/src/routes/index.tsx`
- Create: `web/src/features/live/NowPage.tsx`, `HeroPower.tsx`, `MetricStrip.tsx`, `PVFlow.tsx`, `HealthPanel.tsx`, `WeatherContext.tsx`, `useLiveTelemetry.ts`
- Create: `web/src/features/live/NowPage.test.tsx`, `useLiveTelemetry.test.ts`

**Interfaces:**
- Consumes: `GET /api/v1/live` and authenticated `/api/v1/live/events` SSE.
- Produces: resilient live query cache updated from `state`/`snapshot` events.

- [ ] **Step 1: Write live-state tests**

Fixture values verify locale formatting (`2,07 kW`, `267,1 V`, `8,00 A`, `59,97 Hz`), generated today/lifetime, status, freshness timestamp, and PV1 flow. Assert inactive PV2 is hidden behind `PV2 não utilizado` settings context and never marked fault. Advance fake timers past 30 seconds: page shows `Dados desatualizados`, preserves last measurement, and does not display zero. Simulate SSE error: badge changes to reconnecting while query refetches. Fault fixture exposes code and severity without replacing power metrics. Missing weather says `Previsão indisponível`.

- [ ] **Step 2: Confirm live tests fail**

Run: `npm --prefix web test -- --run NowPage useLiveTelemetry`

Expected: FAIL for missing live feature.

- [ ] **Step 3: Implement editorial composition and SSE adapter**

Layout: status eyebrow and last update; large serif current power; today energy/value secondary; compact metric strip; PV→inverter→grid flow; health/weather cards. Use `Intl.NumberFormat('pt-BR')`; W below 1000, kW otherwise; Wh/kWh analogous. SSE adapter constructs `EventSource('/api/v1/live/events')`, parses only known versioned event shapes, updates `['live']`, exponential reconnect is browser-owned, refetches on open, and closes on unmount/logout. `aria-live=polite` announces connectivity/fault changes, not every power update.

- [ ] **Step 4: Verify responsive snapshots and tests**

Run: `npm --prefix web test -- --run NowPage useLiveTelemetry && npm --prefix web run build`

Expected: PASS; no horizontal overflow at 375×812, 768×1024, 1440×900 in component harness.

- [ ] **Step 5: Commit Now page**

```bash
git add web/src/routes/index.tsx web/src/features/live
git commit -m "feat: add resilient live solar dashboard"
```

### Task 4: History, Comparison, Gaps, and CSV

**Files:**
- Create: `web/src/routes/history.tsx`
- Create: `web/src/features/history/HistoryPage.tsx`, `PeriodPicker.tsx`, `ProductionChart.tsx`, `SummaryCards.tsx`, `AccessibleHistoryTable.tsx`, `history-model.ts`
- Create: `web/src/features/history/HistoryPage.test.tsx`, `history-model.test.ts`

**Interfaces:**
- Consumes: history DTO and CSV endpoint.
- Produces: period/resolution query state encoded in URL; gap-aware chart series.

- [ ] **Step 1: Write history model and interaction tests**

Tests cover day/week/month/year ranges, previous-period comparison, no implicit zero between points over 90 seconds, explicit dashed gap region, peak and productive-hour summaries, coverage warning below 95%, locale/date timezone, empty state, API error retry, and CSV link preserving current from/to. URL query round-trip must restore period after reload/back.

- [ ] **Step 2: Confirm history tests fail**

Run: `npm --prefix web test -- --run HistoryPage history-model`

Expected: FAIL for missing model/page.

- [ ] **Step 3: Implement gap-aware chart and accessible equivalent**

`toChartSegments(points,maxGapMs)` returns separate arrays whenever separation exceeds threshold; never inserts synthetic zero. Recharts line/area renders each segment separately, current period solid green, prior period thin muted dashed, daylight background optional. Tooltip states exact time/value and `Sem dados` in gaps. Text summary reports energy, peak, productive duration, comparison percentage with neutral language when coverage differs. Collapsible table lists timestamp and power for screen reader/precise access. Query range constraints mirror server.

- [ ] **Step 4: Run tests and chart performance check**

Run: `npm --prefix web test -- --run HistoryPage history-model && npm --prefix web run build`

Expected: PASS; production chunk warning absent; 1,440 minute points render without browser console error.

- [ ] **Step 5: Commit History page**

```bash
git add web/src/routes/history.tsx web/src/features/history
git commit -m "feat: visualize honest gap-aware solar history"
```

### Task 5: Insights and Settings Surfaces

**Files:**
- Create: `web/src/routes/insights.tsx`, `settings.tsx`
- Create: `web/src/features/insights/InsightsPage.tsx`, `InsightCard.tsx`, `AlertList.tsx`, `InsightsPage.test.tsx`
- Create: `web/src/features/settings/SettingsPage.tsx`, `SystemForm.tsx`, `DataPanel.tsx`, `ConnectionPanel.tsx`, `SettingsPage.test.tsx`

**Interfaces:**
- Consumes: insights/alerts endpoints added by analysis plan; settings, component health, backup endpoints.
- Produces: explainable insight cards; editable validated configuration; data export controls.

- [ ] **Step 1: Write states and mutation tests**

Insights test confidence labels high/medium/low, evidence, active/resolved alerts, insufficient-history state, and zero claims about household use/import/export. Settings test edits panel count/wattage/MPPT, shows derived capacity, requires current password confirmation before logger identity changes, performs CSRF mutation, preserves form after 422, toggles theme, starts consistent backup download, and shows logger/weather separately from process/DB.

- [ ] **Step 2: Confirm route tests fail**

Run: `npm --prefix web test -- --run InsightsPage SettingsPage`

Expected: FAIL for missing pages.

- [ ] **Step 3: Implement honest evidence-first surfaces**

Insight card order: health state, conclusion, confidence pill, evidence bullets, observation window. Alert list distinguishes open/resolved and uses icon+text+color. Settings sections: System, Connection, Appearance, Data, About. Dangerous-looking logger edits are not inverter writes; copy says Helio remains read-only. Save invalidates settings/live/health queries only after success. Backup uses authenticated fetch→Blob, never places auth in URL. No remote access toggle or Telegram secret field appears in v0.1.

- [ ] **Step 4: Run full frontend suite**

Run: `npm --prefix web test -- --run && npm --prefix web run typecheck && npm --prefix web run lint && npm --prefix web run build`

Expected: all PASS.

- [ ] **Step 5: Commit Insights/Settings UI**

```bash
git add web/src/routes/insights.tsx web/src/routes/settings.tsx web/src/features/insights web/src/features/settings
git commit -m "feat: add explainable insights and settings UI"
```

### Task 6: Browser Journeys and Responsive Accessibility Gate

**Files:**
- Create: `web/playwright.config.ts`
- Create: `web/e2e/bootstrap.spec.ts`, `login.spec.ts`, `now.spec.ts`, `history.spec.ts`, `theme.spec.ts`, `settings.spec.ts`
- Create: `internal/fakeapp/fakeapp.go`
- Modify: `web/package.json`, `Makefile`

**Interfaces:**
- Consumes: full frontend and deterministic fake backend process.
- Produces: `make test-e2e` with desktop Chrome and mobile Safari-equivalent WebKit projects.

- [ ] **Step 1: Write Playwright journeys**

Each spec uses role/label selectors only. Cover first bootstrap, logout/login, SSE update without reload, logger outage/stale recovery, history gap/CSV, system→dark→light persistence across reload, settings validation, keyboard-only nav, and 375px viewport without clipped content. Screenshot only deterministic key states: Now light desktop, Now dark mobile, History with gap, Settings.

- [ ] **Step 2: Confirm fake app command is absent**

Run: `npm --prefix web run test:e2e`

Expected: FAIL because configured web server command does not exist.

- [ ] **Step 3: Implement deterministic fake backend mode**

`internal/fakeapp` serves production React assets and same API schemas, uses fixed clock/fixtures, accepts test-only scenario endpoint only when built/run as `go run ./internal/fakeapp`, never linked into `cmd/helio`. Playwright global setup resets scenario before each test. Disable animations in test CSS; freeze browser timezone `America/Sao_Paulo` and locale `pt-BR`.

- [ ] **Step 4: Run complete UI acceptance matrix**

Run: `npm --prefix web run test:e2e && npm --prefix web test -- --run && npm --prefix web run build`

Expected: all browser/unit/build checks PASS; screenshots contain no private identifiers.

- [ ] **Step 5: Commit browser acceptance**

```bash
git add web internal/fakeapp Makefile
git commit -m "test: cover responsive Helio user journeys"
```

## Plan Acceptance

Test at 375×812, 768×1024, 1440×900 in light/dark/system. Complete bootstrap, login, Now SSE recovery, History gap/CSV, Insights low-confidence state, settings edit, backup, and logout using keyboard. Run `npm --prefix web test -- --run`, typecheck, lint, build, and Playwright projects with zero failures and zero private identifiers in artifacts.
