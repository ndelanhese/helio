# Helio Weather, Insights, and Internal Alerts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add cached weather context, learned production expectations, evidence/confidence scores, and conservative internal alerts without inventing consumption data.

**Architecture:** Provider-neutral weather client caches hourly observations; analysis operates on pure typed inputs and emits explainable results. Alert engine requires daylight, sufficient weather/history coverage, persistence windows, and hysteresis before repository state transitions.

**Tech Stack:** Go 1.26.5 standard HTTP/time/math, Open-Meteo forecast API adapter, SQLite repositories from core plan, React surfaces from UI plan.

## Global Constraints

- Core collection remains functional with no network weather response.
- Weather timeout `5s`; refresh every `60m`; successful cache usable for `6h`; stale cache visibly lowers confidence.
- Daylight uses configured latitude/longitude/timezone and calculated sunrise/sunset; all model buckets use local solar day.
- Baseline starts only after seven days with daily coverage at least `80%`; high confidence requires 30 qualifying days.
- Installed capacity derives from settings; default reference installation is seven 610 W panels = 4.27 kWp.
- PV2 configured inactive contributes neither fault nor expected capacity penalty.
- Zero-generation alert requires sun elevation above 10°, irradiance at least 200 W/m², 20 continuous minutes, no fresh inverter fault, and telemetry coverage at least 80%.
- Underproduction alert requires actual below 65% expected for three qualifying days; resolves after two qualifying days at or above 80%.
- Grid alert thresholds default voltage outside `202..240 V` for 5 minutes or frequency outside `59.5..60.5 Hz` for 2 minutes; values remain configurable.
- No household consumption, grid import/export, self-consumption, savings, avoided cost, or battery claims.

---

## File Structure

- `internal/weather/provider.go`, `openmeteo.go`, `cache.go` — provider contract, adapter, cache behavior.
- `internal/solar/position.go` — deterministic sunrise/sunset/elevation.
- `internal/analysis/model.go`, `baseline.go`, `confidence.go`, `insights.go` — pure analysis.
- `internal/alerts/engine.go`, `rules.go`, `state.go` — conservative rules and transitions.
- `internal/storage/weather.go`, `analysis.go`, `alerts.go` — persistence queries.
- `internal/api/insights.go`, `alerts.go` — authenticated read endpoints.

### Task 1: Solar Position and Cached Weather Provider

**Files:**
- Create: `internal/solar/position.go`, `position_test.go`
- Create: `internal/weather/provider.go`, `openmeteo.go`, `openmeteo_test.go`, `cache.go`, `cache_test.go`
- Create: `internal/storage/weather.go`, `weather_test.go`

**Interfaces:**
- Produces: `solar.Daylight(date,lat,lon,location) (sunrise,sunset time.Time,error)`; `solar.Elevation(time,lat,lon) float64`; `weather.Provider.Hourly(context.Context, Request) ([]Hour,error)`; `weather.Service.Get(context.Context, Request) Result`.

- [ ] **Step 1: Write deterministic astronomy/provider/cache tests**

Use authoritative fixed test vectors for equinox/noon, Southern Hemisphere summer/winter, and polar no-rise/no-set errors. HTTP test server asserts Open-Meteo request includes latitude, longitude, `hourly=cloud_cover,shortwave_radiation`, timezone `UTC`, and bounded dates. JSON test maps timestamps, cloud percent, irradiance. Cache tests assert fresh DB hit avoids HTTP, expired cache refreshes, failed refresh returns stale result with `Stale=true`, and empty failure returns unavailable without erroring caller.

- [ ] **Step 2: Confirm packages fail**

Run: `go test ./internal/solar ./internal/weather ./internal/storage -run 'Test(Daylight|OpenMeteo|WeatherCache)'`

Expected: compile FAIL for missing APIs.

- [ ] **Step 3: Implement pure solar math and provider boundary**

Solar implementation uses NOAA equations with radians normalized and explicit polar error. Open-Meteo adapter uses injected base URL/client/clock, status/content-type/body-size checks, strict JSON decoding, 5-second context, and no location logging. `Service.Get` reads DB first, returns fresh cache under 60 minutes, requests provider otherwise, upserts normalized UTC hours transactionally, and falls back to cache under six hours tagged stale. `Result` contains `Hours`, `Source`, `FetchedAt`, `Stale`, `Available`, and `ErrorClass`; raw error text stays server log only.

