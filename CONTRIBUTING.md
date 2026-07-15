# Contributing to Helio

Thanks for helping build a trustworthy local solar monitor.

## Before contributing

- Search existing issues and discussions.
- Use a discussion for broad ideas or hardware-support exploration.
- Use an issue for bounded bugs and features.
- Never post logger serials, credentials, public IPs, tokens, or private protocol captures.
- Read [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) and [SECURITY.md](SECURITY.md).

## Good contributions

- SOFAR and Solarman protocol documentation
- Sanitized read-only frame captures with hardware metadata
- Register-map verification
- UX and accessibility feedback
- Architecture and threat-model review
- Documentation improvements

## Pull requests

1. Open or reference an issue before substantial work.
2. Keep changes focused.
3. Add tests for behavior changes.
4. Update user-facing documentation.
5. Make small, reviewable commits with imperative subjects and no unrelated changes.
6. Rebase or merge the latest `main`, then complete the pull-request template.
7. Confirm no secrets or device identifiers entered Git history.
8. Wait for the required checks—`backend`, `frontend`, `e2e`, `container`, and `codeql`—and address every failure before requesting final review.

All contributions are submitted under Apache License 2.0. By opening a pull request, you confirm you have the right to contribute the material under that license.

## Safety rules

- Read operations remain separate from write operations.
- Never add a writable register without authoritative documentation, allowlist review, confirmation UX, read-back verification, and tests.
- Hardware integration tests must default to read-only and require explicit opt-in.
- Cloud dependencies must degrade gracefully; local monitoring remains primary.

## Development setup

Use Go 1.26.5, Node 24.17.0, npm, Docker with BuildKit, and Ruby (for the dependency-free workflow contract test). Install frontend dependencies from the lockfile:

```sh
npm --prefix web ci
```

Before opening or updating a pull request, run the same gates as CI:

```sh
go test -race ./...
go vet ./...
npm --prefix web test -- --run
npm --prefix web run typecheck
npm --prefix web run lint
npm --prefix web run build
npx --prefix web playwright install --with-deps chromium webkit
make test-e2e
make workflow-check
HELIO_IMAGE=helio:local IMAGE=helio:local make smoke
```

Browser installation is a one-time local prerequisite. `make workflow-check` uses the repository's digest-pinned actionlint image. Do not commit generated reports, browser traces, credentials, or deployment data.
