# Helio Local Solar Monitor — Design

Date: 2026-07-14

## Purpose

Helio replaces the poor local monitoring experience offered by the Solarman app. It reads a SOFAR inverter directly over the home network, stores an independent history, presents a polished responsive dashboard, detects abnormal production, and creates actionable recommendations.

Initial hardware:

- Inverter: SOFAR 6KTLM-G3, 6 kW, Modbus slave ID 1
- Solarman logger: serial LOGGER_SERIAL at `LOGGER_IP:8899`
- Array: seven 610 W panels, 4.27 kWp total
- Wiring: all panels on PV1; PV2 intentionally unused
- Available measurements: solar generation and inverter/grid telemetry; no household consumption meter

## Product Principles

- Local-first: core monitoring works without Solarman Cloud.
- Read-only by default: MVP contains no Modbus write endpoint.
- Honest analysis: gaps, stale data, and low-confidence weather estimates stay visible.
- Portable deployment: development starts on a Mac, final runtime uses one Docker container and one persistent volume.
- Independent from Home Assistant, while exposing an API suitable for later integration.
- Responsive first: same web app serves Mac and phone on the local Wi-Fi network.

## User Experience

### Visual Direction

Use the approved “Solar editorial” direction:

- Calm green palette, strong hierarchy, compact live metrics, restrained charts
- Light and dark themes
- Default follows operating-system preference
- Visible manual theme toggle
- Manual preference persists per browser
- Navigation: Now, History, Insights, Settings

### Authentication

Every screen requires login. First-run onboarding creates the administrator password. Passwords use Argon2id hashing. Sessions use server-side records and `HttpOnly`, `SameSite=Strict` cookies. Login is rate-limited; state-changing requests require CSRF protection.

Local HTTP authentication does not encrypt traffic. Users must not reuse an important password. Remote access is out of MVP scope and must later use HTTPS or Tailscale.

### Onboarding

First run collects and validates:

- Logger IP, port, serial, and Modbus slave ID
- Panel count, wattage, total installed kWp, and active MPPT inputs
- Approximate location for solar and weather calculations
- Electricity tariff for estimated financial value
- Theme and data-retention preferences

Defaults for this installation prefill known logger, inverter, array, and PV1/PV2 values, but every value remains editable.

## MVP Scope

### Now

- Current AC output power
- Generation today and lifetime generation
- PV1 voltage, current, and power
- Grid voltage and frequency
- Inverter status and active faults
- Logger connectivity, data freshness, and last successful update
- Current weather and expected-production confidence
- PV2 hidden or marked inactive, never treated as a fault

### History

- Intraday production curve
- Weekly, monthly, and yearly aggregates
- Comparison with previous periods
- Peak production and productive-hour summaries
- CSV export
- Explicit gaps when the collector was stopped or unreachable

### Insights

- Production below weather-adjusted expectation
- Comparison against a learned baseline for the 4.27 kWp installation
- Daily peak and productive-hour trends
- Estimated financial value based on configurable tariff
- Overall health state with evidence and confidence
- No household-consumption, import, export, or self-consumption claims without a future meter

### Internal Alerts

- Logger offline
- Stale telemetry
- Inverter-reported fault
- Zero generation during a sufficiently sunny daytime window
- Persistent production below the weather-adjusted baseline
- Out-of-range grid voltage or frequency

Rules account for sunrise, cloud cover, minimum observation windows, and confidence. A single low sample does not trigger an alert.

## Expansion Scope

- Telegram delivery, quiet hours, and daily summaries
- Remote access through Tailscale or an authenticated HTTPS reverse proxy
- Home Assistant integration through documented REST, webhook, or optional MQTT interfaces
- Modbus controls only through a strict register allowlist, displayed diff, strong confirmation, read-back verification, and immutable audit record
- Additional inverters and electricity meters after single-inverter behavior is stable

Expansion items must not add runtime dependencies to MVP architecture.

## Technical Architecture

### Runtime

One Go process performs all runtime duties:

- Serves REST API
- Serves Server-Sent Events for live updates
- Serves embedded React static assets
- Polls and decodes Solarman V5/Modbus telemetry
- Persists samples and aggregates
- Evaluates alerts and insights
- Runs scheduled weather and aggregation jobs

### Frontend

- React
- TypeScript
- Vite
- TanStack Router
- TanStack Query for server-state fetching, caching, invalidation, and mutation lifecycle
- Small local UI state kept outside TanStack Query
- Responsive PWA behavior without requiring app-store packaging

Next.js is excluded because Helio needs neither SEO nor server-side rendering. Static Vite output lets the Go binary serve the entire product without a Node runtime.

### Backend Boundaries

