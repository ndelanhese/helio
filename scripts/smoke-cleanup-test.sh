#!/usr/bin/env bash
set -euo pipefail
umask 077

for command in docker mktemp cmp; do
  command -v "$command" >/dev/null 2>&1 || { printf 'smoke cleanup test: required command missing: %s\n' "$command" >&2; exit 1; }
done

temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/helio-smoke-cleanup.XXXXXX")"
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  rm -rf "$temp_dir"
  exit "$status"
}
trap cleanup EXIT INT TERM HUP

list_resources() {
  docker ps -a --filter 'name=helio-smoke-' --format 'container {{.Names}}'
  docker volume ls --filter 'name=helio-smoke-' --format 'volume {{.Name}}'
}

list_resources | LC_ALL=C sort >"$temp_dir/before"
pids=()
for index in 1 2; do
  HELIO_SMOKE_FAIL_AFTER_VOLUME=1 "$(dirname "$0")/smoke.sh" >"$temp_dir/run-$index.log" 2>&1 &
  pids+=("$!")
done
for index in 0 1; do
  if wait "${pids[$index]}"; then
    printf 'smoke cleanup test: forced failure %d unexpectedly succeeded\n' "$((index + 1))" >&2
    exit 1
  fi
  grep -q 'forced safe failure after volume creation' "$temp_dir/run-$((index + 1)).log"
done
list_resources | LC_ALL=C sort >"$temp_dir/after"
if ! cmp -s "$temp_dir/before" "$temp_dir/after"; then
  printf 'smoke cleanup test: parallel forced failures leaked Docker resources\n' >&2
  exit 1
fi
printf 'smoke cleanup test: PASS\n'
