# Security Policy

## Supported versions

Helio has no released version yet. Security fixes will target the default branch until a release policy is published.

## Reporting a vulnerability

Do not open a public issue for suspected vulnerabilities.

Use GitHub private vulnerability reporting from the repository Security tab. Include:

- affected component and revision
- reproduction steps or proof of concept
- expected impact
- suggested mitigation, if known

Do not include real logger serials, passwords, tokens, public IP addresses, or unredacted energy data. Maintainers will acknowledge a valid report as soon as practical and coordinate disclosure after a fix is available.

## Security boundaries

- Helio is intended for trusted local networks unless deployed behind HTTPS or Tailscale.
- Never expose its application port directly to the public internet.
- MVP must not write inverter registers.
- Treat inverter writes as safety-sensitive operations.
- Secrets belong in Docker secrets or protected environment injection, not source control.

