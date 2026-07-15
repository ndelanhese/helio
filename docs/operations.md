# Operate Helio

## Health and readiness

`/health/live` proves the process can answer HTTP. `/health/ready` is a database-readiness check used by Docker; logger or weather failure does not make the container unready. `/health/components` reports database, logger, collector, jobs, weather, alerts, and analysis states plus sanitized error classes and timestamps. When weather is available, it also returns current hourly forecast cloud cover and solar irradiance. Weather uses `available`, `stale`, or `unavailable`.

```sh
curl --fail http://127.0.0.1:8080/health/live
curl --fail http://127.0.0.1:8080/health/ready
curl --fail http://127.0.0.1:8080/health/components
docker compose ps
```

A ready container with an offline logger is degraded, not crashed. Check routing to logger TCP 8899 and settings before restarting anything.

## Logs and time

```sh
docker compose logs --since=30m --timestamps helio
docker compose logs --follow --timestamps helio
```

Logs intentionally avoid credentials, sessions, logger serials, raw frames, and full addresses. Stored timestamps are UTC. Calendars, aggregation, and display use the configured IANA timezone (for example, `America/Sao_Paulo`) and therefore honor daylight-saving transitions where applicable. Change timezone deliberately: old UTC observations remain intact, daily/monthly summaries are rebuilt, and timezone-derived daily analyses plus persistent-underproduction evidence are invalidated until the jobs recompute them under the new calendar.

## Storage, retention, and ownership

The named `helio-data` volume contains `/data/helio.db`. The image runs as UID/GID `65532:65532`, with a read-only root filesystem; `/data` and the bounded `/tmp` tmpfs are the only writable paths. Restored database files must be owned by `65532:65532` and mode `0600`.

Minute telemetry retention defaults to 730 days and is configurable in Settings. Hourly, daily, and monthly aggregates are retained indefinitely in v0.1. Reducing retention removes old minute detail during the daily maintenance job; it is not a backup policy.

Follow [Backup and restore](backup-restore.md) for the authenticated consistent snapshot and offline restore drill. Stop Helio before replacing a database, restore into a new volume, validate `PRAGMA integrity_check`, and preserve the old stopped volume as rollback. Never overwrite a running SQLite database or copy only `helio.db` while a WAL file exists.

## Update one tag at a time

Back up and validate first. Upgrade to one explicit immutable tag, verify it, and only then consider the next tag:

```sh
export HELIO_IMAGE=ghcr.io/ndelanhese/helio:v0.1.0
docker compose pull helio
docker compose up -d helio
curl --fail http://127.0.0.1:8080/health/ready
docker compose logs --since=10m --timestamps helio
```

These image commands apply only after that release exists. Do not jump across multiple tags in one change, and never restore a database created by a newer Helio into an older binary. GitHub's release workflow serializes repository releases with a concurrency group, but a queued newer tag can run after an older failed or delayed workflow; verify each tag's release, digest, and changelog instead of assuming completion order from tag creation time.

For a source checkout, fetch the intended commit, review the diff, rebuild with `docker compose build --pull helio`, then run `docker compose up -d helio` and the same checks.

## Routine checklist

- Confirm readiness and component health.
- Review logger/weather degradation without treating it as container failure.
- Review logs for stable sanitized error classes.
- Export and validate an encrypted backup on separate storage.
- Perform a restore drill into a new volume and retain the rollback volume.
- Verify the configured timezone and minute-retention window.
- Check phone access only from the intended private subnet; confirm no router port-forward exists.