- `solarman`: transport, V5 framing, Modbus serialization, connection lifecycle
- `sofar`: SOFAR register definitions, scaling, validation, and typed snapshots
- `collector`: polling schedule, retries, freshness, and snapshot publication
- `storage`: SQLite queries, migrations, retention, and aggregation
- `weather`: external weather provider interface and cache
- `analysis`: baseline, expectation, confidence, insights, and alert rules
- `auth`: password, sessions, CSRF, and rate limits
- `api`: REST, SSE, DTOs, validation, and embedded frontend

The Solarman transport sits behind an internal interface. Existing Go libraries may inform or bootstrap the implementation, but tests and domain code cannot depend directly on one immature third-party API.

### Data Flow

1. Collector queries SOFAR every 10 seconds through one serialized Modbus connection.
2. Decoder converts register blocks into a validated typed snapshot.
3. Latest snapshot publishes immediately through SSE.
4. One consolidated sample per minute persists to SQLite; status changes and faults persist immediately.
5. Scheduled jobs produce hourly, daily, and monthly aggregates.
6. Analysis combines telemetry, installed capacity, location, weather, and learned history.
7. API exposes live state, history, insights, alerts, settings, and CSV export.

### Docker Packaging

A multi-stage Dockerfile:

1. Node stage installs locked frontend dependencies and builds static assets.
2. Go stage compiles the backend with frontend assets embedded.
3. Minimal final stage contains the Go binary, certificates, timezone data if required, and an unprivileged runtime user.

Runtime contract:

- One container
- Port `8080`
- Persistent volume mounted at `/data`
- SQLite database at `/data/helio.db`
- Docker healthcheck
- Restart policy `unless-stopped` in supplied Compose example
- Environment variables reserved for bootstrap/runtime overrides and future secrets

Container networking must reach the logger’s LAN address. Docker Desktop starts at login and restarts the container; later Linux/Raspberry Pi deployment uses the same image and volume contract.

## Persistence

SQLite runs in WAL mode with a busy timeout and versioned migrations.

Primary tables:

- `users`
- `sessions`
- `settings`
- `telemetry_minute`
- `weather_hourly`
- `daily_summary`
- `alerts`
- `recommendations`
- `action_audit`

Default retention keeps minute samples for two years and aggregates indefinitely. Retention is configurable. Backup/export must produce a consistent database snapshot or portable archive. Weather responses are cached and Helio remains functional when the provider is unavailable.

## Failure Handling

- Use short logger timeouts and exponential backoff with jitter.
- Serialize requests because the logger has limited concurrent-connection behavior.
- Distinguish process health, database readiness, logger connectivity, and weather availability.
- Logger or weather failure never crashes the HTTP server.
- Mark telemetry stale after a configured threshold.
- Preserve gaps instead of fabricating measurements.
- Shut down gracefully, completing or cancelling in-flight work before closing SQLite.
- Expose `/health/live` for process liveness and `/health/ready` for application readiness.

## Security Boundaries

- MVP exposes no Modbus writes.
- All UI and API routes except health endpoints require authentication.
- Validate and normalize all configuration and API inputs.
- Use CSRF protection for mutations and login rate limiting.
- Do not persist future Telegram or remote-access tokens as plaintext settings; use Docker secrets or protected environment injection.
- Never expose port `8080` directly to the public internet.

## Testing Strategy

- Unit tests for V5 framing, CRC, endian handling, SOFAR scaling, range checks, and alert rules
- Captured read-only frames from the real inverter as deterministic fixtures
- Fake transport for timeouts, stale frames, malformed responses, disconnects, and recovery
- Storage integration tests for migrations, WAL behavior, retention, and aggregation
- API integration tests for authentication, CSRF, DTO validation, SSE, and CSV export
- Frontend tests with Vitest, Testing Library, and MSW
- Playwright end-to-end tests for onboarding, login, dashboard, history, theme persistence, and settings
- Docker smoke test covering image build, healthcheck, LAN configuration, restart, and volume persistence
- Explicit read-only hardware test against `LOGGER_IP:8899`

## Success Criteria

- One command starts one container and preserves data across recreation.
- Mac and phone can authenticate and use the responsive dashboard over local Wi-Fi.
- Live telemetry updates without refreshing the page.
- At least 24 hours of samples produce correct daily charts and summary values.
- Logger and weather outages degrade visibly and recover automatically.
- PV2 inactivity creates no false alert.
- Historical gaps remain explicit.
- No MVP code path can write an inverter register.
- Light/dark/system theme behavior matches approved mockups.

## References

- PySolarmanV5 protocol documentation: https://pysolarmanv5.readthedocs.io/en/latest/solarmanv5_protocol.html
- SOFAR 3K–6KTLM-G3 manual: https://www2.sofarsolar.com/upload/file/20240311/1710118746019090214.pdf
- Compatible SOFAR register-map example: https://gist.github.com/gushcs/223ca2c61af2d345238d286746617650
- Go Solarman implementation reference: https://github.com/snowirbis/solarman
- Alternative Go implementation reference: https://github.com/tlmnb/gosolarman
