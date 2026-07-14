# Helio Durable Core, Authentication, and API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn protocol shell into authenticated local service with SQLite history, ten-second collection, minute persistence, aggregates, onboarding, REST, CSV, and SSE.

**Architecture:** SQLite repository owns transactions and migrations; collector publishes immutable snapshots to bounded subscribers and consolidates minute rows. HTTP middleware enforces bootstrap state, server-side sessions, Strict cookies, origin-bound CSRF, and login limits before API handlers.

**Tech Stack:** Go 1.26.5, `modernc.org/sqlite` 1.53.0, `golang.org/x/crypto` 0.54.0, `github.com/go-chi/chi/v5` 5.3.1, SQLite WAL, REST JSON, SSE.

## Global Constraints

- Database path defaults to `/data/helio.db`; tests use `t.TempDir()` and never `:memory:` for WAL checks.
- SQLite pragmas: `journal_mode=WAL`, `foreign_keys=ON`, `busy_timeout=5000`, `synchronous=NORMAL`.
- Poll every `10s`; logger timeout `3s`; stale threshold `30s`; persisted telemetry interval `1m`.
- Minute retention defaults to `730d`; hourly/daily/monthly aggregates remain indefinitely.
- Passwords use Argon2id with memory `64 MiB`, iterations `3`, parallelism `2`, salt `16 bytes`, key `32 bytes`.
- Session IDs and CSRF tokens use `crypto/rand`, at least 256 bits, stored as SHA-256 digests; sessions expire after `30d` and idle after `24h`.
- Cookie: `helio_session`, `HttpOnly`, `SameSite=Strict`, `Path=/`; `Secure` follows `HELIO_SECURE_COOKIES`.
- Login limiter: five failures per normalized IP and username in 15 minutes; successful login clears matching bucket.
- Bootstrap route works only while `users` is empty. User creation and bootstrap settings commit atomically.
- Health endpoints remain unauthenticated; all other `/api/v1/*` routes require session except bootstrap-status/create and login.
- Authenticated mutations require `X-CSRF-Token` matching session plus same-origin `Origin`/`Host`; unauthenticated bootstrap/login require same-origin validation but cannot require a session token.

---

## File Structure

- `internal/storage/db.go`, `migrate.go`, `migrations/*.sql` — connection, pragmas, schema versions.
- `internal/storage/telemetry.go`, `settings.go`, `auth.go`, `alerts.go` — focused repositories.
- `internal/collector/collector.go`, `backoff.go`, `hub.go` — polling, recovery, latest state, subscribers.
- `internal/auth/password.go`, `session.go`, `limiter.go`, `middleware.go` — authentication boundary.
- `internal/api/*.go` — versioned DTOs and handlers.
- `internal/httpserver/router.go` — route composition only.
- `internal/config/validation.go` — logger/system settings validation.

### Task 1: SQLite Schema and Migration Runner

**Files:**
- Create: `internal/storage/db.go`, `db_test.go`, `migrate.go`, `migrate_test.go`
- Create: `internal/storage/migrations/0001_initial.sql`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: filesystem database path.
- Produces: `storage.Open(context.Context, string) (*storage.DB, error)`; `(*DB).Close() error`; `(*DB).Ready(context.Context) error`; `(*DB).Backup(context.Context, io.Writer) error`.

- [ ] **Step 1: Write migration and WAL tests**

```go
func TestOpenMigratesAndEnablesWAL(t *testing.T) {
    ctx := context.Background()
    db, err := Open(ctx, filepath.Join(t.TempDir(), "helio.db")); if err != nil { t.Fatal(err) }; defer db.Close()
    var mode string
    if err := db.sql.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil { t.Fatal(err) }
    if mode != "wal" { t.Fatalf("journal_mode=%q", mode) }
    for _, table := range []string{"users","sessions","settings","telemetry_minute","telemetry_events","weather_hourly","hourly_summary","daily_summary","monthly_summary","alerts","recommendations","action_audit"} {
        var got string
        err := db.sql.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&got)
        if err != nil || got != table { t.Fatalf("table %s: %v", table, err) }
    }
}

func TestMigrationIsIdempotent(t *testing.T) {
    path := filepath.Join(t.TempDir(), "helio.db")
    first, err := Open(context.Background(), path); if err != nil { t.Fatal(err) }; _ = first.Close()
    second, err := Open(context.Background(), path); if err != nil { t.Fatal(err) }; _ = second.Close()
}
```

