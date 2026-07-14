# Helio Single-Container v0.1 Release Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship reproducible multi-architecture v0.1 image with embedded UI, persistent data, healthcheck, backup path, CI gates, and safe self-hosting documentation.

**Architecture:** Multi-stage Docker build compiles Vite assets into Go embed path, then produces one static-ish non-root binary image. Compose defines one service, one `/data` volume, port 8080, LAN reachability guidance, and readiness-only healthcheck.

**Tech Stack:** Docker BuildKit/buildx, Go 1.26.5, Node 24.17.0, Debian bookworm-slim runtime, GitHub Actions, GHCR, Playwright smoke tests.

## Global Constraints

- One runtime container and one Go process; no Node runtime, sidecar, Redis, Postgres, or reverse proxy bundled.
- Runtime user UID/GID `65532`; root filesystem read-only; writable `/data` and `/tmp` tmpfs only.
- Container exposes `8080`; persistent database `/data/helio.db`.
- Healthcheck calls `/health/ready`, not logger/weather component health.
- Compose restart policy `unless-stopped`.
- Supported release architectures: `linux/amd64`, `linux/arm64`.
- Image tags: immutable `v0.1.0`, moving `0.1`, and `latest` only for stable release.
- Never publish hardware IP, serial, location, credentials, database, raw captures, `.env`, Playwright traces containing form values, or SBOM secrets.
- Port 8080 must not be exposed to public internet; remote access remains out of v0.1.

---

## File Structure

- `Dockerfile`, `.dockerignore`, `compose.yaml` — production artifact/runtime contract.
- `cmd/helio-healthcheck/main.go` — no-shell readiness probe.
- `deploy/helio.env.example` — non-secret runtime overrides.
- `.github/workflows/ci.yml`, `release.yml`, `codeql.yml` — test, build, scan, publish.
- `docs/install.md`, `operations.md`, `backup-restore.md`, `hardware-testing.md`, `api.md` — operator docs.
- `scripts/smoke.sh` — black-box container verification with ephemeral volume.

### Task 1: Reproducible Non-Root Image and Compose Contract

**Files:**
- Create: `Dockerfile`, `.dockerignore`, `compose.yaml`, `deploy/helio.env.example`
- Create: `cmd/helio-healthcheck/main.go`, `main_test.go`
- Modify: `internal/config/config.go`, `Makefile`

**Interfaces:**
- Consumes: frontend build and Go runtime.
- Produces: `docker build -t helio:test .`; Compose service `helio`; healthcheck binary.

- [ ] **Step 1: Write healthcheck unit test**

Use `httptest.Server` to verify exit code 0 only for 200 JSON status `ready`, nonzero for timeout/non-200/malformed JSON. Implement probe logic as `run(ctx,url) error` so test never spawns process.

- [ ] **Step 2: Confirm command package fails**

Run: `go test ./cmd/helio-healthcheck`

Expected: FAIL because command does not exist.

- [ ] **Step 3: Implement image and runtime metadata**

Docker stages:

```dockerfile
# syntax=docker/dockerfile:1.10
FROM node:24.17.0-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26.5-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN rm -rf internal/webui/dist && mkdir -p internal/webui/dist && cp -R web/dist/. internal/webui/dist/
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/helio ./cmd/helio
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/helio-healthcheck

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata && rm -rf /var/lib/apt/lists/* && mkdir -p /data && chown 65532:65532 /data
COPY --from=build /out/helio /usr/local/bin/helio
COPY --from=build /out/healthcheck /usr/local/bin/helio-healthcheck
USER 65532:65532
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 CMD ["helio-healthcheck"]
ENTRYPOINT ["helio"]
```

Compose maps `8080:8080`, named volume `helio-data:/data`, `read_only:true`, `tmpfs:/tmp`, `security_opt:no-new-privileges:true`, drops all capabilities, and uses `unless-stopped`. Config defaults health URL to `http://127.0.0.1:8080/health/ready`.

- [ ] **Step 4: Build and inspect image**

Run: `docker build --pull -t helio:test . && docker image inspect helio:test --format '{{.Config.User}} {{json .Config.Healthcheck.Test}}'`

Expected: build PASS; output begins `65532:65532` and healthcheck invokes `helio-healthcheck`.

- [ ] **Step 5: Commit image contract**

```bash
git add Dockerfile .dockerignore compose.yaml deploy cmd/helio-healthcheck internal/config/config.go Makefile
git commit -m "build: package Helio as one non-root container"
```

### Task 2: Volume Persistence, Backup/Restore, and Docker Smoke

**Files:**
- Create: `scripts/smoke.sh`
- Create: `docs/backup-restore.md`
- Modify: `internal/api/settings.go`, `internal/httpserver/router.go`
- Create: `internal/api/backup_test.go`

**Interfaces:**
- Consumes: authenticated `GET /api/v1/data/backup` from core plan.
- Produces: documented offline restore and `make smoke` durability gate.

- [ ] **Step 1: Write authenticated backup API test**

Test unauthorized 401, authenticated response `application/vnd.sqlite3`, attachment filename, SQLite header bytes, consistent row set during concurrent insert, and action-audit record without archive contents.

- [ ] **Step 2: Confirm existing backup contract passes before container work**

Run: `go test ./internal/api -run TestBackup`

Expected: PASS for auth, content type, SQLite header, and consistent snapshot.

- [ ] **Step 3: Implement exact smoke sequence and harden backup streaming**

