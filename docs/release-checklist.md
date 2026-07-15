# v0.1 release-candidate checklist

Do not tag or publish from this checklist. Record evidence for each item against the exact candidate commit.

## Automated gates

```sh
make docs-check
go test -race ./...
go vet ./...
npm --prefix web ci
npm --prefix web test -- --run
npm --prefix web run typecheck
npm --prefix web run lint
npm --prefix web run build
npm --prefix web run test:e2e
docker build -t helio:rc .
HELIO_IMAGE=helio:rc IMAGE=helio:rc make smoke
```

Inspect the image for runtime user `65532:65532`, read-only Compose root filesystem, amd64/arm64 release platforms, absence of Node, and only `/data` plus bounded `/tmp` writable. Scan source, docs, built UI, and image contents for private IPs, real serials, coordinates, credentials, cookies, traces, databases, and raw captures.

## Manual install and operations

- [ ] A clean Mac with Docker Desktop runs `docker compose up -d`, reaches localhost, and completes first bootstrap.
- [ ] Clean 64-bit Linux amd64 and Raspberry Pi arm64 hosts run the same Compose flow.
- [ ] Host→logger TCP 8899 routing is verified across the intended LAN/VLAN without exposing either service publicly.
- [ ] Default port publishing is localhost only; explicit private `HELIO_BIND_IP` enables a phone on the trusted LAN.
- [ ] Phone→host TCP 8080 works without router port-forwarding; HTTPS/Tailscale limitations and secure-cookie requirements are understood.
- [ ] Logger and weather outages appear in component health while database readiness stays healthy.
- [ ] Logs contain useful sanitized classes and no private identifiers.
- [ ] IANA timezone calendar boundaries and the 730-day default minute retention are correct.
- [ ] The named volume survives container recreation and files are owned by UID/GID 65532.
- [ ] An authenticated backup passes SQLite integrity checks; a stopped-container restore into a new volume succeeds; the old volume remains rollback.
- [ ] Update is performed one immutable tag at a time, accounting for GitHub release concurrency/queue ordering.
- [ ] Uninstall is tested both preserving the volume and explicitly deleting it only after backup.
- [ ] Hardware test skips by default; the explicit owner-LAN run is read-only and emits no address, serial, credential, or raw frame.
- [ ] API authentication/cookie/CSRF/same-origin behavior, SSE reconnect/heartbeat, CSV header, error envelope, and login rate limit match [the API contract](api.md).
- [ ] No service worker is registered and installation makes no offline telemetry claim.