- [ ] **Step 2: Confirm storage package fails**

Run: `go test ./internal/storage -run 'Test(Open|Migration)'`

Expected: FAIL with undefined `Open`.

- [ ] **Step 3: Implement database and exact schema**

Migration runner embeds `migrations/*.sql`, creates `schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`, sorts filenames, and applies each unseen file in one transaction. Reject database schema newer than binary.

`0001_initial.sql` defines:

```sql
CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE COLLATE NOCASE, password_hash TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE sessions (token_hash BLOB PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, csrf_hash BLOB NOT NULL, created_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, expires_at TEXT NOT NULL);
CREATE TABLE settings (key TEXT PRIMARY KEY, value_json TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE telemetry_minute (observed_at TEXT PRIMARY KEY, ac_power_w REAL NOT NULL, energy_today_wh REAL NOT NULL, energy_lifetime_wh REAL NOT NULL, pv1_voltage_v REAL NOT NULL, pv1_current_a REAL NOT NULL, pv1_power_w REAL NOT NULL, pv2_active INTEGER NOT NULL CHECK(pv2_active IN (0,1)), pv2_voltage_v REAL NOT NULL, pv2_current_a REAL NOT NULL, pv2_power_w REAL NOT NULL, grid_voltage_v REAL NOT NULL, grid_frequency_hz REAL NOT NULL, status TEXT NOT NULL, fault_codes_json TEXT NOT NULL);
CREATE TABLE telemetry_events (id INTEGER PRIMARY KEY AUTOINCREMENT, observed_at TEXT NOT NULL, kind TEXT NOT NULL, payload_json TEXT NOT NULL);
CREATE TABLE weather_hourly (hour TEXT PRIMARY KEY, cloud_cover_pct REAL, irradiance_wm2 REAL, source TEXT NOT NULL, fetched_at TEXT NOT NULL);
CREATE TABLE hourly_summary (hour TEXT PRIMARY KEY, energy_wh REAL NOT NULL, peak_power_w REAL NOT NULL, coverage_pct REAL NOT NULL);
CREATE TABLE daily_summary (day TEXT PRIMARY KEY, energy_wh REAL NOT NULL, peak_power_w REAL NOT NULL, productive_minutes INTEGER NOT NULL, coverage_pct REAL NOT NULL, expected_wh REAL, confidence REAL, value_minor INTEGER NOT NULL DEFAULT 0);
CREATE TABLE monthly_summary (month TEXT PRIMARY KEY, energy_wh REAL NOT NULL, peak_power_w REAL NOT NULL, productive_minutes INTEGER NOT NULL, coverage_pct REAL NOT NULL);
CREATE TABLE alerts (id TEXT PRIMARY KEY, rule TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('open','resolved')), severity TEXT NOT NULL, opened_at TEXT NOT NULL, resolved_at TEXT, evidence_json TEXT NOT NULL);
CREATE TABLE recommendations (id TEXT PRIMARY KEY, kind TEXT NOT NULL, created_at TEXT NOT NULL, dismissed_at TEXT, evidence_json TEXT NOT NULL);
CREATE TABLE action_audit (id INTEGER PRIMARY KEY AUTOINCREMENT, occurred_at TEXT NOT NULL, actor_user_id TEXT REFERENCES users(id), action TEXT NOT NULL, detail_json TEXT NOT NULL);
CREATE INDEX telemetry_minute_day ON telemetry_minute(observed_at);
CREATE INDEX telemetry_events_time ON telemetry_events(observed_at);
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE UNIQUE INDEX alerts_one_open_rule ON alerts(rule) WHERE state='open';
```

