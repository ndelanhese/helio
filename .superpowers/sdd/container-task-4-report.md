# Container Task 4 Report

Status: DONE, with the local E2E host issue reported below.

## Scope delivered

- Added exact installable metadata for `Helio`: `/` start URL, standalone display, light theme/background `#F3F1E8`, light/dark HTML theme-color media tags, any-purpose 192/512 icons, and a maskable 512 icon.
- Deliberately added no service worker, offline cache, or offline telemetry claim.
- Created a restrained Helio solar mark as editable JSON design tokens plus generated SVG and deterministic RGBA PNGs. The mark keeps its sun/horizon geometry within the maskable safe zone. A Ruby generator and byte-for-byte check make all committed assets reproducible without design binaries or network tools.
- Added executable PWA, documentation-contract, internal-link, and anchor checks under `make docs-check`.
- Added install, operations, hardware-test, API, and RC-checklist documentation; updated README, CHANGELOG, and SUPPORT to describe source RC availability without claiming a tag or GHCR artifact exists.
- Corrected runtime UI IP examples to the reserved documentation address `192.0.2.1`; the copy states that it is an unusable format example and still directs operators to their private reserved logger address.
- Corrected `make smoke` so the mandated `HELIO_IMAGE=helio:rc make smoke` invocation builds and tests that image instead of silently substituting `helio:local`.

## RED evidence

1. `make docs-check` failed because the manifest, icons, theme metadata, generator, and operator documents did not exist.
2. The smoke image-variable contract failed because `Makefile` ignored `HELIO_IMAGE` and forced `helio:local`.
3. The runtime-example privacy contract failed because authored onboarding/settings copy embedded `192.168.1.50` in the production bundle.

## GREEN evidence

- `make docs-check` — PWA: 4 tests/25 assertions; docs: 3 tests/42 assertions; links and anchors: 18 Markdown files; all passed.
- Focused IP-example/onboarding/settings regression — 69 tests passed; typecheck and lint passed; production build passed.
- Icon visual review — 512px mark is crisp, intentionally solar, uses the shipped light canvas/ink/sun tokens, and retains generous mask-safe spacing.

## Release-candidate gate

- `go test -race ./...` — 426 tests passed across 21 packages.
- `go vet ./...` — passed.
- `npm --prefix web ci` — 274 packages installed, zero reported vulnerabilities.
- `npm --prefix web test -- --run` — 167 tests passed across 23 files.
- `npm --prefix web run typecheck` — passed.
- `npm --prefix web run lint` — passed with zero warnings.
- `npm --prefix web run build` — passed; manifest and icons copied into `internal/webui/dist`.
- `npm --prefix web run test:e2e` — attempted, but reproduced the known local host issue: no Playwright reporter/server output for 90 seconds. The run was interrupted and is reported as inconclusive, not passed. CI remains the authoritative browser gate.
- `docker build -t helio:rc .` — passed.
- `HELIO_IMAGE=helio:rc make smoke` — passed cleanup, bootstrap, persistence after recreation, session/settings/history durability, degraded-logger readiness, and backup integrity checks.
- Image inspection — local candidate is `linux/arm64`; user is `65532:65532`; healthcheck and entrypoint are the dedicated static binaries; exported filesystem has no Node/npm/npx path; Compose resolves read-only root, dropped capabilities, `no-new-privileges`, and bounded `/tmp` tmpfs.
- Exported-image strings scan after the privacy fix found no `192.168.*`, `node_modules`, or Node binary path. Documentation/PWA checks reject private-IP examples in authored runtime copy and docs.
- The broad requested repository grep still lists established synthetic private-network addresses in unit/E2E fixtures and the `loggerSerial` schema field name. They are test values/identifiers, not deployment data. No real logger IP or serial was introduced. The runtime image contains the schema field name because it is part of the public settings API; it contains no serial value.
- `git diff --check` — passed.

## Documentation coverage

- macOS Docker Desktop, Linux, Raspberry Pi, amd64/arm64, LAN/VLAN logger routing, bootstrap, explicit private `HELIO_BIND_IP` phone access, no public port, HTTPS/Tailscale caveats.
- One-tag-at-a-time updates with release concurrency/queue caveat; future GHCR commands explicitly conditional on publication.
- Exact health semantics, sanitized logs, IANA timezone behavior, default 730-day minute retention and indefinite aggregates, UID/GID 65532, backup/restore/rollback, uninstall preserve/delete choices.
- Hardware probe is skip-by-default, explicit opt-in, owner-authorized, read-only, and privacy-redacted.
- API endpoint inventory, Strict cookie/session lifetimes, CSRF/same-origin requirements, login rate, SSE framing/heartbeat/retry, CSV header, error envelope, and explicit absence of inverter/logger write endpoints.

No tag, image push, GitHub Release, or external publication was performed.