- [ ] **Step 4: Run time-zone and outage tests**

Run: `TZ=UTC go test ./internal/solar ./internal/weather ./internal/storage && TZ=America/Sao_Paulo go test ./internal/solar ./internal/weather ./internal/storage`

Expected: PASS both times.

- [ ] **Step 5: Commit weather foundation**

```bash
git add internal/solar internal/weather internal/storage/weather.go internal/storage/weather_test.go
git commit -m "feat: cache local solar weather context"
```

### Task 2: Learned Baseline, Expectation, and Confidence

**Files:**
- Create: `internal/domain/analysis.go`
- Create: `internal/analysis/model.go`, `baseline.go`, `confidence.go`, `model_test.go`, `baseline_test.go`
- Create: `internal/storage/analysis.go`, `analysis_test.go`

**Interfaces:**
- Consumes: daily summaries, installed watts, daylight, weather hours.
- Produces: `analysis.BuildBaseline([]TrainingDay) Baseline`; `analysis.Evaluate(Input) Result`; persisted daily expectation/confidence.

- [ ] **Step 1: Write table-driven model tests**

Define:

```go
type Confidence string
const (ConfidenceLow Confidence = "low"; ConfidenceMedium Confidence = "medium"; ConfidenceHigh Confidence = "high")
type Result struct { ExpectedWh, ActualWh, Ratio float64; Confidence Confidence; Evidence []Evidence; Qualifying bool }
type Evidence struct { Code string `json:"code"`; Label string `json:"label"`; Value float64 `json:"value"`; Unit string `json:"unit"` }
```

Tests assert fewer than seven qualifying days returns low/non-qualifying; 7–29 returns medium; 30+ high only with fresh weather and >=90% coverage; cloudy irradiance lowers expected energy; stale weather reduces confidence one level; missing weather uses season/hour baseline and labels evidence; gap-heavy day does not train; outlier clipping uses median absolute deviation; inactive PV2 does not affect installed capacity.

- [ ] **Step 2: Confirm analysis tests fail**

Run: `go test ./internal/analysis ./internal/storage -run 'Test(Baseline|Evaluate)'`

Expected: compile FAIL for missing model.

- [ ] **Step 3: Implement bounded explainable model**

Baseline groups qualifying history by local month and daylight-hour bucket; normalizes power by installed watts; clips values beyond 3 median absolute deviations; stores median normalized curve and sample count. Expected Wh integrates baseline across observed daylight buckets then multiplies by weather irradiance ratio clamped `0.25..1.15`; without weather, uses baseline only. Clamp final expected daily energy to `0..installedW*daylightHours`. Confidence combines qualifying history count, telemetry coverage, weather freshness/coverage, and model bucket coverage. Every reduction appends evidence code; results never say system is faulty by model alone.

- [ ] **Step 4: Run deterministic property checks**

Add fuzz/property tests: expected never negative/NaN/infinite; increasing installed watts cannot reduce expected Wh with same normalized baseline; lower irradiance cannot increase expected; missing days never become zero samples. Run: `go test -race ./internal/analysis ./internal/storage`.

Expected: PASS.

- [ ] **Step 5: Commit model**

```bash
git add internal/domain/analysis.go internal/analysis internal/storage/analysis.go internal/storage/analysis_test.go
git commit -m "feat: learn explainable production expectations"
```

### Task 3: Stateful Conservative Alert Rules

**Files:**
- Create: `internal/alerts/rules.go`, `rules_test.go`, `engine.go`, `engine_test.go`, `state.go`
- Create: `internal/storage/alerts.go`, `alerts_test.go`

**Interfaces:**
- Consumes: collector state, solar elevation, weather, analysis result, settings.
- Produces: `alerts.Engine.Evaluate(context.Context, Input) ([]Transition,error)`; idempotent open/resolve transitions.

- [ ] **Step 1: Write rule boundary and hysteresis tests**