Configure pool `SetMaxOpenConns(1)`, DSN `_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)`, then execute WAL/synchronous pragmas. `Backup` uses `VACUUM INTO` to a temporary same-filesystem file, streams it, and removes it.

- [ ] **Step 4: Verify schema, race safety, and backup reopen**

Add test inserting one row, calling `Backup` into file, reopening backup, and reading row. Run: `go test -race ./internal/storage`.

Expected: PASS and backup database readable.

- [ ] **Step 5: Commit storage foundation**

```bash
git add go.mod go.sum internal/storage
git commit -m "feat: add durable SQLite schema and migrations"
```

### Task 2: Telemetry Repository, Aggregates, Retention

**Files:**
- Create: `internal/storage/telemetry.go`, `telemetry_test.go`
- Create: `internal/domain/history.go`

**Interfaces:**
- Consumes: `domain.TelemetrySnapshot` from protocol plan.
- Produces: `SaveMinute`, `SaveEvent`, `History`, `AggregateHour`, `AggregateDay`, `AggregateMonth`, `PruneBefore` methods and `domain.HistoryPoint`, `domain.HourlySummary`, `domain.DailySummary`, `domain.MonthlySummary`.

- [ ] **Step 1: Write consolidation, gap, aggregate, and retention tests**

Seed samples at `10:00`, `10:02`, and `10:03`; assert history returns those three timestamps only, daily coverage is `75%` for expected four observed minutes, trapezoidal energy integrates only adjacent samples whose separation is at most `90s`, and prune removes only rows older than cutoff. Seed two snapshots within one minute and assert upsert keeps later observation and maximum lifetime counter. Aggregate two hours into one day and two days across a month boundary; verify hourly, daily, and monthly upserts, local-time bucket boundaries, weighted coverage, and permanent summary rows after raw-minute pruning.

```go
type HistoryPoint struct { At time.Time `json:"at"`; PowerW float64 `json:"powerW"` }
type DailySummary struct { Day string; EnergyWh, PeakPowerW, CoveragePct float64; ProductiveMinutes int }
```

- [ ] **Step 2: Verify repository methods are absent**

Run: `go test ./internal/storage -run 'Test(Telemetry|Aggregate|Retention)'`

Expected: compile FAIL for missing methods.

- [ ] **Step 3: Implement parameterized queries and transaction rules**

`SaveMinute` floors timestamp in configured timezone then converts minute boundary to UTC, marshals fault codes, and uses `INSERT ... ON CONFLICT(observed_at) DO UPDATE`. `History` accepts UTC `[from,to)` and returns ordered points without generated rows. `AggregateHour` integrates ordered minute power using trapezoidal integration only when gap `<=90s`; `AggregateDay` rolls hourly rows into local day, calculates peak/productive minutes and coverage from daylight minutes; `AggregateMonth` rolls daily rows into local month with weighted coverage. All aggregate writes use deterministic upserts. `PruneBefore` deletes raw minute rows in batches of 10,000 until affected rows fall below batch size and never deletes summary tables.

- [ ] **Step 4: Run deterministic timezone tests**

Run: `TZ=UTC go test ./internal/storage && TZ=America/Sao_Paulo go test ./internal/storage`

Expected: both PASS with identical stored UTC instants.

- [ ] **Step 5: Commit telemetry persistence**

```bash
git add internal/domain/history.go internal/storage/telemetry.go internal/storage/telemetry_test.go
git commit -m "feat: persist telemetry history and aggregates"
```

### Task 3: Collector, Backoff, Freshness, and SSE Hub

**Files:**
- Create: `internal/collector/collector.go`, `collector_test.go`, `backoff.go`, `backoff_test.go`, `hub.go`, `hub_test.go`
- Modify: `internal/app/app.go`

**Interfaces:**
- Consumes: `type SnapshotReader interface { ReadSnapshot(context.Context) (domain.TelemetrySnapshot,error) }`; storage `SaveMinute`/`SaveEvent`.
- Produces: `collector.New(Config, SnapshotReader, Store, *Hub) *Collector`; `Run(context.Context) error`; `Latest() State`; `Hub.Subscribe() (<-chan Event, func())`.

