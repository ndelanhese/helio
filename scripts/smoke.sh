#!/usr/bin/env bash
set -euo pipefail
umask 077

for command in docker curl go sed grep base64 dd tr mktemp; do
  command -v "$command" >/dev/null 2>&1 || { printf 'smoke: required command missing: %s\n' "$command" >&2; exit 1; }
done

image="${HELIO_IMAGE:-${IMAGE:-helio:local}}"
image_id="$(docker image inspect "$image" --format '{{.Id}}')"
architecture="$(docker image inspect "$image_id" --format '{{.Architecture}}')"
case "$architecture" in
  amd64|arm64) ;;
  *) printf 'smoke: unsupported image architecture: %s\n' "$architecture" >&2; exit 1 ;;
esac

suffix="$(date +%s)-$$-${RANDOM:-0}"
container_name="helio-smoke-${suffix}"
volume_name="helio-smoke-${suffix}"
temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/helio-smoke.XXXXXX")"
cookie_jar="$temp_dir/cookies"
csrf_config="$temp_dir/curl-csrf.conf"
touch "$cookie_jar"
chmod 0600 "$cookie_jar"
touch "$csrf_config"
chmod 0600 "$csrf_config"

cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  set +e
  docker rm -f "$container_name" >/dev/null 2>&1
  docker volume rm -f "$volume_name" >/dev/null 2>&1
  container_left="$(docker ps -a --filter "name=^/${container_name}$" --format '{{.Names}}')"
  volume_left="$(docker volume ls --filter "name=^${volume_name}$" --format '{{.Name}}')"
  if [[ -n "$container_left" || -n "$volume_left" ]]; then
    printf 'smoke: cleanup left Docker resources\n' >&2
    status=1
  fi
  rm -rf "$temp_dir"
  exit "$status"
}
trap cleanup EXIT INT TERM HUP

start_container() {
  docker run --detach --name "$container_name" \
    --read-only --tmpfs /tmp:rw,noexec,nosuid,nodev,size=16m,mode=1777 \
    --security-opt no-new-privileges --cap-drop ALL \
    --volume "$volume_name:/data" --publish 127.0.0.1::8080 \
    "$image_id" >/dev/null
  host_port="$(docker inspect "$container_name" --format '{{(index (index .NetworkSettings.Ports "8080/tcp") 0).HostPort}}')"
  base_url="http://127.0.0.1:${host_port}"
  deadline=$((SECONDS + 60))
  until curl --silent --fail "$base_url/health/ready" >"$temp_dir/ready.json"; do
    if (( SECONDS >= deadline )); then
      printf 'smoke: readiness did not succeed within 60 seconds\n' >&2
      return 1
    fi
    sleep 0.5
  done
  grep -q '"status":"ready"' "$temp_dir/ready.json"
}

docker volume create "$volume_name" >/dev/null
if [[ "${HELIO_SMOKE_FAIL_AFTER_VOLUME:-0}" == "1" ]]; then
  printf 'smoke: forced safe failure after volume creation\n' >&2
  exit 86
fi
CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" go build -tags smoke -trimpath -o "$temp_dir/fixture-linux" ./scripts/smokefixture
CGO_ENABLED=0 go build -tags smoke -trimpath -o "$temp_dir/fixture-host" ./scripts/smokefixture
chmod 0555 "$temp_dir/fixture-linux" "$temp_dir/fixture-host"

seed_window="$(docker run --rm --read-only --tmpfs /tmp:rw,noexec,nosuid,nodev,size=16m,mode=1777 \
  --security-opt no-new-privileges --cap-drop ALL --volume "$volume_name:/data" \
  --volume "$temp_dir/fixture-linux:/smokefixture:ro" --entrypoint /smokefixture \
  "$image_id" seed /data/helio.db)"
history_from="$(printf '%s\n' "$seed_window" | sed -n '1p')"
history_to="$(printf '%s\n' "$seed_window" | sed -n '2p')"
[[ -n "$history_from" && -n "$history_to" ]]
printf 'smoke: fixture seeded\n'

start_container
printf 'smoke: initial container ready\n'
password="$(LC_ALL=C dd if=/dev/urandom bs=32 count=1 2>/dev/null | base64 | tr -d '\r\n')"
printf '{"username":"SmokeAdmin","password":"%s","settings":{"loggerHost":"127.0.0.1","loggerSerial":"123456789","loggerPort":8899,"modbusSlave":1,"panelCount":7,"panelWattage":610,"activeMPPT":[1],"latitude":-23.5,"longitude":-46.6,"timezone":"America/Sao_Paulo","currency":"BRL","tariffMinorPerKWh":95,"retentionDays":730}}' "$password" >"$temp_dir/bootstrap.json"
bootstrap_status="$(curl --silent --show-error --output "$temp_dir/bootstrap-response.json" --write-out '%{http_code}' \
  --cookie-jar "$cookie_jar" --header "Origin: $base_url" --header 'Content-Type: application/json' \
  --data-binary "@$temp_dir/bootstrap.json" "$base_url/api/v1/bootstrap")"
