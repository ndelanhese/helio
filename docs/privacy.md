# Privacy and network boundary

Helio is local-first: the Solarman logger is read directly over the LAN, raw telemetry and aggregates are stored in the local SQLite volume, authentication is local, and the browser talks to the same Helio origin. Helio has no vendor-cloud account integration and no analytics or crash-reporting SDK.

## Finance data and tariff sources

Billing-cycle values, approved tariff versions, projected component rows, and credit balances remain in the local SQLite volume. The browser only renders the server-returned financial values; it does not calculate tariffs or savings. A tariff proposal records its official source URL and retrieval time, and requires an explicit local approval before it can be used for reconciliation. Operators should treat bill totals and source links as sensitive household energy information when exporting or sharing backups.

## Outbound weather request

Weather-aware analysis is the one intentional external data path in v0.1. About once per hour, the Go process sends an HTTPS request to `api.open-meteo.com`. The query contains:

- configured latitude and longitude;
- bounded start/end dates;
- requested hourly cloud-cover and shortwave-radiation fields;
- UTC as the response timezone.

Coordinates can reveal an approximate installation location. Open-Meteo also observes normal connection metadata such as the Docker host's public source IP. Helio does not include inverter readings, production history, usernames, passwords, cookies, CSRF tokens, logger IP/serial, database contents, or raw protocol frames in that request.

Responses are cached in local SQLite. A failed refresh can use a stale cached response and marks weather health `stale`; without usable cache it marks weather `unavailable`. Logger collection, local history, backups, and non-weather UI remain available.

v0.1 has no runtime weather-disable toggle. Sites requiring zero egress must deny outbound traffic from the container. That deliberately disables refreshed weather context and limits weather-dependent analysis; it does not make logger collection depend on the internet.

## Data the operator controls

The `/data` volume contains the SQLite database and should be treated as sensitive energy/location data. CSV and database backups are authenticated exports and should be stored encrypted outside the Docker host. Logs and action-audit metadata intentionally exclude credentials, session material, raw frames, logger serials, full addresses, and export contents.

Do not expose Helio's HTTP port to the public internet. Use a trusted private LAN or a reviewed HTTPS/Tailscale deployment with secure cookies as described in [Install](install.md) and [Security](../SECURITY.md).
