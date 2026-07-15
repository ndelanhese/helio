# Support

Helio v0.1 is release-candidate software available from source. A container image is supported only after its matching release, digest, and signature are published.

- Questions and hardware exploration: GitHub Discussions
- Confirmed bugs: GitHub Issues using bug form
- Feature proposals: GitHub Issues using feature form
- Security vulnerabilities: private reporting described in [SECURITY.md](SECURITY.md)

Never publish credentials, logger serials, tokens, public IPs, or unredacted diagnostic archives.

Before asking for help, read [Install](docs/install.md) and [Operations](docs/operations.md), then collect only:

- Helio source commit or immutable release tag/digest
- host OS and `amd64` or `arm64`
- the output state names from `/health/ready` and `/health/components`
- sanitized log lines with addresses, serials, cookies, tokens, coordinates, and form values removed

Do not upload the SQLite database, backup, `.env` files, browser trace, raw protocol capture, or hardware-test environment. For restore help, describe which numbered step in [Backup and restore](docs/backup-restore.md) failed without attaching the backup.

Helio provides community support, not electrical, safety, warranty, or grid-compliance advice. Do not change inverter parameters while troubleshooting; v0.1 has no write controls.