- [ ] **Step 1: Write fake-clock collector tests**

Use injected `Clock`:

```go
type Clock interface { Now() time.Time; After(time.Duration) <-chan time.Time }
type State struct { Snapshot *domain.TelemetrySnapshot; LastSuccess time.Time; LastError string; Stale bool }
type Event struct { Kind string `json:"kind"`; Snapshot *domain.TelemetrySnapshot `json:"snapshot,omitempty"`; State State `json:"state"` }
```

Tests assert: first poll occurs immediately; successes publish every 10 seconds; only latest snapshot in each minute persists; status/fault changes call `SaveEvent` immediately; timeout sequence waits `1s,2s,4s` capped at `60s` with deterministic injected jitter; success resets backoff; stale becomes true 30 seconds after last success; cancellation exits without goroutine leak; slow subscriber receives newest state without blocking collector.

- [ ] **Step 2: Confirm collector package fails**

Run: `go test ./internal/collector`

Expected: compile FAIL for missing `Collector`, `Hub`, and `Backoff`.

- [ ] **Step 3: Implement one polling owner and bounded publication**

`Hub` stores subscriber channels of capacity one. `Publish` non-blockingly removes stale buffered event before sending newest. `Collector.Run` is only caller of reader; creates a 3-second child timeout; copies snapshots before publishing; compares status plus sorted fault codes for event persistence; tracks current minute; flushes previous minute before rollover and final pending minute during graceful shutdown; treats storage errors as component degradation but retains latest live snapshot; returns only context cancellation or unrecoverable configuration error.

- [ ] **Step 4: Run race and leak-focused tests**

Run: `go test -race -count=20 ./internal/collector`

Expected: PASS with stable goroutine count and no race.

- [ ] **Step 5: Commit collector**

```bash
git add internal/collector internal/app/app.go
git commit -m "feat: collect and publish resilient live telemetry"
```

### Task 4: Passwords, Sessions, CSRF, and Login Limiting

**Files:**
- Create: `internal/auth/password.go`, `password_test.go`, `session.go`, `session_test.go`, `limiter.go`, `limiter_test.go`, `middleware.go`, `middleware_test.go`
- Create: `internal/storage/auth.go`, `auth_test.go`

**Interfaces:**
- Consumes: users/sessions tables.
- Produces: `HashPassword`, `VerifyPassword`, `Manager.Bootstrap`, `Login`, `Authenticate`, `Logout`; middleware `RequireSession`, `RequireCSRF`, `BootstrapGate`.

- [ ] **Step 1: Write security contract tests**

Test password round-trip and wrong password; reject password under 12 characters or over 128 bytes; ensure stored hash starts `$argon2id$v=19$m=65536,t=3,p=2$`; bootstrap succeeds once then returns `ErrBootstrapClosed`; session lookup stores no raw token; cookie flags match constraints; missing/wrong CSRF returns 403; cross-origin mutation returns 403; unauthenticated private route returns 401 JSON; sixth failed login returns 429 with `Retry-After`; successful login resets limit; expired and idle sessions fail.

- [ ] **Step 2: Confirm auth tests fail**

Run: `go test ./internal/auth ./internal/storage -run 'Test(Password|Bootstrap|Session|CSRF|Limiter)'`

Expected: compile FAIL for missing auth APIs.

- [ ] **Step 3: Implement exact security primitives**

Argon2 encoded format is `$argon2id$v=19$m=65536,t=3,p=2$<base64-salt>$<base64-key>` using `base64.RawStdEncoding`; parser requires exactly six segments and bounded numeric values before allocation. Compare with `subtle.ConstantTimeCompare`. Generate 32 random token bytes, return base64url raw token once, store SHA-256 digest. `Authenticate` hashes cookie token, loads session and user in one query, checks absolute/idle expiry, updates `last_seen_at` at most once per five minutes. CSRF raw token is returned in login/bootstrap JSON and stored only as digest. Limiter keys HMAC-normalized IP plus lowercase username, prunes expired buckets, and has injected clock.

