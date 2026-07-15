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
8. Repository administrators must configure `backend`, `frontend`, `e2e`, `container`, and `codeql` as required status checks. Once that external protection is active, wait for every check and address every failure before requesting final review.

These checks must be configured as required status checks in GitHub; workflow files alone do not enforce merge protection.

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

## Repository protection and releases

These GitHub settings are external state; this repository does not claim they are active merely because the workflows exist. An administrator must configure them before accepting the first protected pull request or pushing the first release tag:

1. Open **Settings → Rules → Rulesets**, create a branch ruleset targeting `main`, set it active, require pull requests, require the `backend`, `frontend`, `e2e`, `container`, and `codeql` status checks, block force pushes and deletions, and require the branch to be up to date before merging.
2. In **Settings → Rules → Rulesets**, create an active tag ruleset targeting `v*`. Restrict tag creation to release maintainers and block tag updates, force updates, and deletions. The release workflow independently rejects lightweight tags and tags that do not dereference to the exact `main` commit.
3. Open **Settings → Environments**, create the `release` environment, add at least one required reviewer other than the releaser, enable prevention of self-review, choose selected deployment branches and tags, and allow only the tag pattern `v*`.
4. Verify the external configuration before tagging with `gh api repos/ndelanhese/helio/rulesets` and `gh api repos/ndelanhese/helio/environments/release`. Review the returned enforcement state, required checks, bypass actors, reviewers, and deployment policy; do not proceed if any differ from the policy above.

Create a release only from a commit already present on `origin/main`, using an annotated tag. A signed annotated tag is preferred when the maintainer has a verified signing key:

```sh
git fetch origin main --tags
git merge-base --is-ancestor HEAD origin/main
git tag -a v0.1.0 -m "Helio v0.1.0"
git push origin v0.1.0
```

Never retry by force-moving a release tag. A rerun succeeds without mutation only when the immutable GHCR tag and GitHub Release both exist, their recorded digests match, and Cosign verifies the keyless signature and provenance. A half-published or mismatched state requires maintainer investigation rather than automated overwrite.