Confirm route streams `storage.Backup` with no full database buffering and aborts cleanly on client disconnect. `scripts/smoke.sh` uses `set -euo pipefail`, unique container/volume names, trap cleanup, starts image, waits readiness up to 60 seconds, bootstraps via generated throwaway password, injects fake collector only through build-tagged smoke binary or API-free fixture mode, confirms history, recreates container with same volume, confirms session/settings/history, checks logger component may degrade while ready stays 200, downloads/reopens backup, and removes container/volume. Script never prints password/cookie/CSRF.

- [ ] **Step 4: Run smoke twice**

Run: `make smoke && make smoke`

Expected: both PASS from clean ephemeral volumes and leave no `helio-smoke-*` resources.

- [ ] **Step 5: Commit durability gate**

```bash
git add scripts/smoke.sh docs/backup-restore.md internal/api internal/httpserver/router.go Makefile
git commit -m "test: verify container persistence and backup"
```

### Task 3: CI, Security Scans, Multi-Architecture Release

**Files:**
- Create: `.github/workflows/ci.yml`, `release.yml`, `codeql.yml`
- Create: `.github/dependabot.yml`
- Modify: `CONTRIBUTING.md`, `SECURITY.md`

**Interfaces:**
- Produces required checks `backend`, `frontend`, `e2e`, `container`, `codeql`; signed/provenanced GHCR image on `v*` tag.

- [ ] **Step 1: Add local workflow syntax check**

Run `docker run --rm -v "$PWD:/repo" rhysd/actionlint:latest -color` after workflow files exist. Expected: exit 0 and no diagnostic.

- [ ] **Step 2: Create least-privilege workflows**

CI triggers pull requests/push main; permissions `contents:read`; Go job runs test-race/vet; frontend job uses npm cache then test/typecheck/lint/build; E2E uploads traces only on failure after redaction assertion; container job builds and runs smoke. Release triggers tags matching `v[0-9]+.[0-9]+.[0-9]+`, requires tests, logs into GHCR with `packages:write`, buildx builds amd64/arm64, generates SBOM/provenance, signs keylessly with GitHub OIDC, and creates GitHub release from changelog. No `pull_request_target`, mutable third-party action tags, or write permission in PR jobs.

- [ ] **Step 3: Configure dependency updates and CodeQL**

Dependabot weekly groups Go modules, npm, Docker, and Actions separately with limit five. CodeQL runs Go and JavaScript/TypeScript weekly plus PR/push, permissions only `security-events:write` and `contents:read`.

- [ ] **Step 4: Validate workflow and local parity**

Run: `actionlint && go test -race ./... && go vet ./... && npm --prefix web ci && npm --prefix web test -- --run && npm --prefix web run build && docker build -t helio:test . && make smoke`

Expected: all exit 0.

- [ ] **Step 5: Commit automation**

```bash
git add .github CONTRIBUTING.md SECURITY.md
git commit -m "ci: test scan and publish Helio releases"
```

### Task 4: PWA, Operator Documentation, and v0.1 Release Gate

**Files:**
- Create: `web/public/manifest.webmanifest`, `web/public/icons/icon-192.png`, `icon-512.png`, `maskable-512.png`
- Modify: `web/index.html`
- Create: `docs/install.md`, `operations.md`, `hardware-testing.md`, `api.md`
- Modify: `README.md`, `CHANGELOG.md`, `SUPPORT.md`

**Interfaces:**
- Produces installable metadata without offline telemetry cache; complete operator/runbook/API docs.

- [ ] **Step 1: Write documentation verification checklist**

Create link-check command and manual checklist covering Mac Docker Desktop, Linux/Raspberry Pi, logger LAN routing, first bootstrap, phone access by private host IP, updates, backup/restore drill, data retention, health semantics, logs, timezones, hardware opt-in test, API auth/SSE/CSV, HTTPS/Tailscale warning, and uninstall preserving/removing volume choices.

- [ ] **Step 2: Add safe PWA metadata**

Manifest name `Helio`, short name `Helio`, start URL `/`, display `standalone`, theme/background colors matching light theme, maskable icon. Do not add service worker in v0.1: stale offline telemetry/auth shell would misrepresent current status. HTML includes theme-color variants and manifest link.

- [ ] **Step 3: Write exact operator docs and update project status**

Install uses `docker compose up -d`; explains Docker Desktop LAN reachability and Linux host networking differences without hardcoded addresses; says never publish port; gives health commands; documents volume ownership; provides tested backup/restore commands that stop container before replacing DB; API doc lists endpoints, auth/cookie/CSRF, examples using reserved documentation address `192.0.2.1`, SSE format, error envelope, rate limits, and no write endpoints. README replaces design-only warning with v0.1 availability only after image is published. Changelog includes Added/Security/Known limitations.

- [ ] **Step 4: Execute release candidate gate**

Run: `go test -race ./... && go vet ./... && npm --prefix web ci && npm --prefix web test -- --run && npm --prefix web run typecheck && npm --prefix web run lint && npm --prefix web run build && npm --prefix web run test:e2e && docker build -t helio:rc . && HELIO_IMAGE=helio:rc make smoke && git grep -En '192\.168\.|loggerSerial[^A-Za-z]' -- ':!docs/superpowers/plans/*'`

Expected: every check PASS; final grep contains reserved documentation examples only, never deployment data.

- [ ] **Step 5: Commit release docs**

```bash
git add web/public web/index.html docs README.md CHANGELOG.md SUPPORT.md
git commit -m "docs: prepare Helio v0.1 self-hosted release"
```

## Plan Acceptance

Fresh Mac/Linux machine runs `docker compose up -d`, completes bootstrap from desktop and phone, receives live data, survives container recreation, exports/restores backup, and sees logger/weather outages without container restart. CI passes five required checks; amd64/arm64 image contains no Node runtime or private identifiers; image runs as UID 65532 with read-only root filesystem.
