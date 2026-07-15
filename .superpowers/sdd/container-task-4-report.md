# Container Task 4 Report

Status: DONE. All local release-candidate gates pass.

## Scope delivered

- Added exact installable metadata for Helio: `/` start URL, standalone display, light theme/background `#F3F1E8`, light/dark HTML theme-color media tags, any-purpose 192/512 icons, and a maskable 512 icon.
- Deliberately added no service worker, offline cache, or offline telemetry claim.
- Created a restrained Helio solar mark as editable JSON design tokens plus generated SVG and deterministic RGBA PNGs. `--check` compares decoded dimensions and pixels, so harmless PNG recompression passes while a one-pixel mutation fails.
- Added executable PWA, icon, documentation-contract, traversal-safe Markdown link/anchor, E2E-runner, and structural privacy checks under `make docs-check`.
- Added install, operations, hardware-test, API, and RC-checklist documentation; updated README, CHANGELOG, and SUPPORT without claiming a tag or published GHCR artifact exists.
- Replaced authored runtime network examples with RFC 5737 TEST-NET values. The production onboarding model now starts with blank required latitude/longitude fields and uses the browser's resolved IANA timezone at runtime.
- Corrected `make smoke` so the mandated `HELIO_IMAGE=helio:rc make smoke` invocation builds and tests that image instead of silently substituting `helio:local`.
- Added one deterministic E2E entrypoint: one single-worker desktop Chromium run, followed by three small single-worker mobile WebKit groups, with zero retries and fail-fast behavior. Every spec runs exactly once per project; desktop-only screenshot cases are skipped intentionally on mobile.

## Regression evidence

- Link-checker tests reject decoded traversal outside the selected root, ignore links and headings inside variable-length backtick/tilde fences, and validate duplicate GitHub-style anchors: 4 tests / 14 assertions.
- Icon reproduction accepts different zlib encodings of identical pixels and rejects a changed pixel: 1 test / 4 assertions.
- E2E runner coverage proves one desktop invocation, sequential mobile groups, every spec exactly once, `--retries=0`, and `--max-failures=1`: 1 test / 14 assertions.
- Privacy tests reject RFC 1918 addresses, precise baked coordinates, unclassified serial values, a Node runtime, database/trace/capture artifacts, and sensitive strings inside an exported image archive: 2 tests / 12 assertions.
- The stale visual baselines were traced to the committed global `scrollbar-gutter` change in `5b5c6c5`; only History had been refreshed then. Regenerating the three other affected baselines records that existing global layout behavior, and the screenshot update suite passes 4/4.

## Release-candidate gate

- `make docs-check` — PWA 4/25; icon 1/4; docs 4/48; dynamic links 4/14; 21 Markdown files; runner 1/14; privacy 2/12; all passed.
- `go test -race ./...` — 426 tests passed across 21 packages.
- `go vet ./...` — passed.
- `npm --prefix web ci` — 274 packages installed; zero reported vulnerabilities.
- `npm --prefix web test -- --run` — 168 tests passed across 23 files.
- `npm --prefix web run typecheck` — passed.
- `npm --prefix web run lint` — passed with zero warnings.
- `npm --prefix web run build` — passed; manifest and icons copied into `internal/webui/dist`.
- `npm --prefix web run test:e2e` — passed exactly: desktop Chromium 71/71; mobile WebKit groups 61/61, 3 passed + 3 intentional screenshot skips, and 3 passed + 1 intentional screenshot skip. Total: 138 passed, 4 intentional skips, no retries.
- `docker build -t helio:rc .` — passed; local candidate digest `sha256:a1e9781676b28508879aef2690127d83ff54dfdeb75622a950666efd4eaf4182`.
- `HELIO_IMAGE=helio:rc make smoke` — passed cleanup, bootstrap, persistence after recreation, session/settings/history durability, degraded-logger readiness, and backup integrity checks.
- `ruby scripts/privacy-check.rb --image helio:rc` — passed; 15 explicitly synthetic serial fixtures classified and 1,446 exported filesystem entries scanned. No RFC 1918 address, precise default coordinate, Node runtime, database, trace, HAR, or raw capture was found.
- Image inspection — `linux/arm64`; user `65532:65532`; static `/usr/local/bin/helio` entrypoint; static `/usr/local/bin/helio-healthcheck` healthcheck. Compose resolves a read-only root, all capabilities dropped, `no-new-privileges`, localhost-only default publishing, `/data` volume, and bounded `/tmp` tmpfs.
- Broad repository audit — no remaining common private-/16 fixture. Required private-host policy fixtures use the conspicuously synthetic `10.0.0.50`; scientific solar/weather tests retain São Paulo coordinates because location is their test subject. `loggerSerial` occurrences are API/schema identifiers or classified synthetic values, never deployment data.
- `git diff --check` — passed.

## Documentation coverage

- macOS Docker Desktop, Linux, Raspberry Pi, amd64/arm64, LAN/VLAN logger routing, bootstrap, explicit private `HELIO_BIND_IP` phone access, no public port, and HTTPS/Tailscale caveats.
- One-tag-at-a-time updates with release concurrency/queue caveat; future GHCR commands explicitly conditional on publication.
- Exact health semantics, sanitized logs, IANA timezone behavior, default 730-day minute retention and indefinite aggregates, UID/GID 65532, backup/restore/rollback, and uninstall preserve/delete choices.
- Hardware probe is skip-by-default, explicit opt-in, owner-authorized, read-only, and privacy-redacted.
- API endpoint inventory, strict cookie/session lifetimes, CSRF/same-origin requirements, login rate, SSE framing/heartbeat/retry, CSV header, error envelope, and explicit absence of inverter/logger write endpoints.

No tag, image push, GitHub Release, or external publication was performed.
