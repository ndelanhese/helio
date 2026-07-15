# Hardware testing

Hardware tests are explicit, opt-in, and read-only. The Solarman client supports Modbus function `0x03` only; Helio has no inverter write path. The default test run skips without opening a network connection.

Never commit or paste a real logger IP, serial, location, credential, `.env.hardware`, raw capture, or test trace. Documentation and automated fixtures use reserved fake identities only.

## Offline safety check

```sh
go test -v ./internal/sofar -run TestHardwareReadOnly
```

Expected: `SKIP`, with no hardware access.

## Owner-authorized LAN probe

Copy `.env.hardware.example` to the ignored `.env.hardware`, fill it locally, and ensure the target is your private logger. The probe rejects public targets unless a separate explicit override is set; do not use that override for normal testing.

```sh
set -a
. ./.env.hardware
set +a
HELIO_HARDWARE_TEST=1 go test -v ./internal/sofar -run TestHardwareReadOnly
```

Alternatively, `HELIO_HARDWARE_TEST=1 make hardware-test` prints one redacted telemetry snapshot. It must not print the logger address, serial, raw frame, or credentials and must not modify source or fixtures. Stop immediately if the target identity or network route is uncertain.

Use the reserved documentation address `192.0.2.1` and fake serial `123456789` only in examples; they are not runnable deployment values. Real hardware validation belongs on the equipment owner's trusted LAN with authorization and electrical safety procedures.
