# Container Task 3 — CI, security, and multi-architecture release report

## Scope

Implemented Task 3 only from `docs/superpowers/plans/2026-07-14-helio-container-release.md`, starting from `94c1e39`. No PWA work or release/operator documentation from Task 4 was added.

## Delivered

- Pull-request and `main` CI with exact required jobs: `backend`, `frontend`, `e2e`, and `container`.
- Separate `codeql` workflow and exact `codeql` job covering Go and JavaScript/TypeScript on pull requests, `main`, and a weekly schedule.
- Strict stable `vMAJOR.MINOR.PATCH` release validation before any publishing authority is granted.
- Release verification gates for Go race/vet, locked frontend install/test/typecheck/lint/build, browser acceptance, and container smoke.
- Lower-case `ghcr.io/ndelanhese/helio` multi-architecture publication for `linux/amd64` and `linux/arm64`, with SBOM, maximum provenance, digest output, and keyless Cosign signing through GitHub OIDC.
- GitHub release creation from `CHANGELOG.md` only after image publication and signing succeed.
- Weekly, grouped, five-PR-limited Dependabot configuration for Go modules, npm, Docker, and GitHub Actions.
- Operational contributor parity instructions and private vulnerability-reporting expectations.
- Dependency-free Ruby contract tests plus digest-pinned actionlint validation through `make workflow-check`.

## Security decisions

- CI has only top-level `contents: read`; it does not use secrets, `pull_request_target`, write permissions, or executable download pipelines.
- CodeQL has only `contents: read`, `security-events: write`, and the required `actions: read`.
- Release starts with `permissions: {}`. Verification gets only `contents: read`; image publication gets `contents: read`, `packages: write`, and `id-token: write`; GitHub release creation is isolated to a final job with only `contents: write`.
- Every action reference, including official GitHub actions, is a full 40-character commit SHA with its exact version recorded in a comment.
- Action SHAs were resolved from their upstream exact version refs with `git ls-remote`. Pins use checkout v7.0.0, setup-go v6.5.0, setup-node v7.0.0, upload-artifact v7.0.1, CodeQL v4.37.0, Docker setup-qemu v4.2.0, setup-buildx v4.2.0, login v4.4.0, metadata v6.2.0, build-push v7.3.0, and Cosign installer v4.1.2.
- actionlint v1.7.12 is pinned to the verified multi-platform index digest `sha256:b1934ee5f1c509618f2508e6eb47ee0d3520686341fec936f3b79331f9315667`.
- E2E artifacts exist only after a test failure and a successful privacy scan. Sensitive trace archives are deleted before upload. The scan materializes archives before matching to avoid a `pipefail`/SIGPIPE false-negative.
- Fork-safe caches use only the built-in read-safe caches from setup-go and setup-node; no cache write tokens or custom secret-bearing keys are used.

## TDD evidence

RED 1: `ruby scripts/workflow_contract_test.rb` produced 12 expected failures because the workflows, Dependabot policy, lint target, and operational policy text did not exist.

GREEN 1: the same suite passed 12 tests and 236 assertions after the minimum implementation.

RED 2: a new privacy regression assertion failed against the `unzip | grep` pipeline, proving the SIGPIPE false-negative was present.

GREEN 2: after materializing trace contents before matching, the suite passed 12 tests and 238 assertions.

## Verification

- `make workflow-check`: PASS — 12 contract tests, 238 assertions, and actionlint v1.7.12 with no diagnostics.
- `go test -race ./...`: PASS — 426 tests across 21 packages.
- `go vet ./...`: PASS — no issues.
- `npm --prefix web ci`: PASS — 274 packages, zero reported vulnerabilities.
- `npm --prefix web test -- --run`: PASS — 23 files, 167 tests.
- `npm --prefix web run typecheck`: PASS.
- `npm --prefix web run lint`: PASS — 97 files, no warnings.
- `npm --prefix web run build`: PASS — production Vite build.
- `HELIO_IMAGE=helio:task3 IMAGE=helio:task3 make smoke`: PASS — image build, cleanup test, bootstrap, persisted settings/session/history, degraded logger readiness, and backup integrity.
- `git diff --check`: PASS.

One initial Go race run overlapped `npm ci` and encountered a transient missing `web/node_modules` directory while npm atomically replaced dependencies. The isolated rerun passed all 426 tests and vet. The full browser suite was not rerun on this host because of the coordinator-known browser-host issue; browser dependency installation, `make test-e2e`, failure-only artifact handling, and release gating are enforced by workflow contracts and validated by actionlint. CI will execute the browser suite on Ubuntu.