Cover logger offline after three consecutive failed polls; stale telemetry at threshold; immediate inverter fault and resolution after fault clears for two fresh polls; zero generation exactly below/above elevation/irradiance/coverage/window boundaries; underproduction after three qualifying days and resolution after two recovered days; voltage/frequency duration thresholds; nighttime and inactive PV2 never alert; weather unavailable suppresses sun-dependent alerts; repeated evaluation creates no duplicate open alert; restart resumes pending duration state from stored evidence timestamps.

- [ ] **Step 2: Confirm alert tests fail**

Run: `go test ./internal/alerts ./internal/storage -run 'Test(Alert|Rule|Transition)'`

Expected: compile FAIL for missing engine/repository.

- [ ] **Step 3: Implement pure rules plus transactional transitions**

Rules return `Decision{Rule,ShouldOpen,ShouldResolve,Severity,Evidence,Reason}` without persistence. Engine loads current state, evaluates in fixed order, and transactionally inserts open, resolves existing, or updates pending evidence. Use stable rules: `logger_offline`, `telemetry_stale`, `inverter_fault`, `zero_sunny_generation`, `persistent_underproduction`, `grid_voltage`, `grid_frequency`. Store numeric evidence and timestamps, never raw frames or identifiers. Opening/resolution writes `action_audit`; process restarts preserve state.

- [ ] **Step 4: Run repeated/race tests**

Run: `go test -race -count=20 ./internal/alerts ./internal/storage`

Expected: PASS; one open row per rule.

- [ ] **Step 5: Commit alerts**

```bash
git add internal/alerts internal/storage/alerts.go internal/storage/alerts_test.go
git commit -m "feat: detect conservative internal solar alerts"
```

### Task 4: Jobs, API, and UI Integration

**Files:**
- Modify: `internal/jobs/runner.go`, `internal/app/app.go`, `internal/httpserver/health.go`
- Create: `internal/api/insights.go`, `alerts.go`, `insights_test.go`
- Modify: `web/src/api/types.ts`, `queries.ts`
- Modify: `web/src/features/insights/InsightsPage.tsx`, `InsightCard.tsx`, `AlertList.tsx`, tests
- Modify: `web/src/features/live/WeatherContext.tsx`, `HealthPanel.tsx`, tests

**Interfaces:**
- Produces: `GET /api/v1/insights?day=YYYY-MM-DD`; `GET /api/v1/alerts?state=open|resolved`; weather component health.

- [ ] **Step 1: Write integration contract tests**

API test asserts response includes actual/expected/ratio, confidence enum, evidence array, trend summaries, tariff-derived production value labeled estimate, and no prohibited consumption fields. UI test renders low confidence, weather unavailable, underproduction evidence, active/resolved alerts, and recovery. Component health reports `available|stale|unavailable` while readiness stays ready.

- [ ] **Step 2: Confirm integration tests fail**

Run: `go test ./internal/api ./internal/jobs && npm --prefix web test -- --run InsightsPage NowPage`

Expected: backend 404/missing handlers and frontend missing DTO fields.

- [ ] **Step 3: Wire schedules and authenticated DTOs**

Weather refresh starts after settings load and hourly thereafter; daily analysis runs after aggregation; fast alert rules run on each collector event; daily rule runs after analysis. API exposes only stable evidence labels and values. Production value equals `actualWh/1000 * tariffMinorPerKWh`, rounded to integer minor units; copy says `valor estimado da energia gerada`, never savings. Live page shows weather age/confidence. Insights page shows insufficient-data onboarding until seven qualifying days.

- [ ] **Step 4: Run complete cross-stack checks**

Run: `go test -race ./... && npm --prefix web test -- --run && npm --prefix web run build`

Expected: PASS; simulated weather outage leaves live/history working.

- [ ] **Step 5: Commit integration**

```bash
git add internal web/src
git commit -m "feat: surface weather-aware insights and alerts"
```

## Plan Acceptance

Replay deterministic 35-day dataset containing clear, cloudy, missing, outage, and underproduction days. Verify confidence progression low→medium→high, no alert from a single low sample, three-day underproduction transition, two-day recovery, PV2 silence, weather-outage degradation, and no prohibited household-consumption field/copy through `git grep -Ei 'self-consumption|autoconsumo|grid import|grid export|economia|poupança' internal web/src`.