- [ ] **Step 4: Run auth tests under race detector**

Run: `go test -race ./internal/auth ./internal/storage`

Expected: PASS; repository query confirms raw token absent.

- [ ] **Step 5: Commit auth boundary**

```bash
git add go.mod go.sum internal/auth internal/storage/auth.go internal/storage/auth_test.go
git commit -m "feat: secure bootstrap and local sessions"
```

### Task 5: Onboarding Settings and Validation

**Files:**
- Create: `internal/domain/settings.go`
- Create: `internal/config/validation.go`, `validation_test.go`
- Create: `internal/storage/settings.go`, `settings_test.go`

**Interfaces:**
- Consumes: settings table.
- Produces: `domain.Settings`; `config.ValidateSettings`; `storage.GetSettings`, `PutSettings`.

- [ ] **Step 1: Write validation matrix**

```go
type Settings struct {
    LoggerHost, LoggerSerial string
    LoggerPort int
    ModbusSlave int
    PanelCount int
    PanelWattage int
    ActiveMPPT []int
    Latitude, Longitude float64
    Timezone, Currency string
    TariffMinorPerKWh int64
    RetentionDays int
}
```

Valid fixture: private IPv4, port 8899, decimal uint32 serial, slave 1, seven panels, 610 W, active MPPT `[1]`, valid Brazil coordinates, `America/Sao_Paulo`, `BRL`, nonnegative tariff, retention 730. Reject URL host, public IP without explicit override, duplicate/unknown MPPT, total capacity over inverter-safe 12 kWp, invalid IANA timezone, non-ISO currency, negative tariff, retention outside `30..3650`. Assert computed capacity equals `4270 W` and never accepts client-supplied computed total.

- [ ] **Step 2: Confirm validation failure**

Run: `go test ./internal/config ./internal/storage -run TestSettings`

Expected: compile FAIL for missing `Settings`/validation/repository.

- [ ] **Step 3: Implement normalized settings persistence**

Validate host with `net.ParseIP`, require private/loopback/link-local unless process override, normalize serial via `strconv.ParseUint(...,10,32)`, load timezone via `time.LoadLocation`, sort unique MPPT list, and derive `InstalledPowerW = PanelCount*PanelWattage` server-side. Persist entire validated object as versioned JSON under key `system` in one upsert. Unknown JSON fields fail decoding at API boundary.

- [ ] **Step 4: Verify round-trip and secrets scan**

Run: `go test ./internal/config ./internal/storage && git grep -E 'loggerHost|loggerSerial' -- internal/sofar/testdata internal/solarman/testdata`

Expected: tests PASS; grep returns no deployment values.

- [ ] **Step 5: Commit settings**

```bash
git add internal/domain/settings.go internal/config/validation.go internal/config/validation_test.go internal/storage/settings.go internal/storage/settings_test.go
git commit -m "feat: validate and persist onboarding settings"
```

### Task 6: Versioned REST, SSE, CSV, and Component Health

**Files:**
- Create: `internal/api/dto.go`, `bootstrap.go`, `auth.go`, `live.go`, `history.go`, `settings.go`, `sse.go`, `csv.go`, `api_test.go`
- Modify: `internal/httpserver/router.go`, `health.go`
- Modify: `internal/app/app.go`

**Interfaces:**
- Consumes: auth manager, settings/telemetry repositories, collector state/hub.
- Produces routes listed below with JSON content type and `{ "error": { "code", "message" } }` failures.

- [ ] **Step 1: Write black-box API tests**

Cover exact routes:

```text
GET  /api/v1/bootstrap/status
POST /api/v1/bootstrap
POST /api/v1/auth/login
POST /api/v1/auth/logout
GET  /api/v1/auth/session
GET  /api/v1/live
GET  /api/v1/live/events
GET  /api/v1/history?from=<RFC3339>&to=<RFC3339>&resolution=minute|hour|day|month
GET  /api/v1/history.csv?from=<RFC3339>&to=<RFC3339>
GET  /api/v1/settings
PUT  /api/v1/settings
GET  /api/v1/data/backup
GET  /health/components
```

