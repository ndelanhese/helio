# Changelog

All notable changes will be documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases follow semantic versioning.

## [Unreleased]

### Added

- Release-candidate validation and documentation refinements made after the v0.1 feature freeze.

## [0.1.0-rc] - Unreleased

This section describes the source release candidate. It is not a claim that a `v0.1.0` tag, GitHub Release, or GHCR image has been published.

### Added

- One non-root Go container with embedded responsive UI, SQLite persistence, readiness probe, amd64/arm64 release workflow, and Docker Compose deployment.
- Read-only Solarman V5/SOFAR telemetry, minute history and aggregates, local authentication, settings, CSV export, consistent database backup, SSE live updates, weather-aware insights, and durable alerts.
- Install, operations, backup/restore, hardware-test, support, and exact local API documentation.
- Installable manifest and deterministic Helio icon assets without a service worker or offline telemetry cache.

### Security

- Localhost-only Compose default; private LAN access requires an explicit `HELIO_BIND_IP`.
- Runtime UID/GID 65532, read-only root filesystem, dropped capabilities, `no-new-privileges`, bounded tmpfs, Strict HttpOnly sessions, origin-bound CSRF, login throttling, and sanitized component errors.
- Protocol and hardware probe are read-only; no Modbus write function or inverter write API is exposed.

### Known limitations

- Initial hardware/register validation targets SOFAR 6KTLM-G3 with a Solarman V5 logger; other models require validated fixtures.
- No supported public-internet or turnkey remote-access deployment, bundled TLS proxy, notification channel, Home Assistant integration, or inverter controls.
- No service worker/offline cache: an installed shell still requires the live local Helio host and does not imply offline telemetry.
- Minute telemetry defaults to 730-day retention; weather is an optional external dependency, and insights remain confidence-qualified while history accumulates.
