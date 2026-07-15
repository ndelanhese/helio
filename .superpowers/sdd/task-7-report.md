# Core Task 7 — Scheduled Aggregation, Retention, Graceful Shutdown

## Status

Implemented and verified.

## Delivered

- Added `internal/jobs.Runner` with an injected clock and persisted hourly, daily, and monthly aggregation.
- Schedules against the effective IANA timezone at local `00:05`; performs one idempotent catch-up after startup when that day's schedule was missed.
- Runs minute retention once with each daily job and uses the effective setting, defaulting to 730 days.
- Waits for an in-flight aggregation during cancellation and cancels/caps that wait at 10 seconds.
- Starts HTTP before settings-dependent collector/jobs; starts collector/jobs only after settings exist, including first bootstrap.
- Gracefully stops HTTP, SSE, collector, jobs, then closes SQLite. Shutdown prevents a late jobs start racing with `Wait`.
- Makes collector state truthful after normal stop, spontaneous exit, and reconfiguration stop timeout.
- Adds component timestamps and sanitized error classes only; health does not expose logger host/serial, frames, credentials, sessions, or CSRF values.
- Keeps readiness database-only, so logger/collector degradation continues to return HTTP 200 from `/health/ready`.

## TDD evidence

- RED: missing `jobs.New`/`WithClock`; schedule and shutdown tests failed to compile.
- GREEN: local 00:05, startup catch-up, daily retention/default 730, transaction wait, and 10-second cap tests passed.
- RED/GREEN: collector stop timestamp/truthfulness; reconfigure timeout no longer reports cancelled collector as running.
- RED/GREEN: jobs cannot start after shutdown begins.
- Existing storage coverage verifies aggregate upserts are deterministic and durable across hourly/daily/monthly summaries.

## Verification

```text
go test -race ./...  -> 224 passed in 14 packages
go vet ./...         -> No issues found
```

No hardware or network calls were used by the new tests.

## Fix Review

Reviewer critical/important findings were resolved in a follow-up TDD wave:

- The 10-second jobs deadline is now a grace period: after it expires the operation context is cancelled and the worker is joined before `Runner.Run` returns. An instrumented repository asserts zero active or post-owner-close calls.
- Collector stop/reconfigure timeouts publish `stop_timeout` but continue waiting for the context-aware collector/final minute flush before returning DB ownership.
- Settings updates pause and join the jobs runner before the settings/calendar transaction, then restart after commit or rollback. Barrier tests prevent old-calendar bounds from interleaving with `ApplyLocation`, and assert no duplicate runner.
- Restarting jobs immediately recomputes effective timezone and retention scheduling.
- Failed aggregation/retention does not advance the completed day and retries after an injected one-minute delay. Transient settings reads degrade and recover instead of terminating the scheduler.
- Unexpected jobs termination clears the app runtime slot so a later start is possible.
- Shutdown explicitly closes the listener, cancels and joins services, drains HTTP, and only then returns to the deferred DB close. An ordered test verifies listener stop → service cancellation → workers joined.
- Logger failures now carry timestamps and sanitized classes; success clears failure metadata. Database, jobs, collector, and weather component observations expose timestamps/classes without raw private error content.

Follow-up verification:

```text
go test -race ./...  -> 233 passed in 14 packages
go vet ./...         -> No issues found
```