Assert bootstrap closes atomically; private routes return 401 before auth; mutation without CSRF returns 403; malformed/unknown JSON returns 400; invalid range and over-366-day minute request return 422; CSV starts UTF-8 header `timestamp,power_w,energy_today_wh,status`; backup returns authenticated consistent SQLite bytes; SSE sends `retry: 5000`, initial `state` event, snapshot event, 15-second comment heartbeat, and stops on disconnect; component health returns 200 even when logger is offline; ready returns 503 only when DB ping fails.

- [ ] **Step 2: Confirm route tests fail**

Run: `go test ./internal/api ./internal/httpserver`

Expected: FAIL with 404 or missing package.

- [ ] **Step 3: Implement DTO boundary and route composition**

Use `chi.Router`; request bodies limited to 64 KiB; `json.Decoder.DisallowUnknownFields`; all timestamps RFC3339 UTC; responses include `Cache-Control: no-store` for auth/live and `X-Request-ID`; CSV uses `encoding/csv` and `Content-Disposition: attachment; filename="helio-history.csv"`; backup streams `storage.Backup` as `application/vnd.sqlite3` with filename `helio-backup-YYYYMMDD-HHMMSS.db` and audit metadata only. SSE uses `http.Flusher`, never buffers unbounded history, and subscribes only after authentication. Bootstrap transaction creates admin and validated settings together, then starts/reconfigures collector after commit. Settings update swaps reader configuration only after validation/persistence success and records action audit without logger serial.

- [ ] **Step 4: Run API integration suite**

Run: `go test -race ./internal/api ./internal/httpserver ./internal/app`

Expected: PASS; no goroutine remains after SSE disconnect.

- [ ] **Step 5: Commit API**

```bash
git add internal/api internal/httpserver internal/app
git commit -m "feat: expose authenticated REST and SSE API"
```

### Task 7: Scheduled Aggregation, Retention, and Graceful Shutdown

**Files:**
- Create: `internal/jobs/runner.go`, `runner_test.go`
- Modify: `internal/app/app.go`
- Modify: `internal/httpserver/health.go`

**Interfaces:**
- Consumes: telemetry repository aggregation/prune APIs and settings.
- Produces: `jobs.Runner.Run(context.Context) error`; complete process readiness/component states.

- [ ] **Step 1: Write schedule and shutdown tests**

Injected clock tests assert: daily aggregate runs after local midnight plus five minutes; retention runs once daily; missed run executes once on startup; duplicate day upserts deterministically; cancellation waits for current transaction but caps shutdown at ten seconds; collector/logger failure leaves `/health/ready` 200 and `/health/components` degraded.

- [ ] **Step 2: Confirm jobs tests fail**

Run: `go test ./internal/jobs ./internal/app`

Expected: compile FAIL for missing runner.

- [ ] **Step 3: Implement coordinated lifecycle**

`app.Run` uses one cancellation context and `sync.WaitGroup` for collector, jobs, and HTTP server. Startup order: open/migrate DB, build repositories, start HTTP, then collector/jobs only when settings exist. Shutdown order: stop accepting HTTP, cancel SSE/collector/jobs, flush collector minute, wait, close DB. Store component status timestamps and error classes; never place host, serial, raw frames, password, session, or CSRF values in logs/health.

- [ ] **Step 4: Run complete backend suite**

Run: `go test -race ./... && go vet ./...`

Expected: PASS with clean race/vet output.

- [ ] **Step 5: Commit durable core**

```bash
git add internal/jobs internal/app internal/httpserver/health.go
git commit -m "feat: schedule aggregates and graceful lifecycle"
```

## Plan Acceptance

Run `go test -race ./...`, `go vet ./...`, start server against temporary `/data`, complete bootstrap through API, log in, connect SSE, inject fake snapshots, query minute history/CSV, restart process, and confirm session/settings/history survive. Confirm logger-down response leaves `/health/ready` at 200 and marks only logger component degraded.
