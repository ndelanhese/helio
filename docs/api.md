# Local API v1

Helio serves a same-origin JSON/CSV/SSE API under `/api/v1`. It is for trusted local integrations. There are no write endpoints for the inverter or logger; the only authenticated mutation changes Helio settings.

Examples use `http://192.0.2.1:8080`, an IANA-reserved documentation address. Replace it with the private Helio host only in your local shell. Never expose the API to the public internet.

## Authentication and CSRF

`POST /api/v1/bootstrap` and `POST /api/v1/auth/login` require a same-origin `Origin` matching `Host`. Success sets `helio_session`, an `HttpOnly; SameSite=Strict; Path=/` cookie. `Secure` is enabled when `HELIO_SECURE_COOKIES=1`. Sessions have a 30-day absolute lifetime and a 24-hour idle limit.

`GET /api/v1/auth/session` rotates and returns `csrfToken`. Send that value as `X-CSRF-Token` on authenticated mutations (`PUT /api/v1/settings`, `POST /api/v1/auth/logout`, and finance POST routes) and include the same-origin `Origin`. API responses use `Cache-Control: no-store`.

Five failed or concurrent login attempts for the same normalized client IP and username in 15 minutes produce `429` with `Retry-After`. A successful login clears that bucket. Other endpoints have no general request-rate limiter in v0.1; keep integrations modest, and reconnect SSE using the advertised retry delay.

```sh
base=http://192.0.2.1:8080
curl -sS -c cookies.txt -H 'Content-Type: application/json' \
  -H 'Origin: http://192.0.2.1:8080' \
  --data '{"username":"operator","password":"<password>"}' \
  "$base/api/v1/auth/login"
curl -sS -b cookies.txt "$base/api/v1/auth/session"
```

Protect `cookies.txt` and delete it after use. Do not put passwords or CSRF tokens in command history on shared systems.

## Endpoints

Unauthenticated:

- `GET /health/live` — process liveness.
- `GET /health/ready` — database readiness.
- `GET /health/components` — sanitized component state; when weather is available, includes current modeled `temperatureC`, `precipitationMM`, `weatherCode`, `cloudCoverPct`, `windSpeedKMH`, and `irradianceWM2`.
- `GET /api/v1/bootstrap/status` — `{ "open": true|false }`.
- `POST /api/v1/bootstrap` — one-time administrator and settings creation; closes after success.
- `POST /api/v1/auth/login` — creates a session.

Authenticated:

- `GET /api/v1/auth/session`; `POST /api/v1/auth/logout`.
- `GET /api/v1/live`; `GET /api/v1/live/events`.
- `GET /api/v1/history?from=<RFC3339>&to=<RFC3339>&resolution=minute|hour|day|month`. Minute ranges are limited to 366 days.
- `GET /api/v1/history.csv?from=<RFC3339>&to=<RFC3339>`. CSV ranges are limited to 31 days to bound server memory and staging-disk use.
- `GET /api/v1/insights?day=YYYY-MM-DD` in the configured local timezone.
- `GET /api/v1/alerts?state=open|resolved`; newest-first, maximum 100.
- `GET /api/v1/settings`; `PUT /api/v1/settings` with CSRF and same-origin checks. Logger host, serial, port, or Modbus-slave changes additionally require a successful `POST /api/v1/auth/confirm-password` for the same session immediately beforehand; confirmation is short-lived and one-shot.
- `GET /api/v1/data/backup` — consistent SQLite snapshot, audited.
- `GET /api/v1/finance/summary` — latest projection (or `null`) and the 12 latest billing cycles.
- `GET /api/v1/finance/cycles`; `POST /api/v1/finance/cycles` with CSRF. The POST body requires RFC3339 `readingStart` and `readingEnd`, plus nonnegative integer `activeConsumptionKWh`, `injectedKWh`, `creditsUsedKWh`, `creditBalanceKWh`, and `totalPaidMinor` (centavos). It returns the saved cycle and an explicitly estimated component projection.
- `GET /api/v1/finance/tariff-proposals`; `POST /api/v1/finance/tariff-proposals/{id}/approve` with CSRF. Approval creates an immutable tariff version and is audited.

## SSE, CSV, and errors

The live stream has content type `text/event-stream`, begins with `retry: 5000`, sends an initial `event: state`, then named collector events with one JSON `data:` line. Comment heartbeats are sent about every 15 seconds. Clients must tolerate reconnects and replace local state with the next `state` event.

```text
retry: 5000

event: state
data: {"status":"waiting","stale":false}
```

CSV is UTF-8 with exact header:

```text
timestamp,power_w,energy_today_wh,status
```

Timestamps are RFC3339 UTC. CSV export and database backup are recorded in the action audit.

JSON errors use one stable envelope; the HTTP status remains authoritative:

```json
{"error":{"code":"invalid_range","message":"from must be before to and both must be RFC3339 timestamps"}}
```

Common statuses are `400` invalid JSON, `401` missing/invalid session or password confirmation, `403` origin/CSRF failure or missing recent confirmation, `409` closed bootstrap, `422` validation, `429` login/confirmation rate limit, `500` internal failure, and `503` unavailable dependency.
