# Security Policy

## Supported versions

Helio is currently pre-release. Security fixes target the latest commit on the default branch; older commits and unpublished development builds are not supported. This table will list supported release lines when the first stable image is published.

| Version | Supported |
| --- | --- |
| Default branch | Yes |
| Older commits and forks | No |

## Reporting a vulnerability

Do not open a public issue for suspected vulnerabilities.

Use GitHub private vulnerability reporting from the repository Security tab. This creates a private maintainer discussion without exposing the report in a public issue. Include:

- affected component and revision
- reproduction steps or proof of concept
- expected impact
- suggested mitigation, if known

Do not include passwords, session cookies, CSRF values, tokens, public IP addresses, real logger serials or other hardware identifiers, private protocol captures, database files, precise locations, or unredacted energy data. Use synthetic fixtures and remove metadata from attachments.

Maintainers aim to acknowledge reports within three business days and provide a status update within ten business days. Triage and remediation timing depends on severity and reproducibility. Maintainers will coordinate disclosure with the reporter after a fix or mitigation is available and will credit reporters who want attribution.

## Security boundaries

- Helio is intended for trusted local networks unless deployed behind HTTPS or Tailscale.
- Never expose its application port directly to the public internet.
- MVP must not write inverter registers.
- Treat inverter writes as safety-sensitive operations.
- Secrets belong in Docker secrets or protected environment injection, not source control.