if [[ "$bootstrap_status" != "201" ]]; then
  printf 'smoke: bootstrap returned HTTP %s\n' "$bootstrap_status" >&2
  exit 1
fi
csrf="$(sed -n 's/.*"csrfToken":"\([^"]*\)".*/\1/p' "$temp_dir/bootstrap-response.json")"
[[ -n "$csrf" ]]
printf 'header = "X-CSRF-Token: %s"\n' "$csrf" >"$csrf_config"
chmod 0600 "$csrf_config"
rm -f "$temp_dir/bootstrap.json" "$temp_dir/bootstrap-response.json"
unset csrf
unset password
printf 'smoke: bootstrap complete\n'

printf '%s' '{"loggerHost":"127.0.0.1","loggerSerial":"123456789","loggerPort":8899,"modbusSlave":1,"panelCount":7,"panelWattage":610,"activeMPPT":[1],"latitude":-23.5,"longitude":-46.6,"timezone":"America/Sao_Paulo","currency":"BRL","tariffMinorPerKWh":96,"retentionDays":730}' >"$temp_dir/settings-update.json"
settings_status="$(curl --silent --show-error --output "$temp_dir/settings-put.json" --write-out '%{http_code}' \
  --cookie "$cookie_jar" --header "Origin: $base_url" --config "$csrf_config" \
  --header 'Content-Type: application/json' --request PUT --data-binary "@$temp_dir/settings-update.json" \
  "$base_url/api/v1/settings")"
if [[ "$settings_status" != "200" ]]; then
  printf 'smoke: settings update returned HTTP %s\n' "$settings_status" >&2
  exit 1
fi
grep -q '"tariffMinorPerKWh":96' "$temp_dir/settings-put.json"
curl --silent --show-error --fail --cookie "$cookie_jar" \
  "$base_url/api/v1/history?from=$history_from&to=$history_to&resolution=minute" >"$temp_dir/history.json"
grep -q '"powerW":4321' "$temp_dir/history.json"
grep -q '"powerW":4322' "$temp_dir/history.json"
printf 'smoke: initial settings and history verified\n'

docker rm -f "$container_name" >/dev/null
start_container
printf 'smoke: recreated container ready\n'
curl --silent --show-error --fail --cookie "$cookie_jar" "$base_url/api/v1/auth/session" >"$temp_dir/session.json"
grep -q '"username":"SmokeAdmin"' "$temp_dir/session.json"
csrf="$(sed -n 's/.*"csrfToken":"\([^"]*\)".*/\1/p' "$temp_dir/session.json")"
[[ -n "$csrf" ]]
printf 'header = "X-CSRF-Token: %s"\n' "$csrf" >"$csrf_config"
chmod 0600 "$csrf_config"
unset csrf
curl --silent --show-error --fail --cookie "$cookie_jar" "$base_url/api/v1/settings" >"$temp_dir/settings.json"
grep -q '"tariffMinorPerKWh":96' "$temp_dir/settings.json"
curl --silent --show-error --fail --cookie "$cookie_jar" \
  "$base_url/api/v1/history?from=$history_from&to=$history_to&resolution=minute" >"$temp_dir/history-after.json"
grep -q '"powerW":4321' "$temp_dir/history-after.json"
grep -q '"powerW":4322' "$temp_dir/history-after.json"
printf 'smoke: persisted session, settings, and history verified\n'

deadline=$((SECONDS + 60))
while :; do
  curl --silent --show-error --fail "$base_url/health/components" >"$temp_dir/components.json"
  if grep -q '"logger":"offline"' "$temp_dir/components.json"; then
    break
  fi
  if (( SECONDS >= deadline )); then
    printf 'smoke: logger did not enter degraded state within 60 seconds\n' >&2
    exit 1
  fi
  sleep 0.5
done
curl --silent --show-error --fail "$base_url/health/ready" >"$temp_dir/ready-degraded.json"
grep -q '"status":"ready"' "$temp_dir/ready-degraded.json"
printf 'smoke: degraded logger readiness verified\n'

first_backup_status="$(curl --silent --show-error --cookie "$cookie_jar" --output "$temp_dir/first-backup.db" \
  --write-out '%{http_code}' "$base_url/api/v1/data/backup")"
if [[ "$first_backup_status" != "200" ]]; then
  printf 'smoke: first backup returned HTTP %s\n' "$first_backup_status" >&2
  exit 1
fi
second_backup_status="$(curl --silent --show-error --cookie "$cookie_jar" --dump-header "$temp_dir/backup.headers" \
  --output "$temp_dir/backup.db" --write-out '%{http_code}' "$base_url/api/v1/data/backup")"
if [[ "$second_backup_status" != "200" ]]; then
  printf 'smoke: second backup returned HTTP %s\n' "$second_backup_status" >&2
  exit 1
fi
grep -qi '^content-type: application/vnd.sqlite3' "$temp_dir/backup.headers"
grep -Eqi '^content-disposition: attachment; filename="helio-backup-[0-9]{8}-[0-9]{6}\.db"' "$temp_dir/backup.headers"
"$temp_dir/fixture-host" validate "$temp_dir/backup.db"
printf 'smoke: backup integrity and rows verified\n'

printf 'smoke: PASS\n'
