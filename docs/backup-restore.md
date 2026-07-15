# Backup and restore

Helio stores its durable state in one SQLite database, `/data/helio.db`. Treat a backup as sensitive: it contains settings, telemetry history, the administrator password hash, and active session records. Store it with restrictive permissions and encrypt it at rest and in transit.

## Create and validate a backup

Prefer the authenticated online backup for normal operation. It is safe while Helio is running, creates a consistent SQLite snapshot, and downloads a file named `helio-backup-YYYYMMDD-HHMMSS.db`. Keep the browser connected until the download completes.

Online snapshot creation needs temporary free space on the `/data` volume of approximately the current database size. Check volume free space before starting a backup; a full volume causes the export to fail without changing the live database.

Validate every downloaded file before relying on it:

```sh
chmod 600 helio-backup-*.db
sqlite3 helio-backup-20260715-120000.db 'PRAGMA integrity_check;'
sqlite3 helio-backup-20260715-120000.db 'SELECT count(*) FROM schema_migrations;'
```

`integrity_check` must print exactly `ok`. Copy the validated file to separate storage, then encrypt that copy with your established backup tool. A file on the same disk or Docker volume is not a disaster-recovery backup.

Use an offline filesystem copy only when the authenticated backup is unavailable. First stop the application gracefully and leave its container stopped for the entire copy:

```sh
set -eu
wal_probe="$(mktemp -d)"
cleanup() { rm -rf "$wal_probe"; }
trap cleanup EXIT HUP INT TERM

docker compose stop --timeout 30 helio
container_id="$(docker compose ps --all --quiet helio)"
test -n "$container_id"

exit_code="$(docker inspect --format '{{.State.ExitCode}}' "$container_id")"
oom_killed="$(docker inspect --format '{{.State.OOMKilled}}' "$container_id")"
state_error="$(docker inspect --format '{{.State.Error}}' "$container_id")"
if [ "$exit_code" -ne 0 ] || [ "$oom_killed" != "false" ] || [ -n "$state_error" ]; then
  echo "ABORT: Helio did not stop cleanly (exit=$exit_code oom=$oom_killed error=$state_error)" >&2
  exit 1
fi

if docker cp "$container_id:/data/helio.db-wal" "$wal_probe/helio.db-wal" 2>/dev/null; then
  wal_bytes="$(wc -c <"$wal_probe/helio.db-wal" | tr -d ' ')"
  echo "ABORT: /data/helio.db-wal still exists after shutdown ($wal_bytes bytes)" >&2
  exit 1
fi

docker cp "$container_id:/data/helio.db" ./helio-offline.db
chmod 600 ./helio-offline.db
sqlite3 ./helio-offline.db 'PRAGMA integrity_check;'
```

Any nonzero exit code—including `137` after a forced kill—or `OOMKilled=true` means the shutdown was not safe for a main-file-only copy. Abort, preserve the stopped volume, investigate, then restart Helio and prefer the authenticated online backup.

The WAL can contain committed data that has not yet been checkpointed into `helio.db`. Omitting it can silently lose that data; the integrity_check can still report `ok` on the older main database because structural validity does not prove the WAL's committed rows were included. Therefore the procedure aborts if `/data/helio.db-wal` exists at all, even when it appears empty; it never copies only the main database while a WAL artifact remains.

Start Helio again only after the copy and validation finish: `docker compose start helio`.

## Restore without overwriting the live volume

Never overwrite `helio.db` in the live volume. Stop Helio, preserve the current volume as the rollback point, and restore into a newly named volume.

1. Confirm the backup passes `PRAGMA integrity_check` and was created by the same Helio version or an older version supported by the target release. Restoring a database from a newer Helio release into an older image is not supported; check version compatibility before proceeding.
2. Run `docker compose stop helio`. Record the current image reference and volume name. Do not delete either one.
3. Create a new volume and copy into that volume only. Set `maintenance_image` to a trusted shell image pinned by a digest that you have verified; do not use an unpinned tag in an unattended restore.

   ```sh
   backup_path="$PWD/helio-backup-20260715-120000.db"
   backup_dir="$(dirname "$backup_path")"
   backup_file="$(basename "$backup_path")"
   restore_volume="helio-restore-20260715"
   maintenance_image='<trusted-image>@sha256:<verified-digest>'

   docker volume create "$restore_volume"
   docker run --rm --user 0:0 \
     -e BACKUP_FILE="$backup_file" \
     -v "$restore_volume:/restore" \
     -v "$backup_dir:/backup:ro" \
     "$maintenance_image" sh -eu -c \
     'cp "/backup/$BACKUP_FILE" /restore/helio.db && chown 65532:65532 /restore/helio.db && chmod 0600 /restore/helio.db'
   ```

4. Validate the exact copy in the new volume before starting Helio. `helio_image` must be the immutable image reference recorded in step 2.

   ```sh
   helio_image='<registry>/helio@sha256:<recorded-digest>'
   check_container="helio-restore-check-$$"
   docker create --name "$check_container" -v "$restore_volume:/data" "$helio_image"
   docker cp "$check_container:/data/helio.db" ./helio-restore-check.db
   docker rm "$check_container"
   chmod 600 ./helio-restore-check.db
   sqlite3 ./helio-restore-check.db 'PRAGMA integrity_check;'
   rm ./helio-restore-check.db
   ```

   The check must print exactly `ok`.

5. Point a temporary Compose override at the new external volume:

   ```yaml
   # compose.restore.yaml
   volumes:
     helio-data:
       external: true
       name: ${HELIO_RESTORE_VOLUME}
   ```

   Start the recorded image against the restored volume, then check `/health/ready`, sign in, and verify settings and history before accepting the restore:

   ```sh
   HELIO_IMAGE="$helio_image" HELIO_RESTORE_VOLUME="$restore_volume" \
     docker compose -f compose.yaml -f compose.restore.yaml up -d helio
   ```
6. Keep the old stopped volume unchanged until the restored installation has been verified and separately backed up.

The maintenance container needs root only to set ownership inside the new volume; it must not mount the live volume. Pin its image by digest according to your deployment policy and remove it immediately after the copy.

## Rollback

If validation, startup, migrations, settings, or history checks fail, stop the restored container. Repoint Compose to the untouched old volume and the recorded prior image, then start Helio and verify readiness. Preserve the failed restore volume for diagnosis; do not copy its files back over the live volume.

Only remove an old volume after a successful restore drill, a new encrypted backup, and the end of your rollback retention period.
