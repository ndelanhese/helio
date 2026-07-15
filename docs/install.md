# Install Helio

Helio runs as one local container on `linux/amd64` and `linux/arm64`. It is a release candidate: build it from this checkout until a signed release image is published. Keep port 8080 on a trusted private network; never expose it to the public internet.

The container requires outbound HTTPS for weather-aware analysis. About once per hour it queries Open-Meteo with the configured latitude/longitude and bounded dates. Blocking that egress leaves logger collection, local history, and the UI working, but weather health becomes unavailable and weather-dependent conclusions remain limited. v0.1 has no in-app switch to disable these requests; block outbound access at the container/network layer only if you accept that degradation. See [Privacy](privacy.md).

## Requirements

- macOS: Docker Desktop with Compose v2.
- Linux or Raspberry Pi: Docker Engine with the Compose plugin. A 64-bit OS is required; x86-64 uses `amd64`, and current 64-bit Raspberry Pi systems use `arm64`.
- The Docker host must be able to route to the Solarman logger on TCP 8899. Guest Wi-Fi, client isolation, firewall rules, or VLAN boundaries often block this path.

## Start from source

Copy the repository to the host, then run from its root:

```sh
cp deploy/helio.env.example deploy/helio.env
docker compose up -d
docker compose ps
curl --fail http://127.0.0.1:8080/health/ready
```

Compose builds the current source and binds only `127.0.0.1` by default. Docker Desktop runs Linux containers inside its VM but forwards the published localhost port to macOS. On Linux, Docker publishes directly on the selected host interface. These differences do not change the logger requirement: the container's outbound route must reach the logger.

Open `http://127.0.0.1:8080`, create the administrator, and enter the logger's private address and decimal serial, active MPPT inputs, array details, location, IANA timezone, currency, and tariff. The first bootstrap is atomic and closes after the first administrator is created.

## Phone access on a private LAN

Choose the Docker host's stable RFC1918 LAN interface address—not `0.0.0.0`, a public address, or the logger address—and set it as Compose interpolation in a project-level `.env`:

```sh
printf 'HELIO_BIND_IP=%s\n' '<private-host-LAN-IP>' > .env
docker compose up -d
```

Then open `http://<private-host-LAN-IP>:8080` from a phone on the same trusted LAN. Permit TCP 8080 only from that private subnet in the host firewall. If the phone, host, and logger are on different VLANs, explicitly allow phone→host TCP 8080 and host→logger TCP 8899; do not add a public route or port-forward on the router.

Plain HTTP is appropriate only on a trusted LAN. Tailscale does not by itself turn an HTTP cookie into an HTTPS cookie, and remote access is not a tested v0.1 deployment. If you add Tailscale or a reverse proxy, terminate HTTPS, restrict peers, set `HELIO_SECURE_COOKIES=1`, preserve same-origin `Host`/`Origin`, and never publish 8080 directly.

## Future published image

The following commands are for after the `v0.1.0` release and GHCR image exist; they are not evidence that an image is currently published:

```sh
export HELIO_IMAGE=ghcr.io/ndelanhese/helio:v0.1.0
docker compose pull helio
docker compose up -d helio
```

Use an immutable version tag or digest. See [Operations](operations.md) for updates and health checks, [Backup and restore](backup-restore.md) before upgrades, and [Hardware testing](hardware-testing.md) for the opt-in probe.

## Uninstall

Preserve data while removing the container:

```sh
docker compose down
```

Permanently delete the container and named database volume only after a verified external backup:

```sh
docker compose down --volumes
```

The second command is destructive and cannot be undone without a backup.
