# Contributing to Helio

Thanks for helping build a trustworthy local solar monitor.

## Before contributing

- Search existing issues and discussions.
- Use a discussion for broad ideas or hardware-support exploration.
- Use an issue for bounded bugs and features.
- Never post logger serials, credentials, public IPs, tokens, or private protocol captures.
- Read [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) and [SECURITY.md](SECURITY.md).

## Good contributions during design phase

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
5. Use clear commits and complete the pull-request template.
6. Confirm no secrets or device identifiers entered Git history.

All contributions are submitted under Apache License 2.0. By opening a pull request, you confirm you have the right to contribute the material under that license.

## Safety rules

- Read operations remain separate from write operations.
- Never add a writable register without authoritative documentation, allowlist review, confirmation UX, read-back verification, and tests.
- Hardware integration tests must default to read-only and require explicit opt-in.
- Cloud dependencies must degrade gracefully; local monitoring remains primary.

## Development setup

Implementation has not started. Exact build, lint, and test commands will be added with initial scaffold. Until then, design changes must keep Markdown links valid and specifications internally consistent.

