#!/usr/bin/env bash
set -euo pipefail
unset FORCE_COLOR NO_COLOR

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d "${TMPDIR:-/tmp}/helio-e2e.XXXXXX")"
server_pid=""
cleanup() {
  if [[ -n "$server_pid" ]]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf "$work"
}
trap cleanup EXIT HUP INT TERM

rm -rf "$root/web/test-results" "$root/web/playwright-report"
npm --prefix "$root/web" run build
(cd "$root" && go build -o "$work/helio-fakeapp" ./internal/fakeapp)
"$work/helio-fakeapp" >"$work/server.log" 2>&1 &
server_pid="$!"
for _ in {1..100}; do
  if curl --fail --silent http://127.0.0.1:4173/health/live >/dev/null; then break; fi
  if ! kill -0 "$server_pid" 2>/dev/null; then cat "$work/server.log" >&2; exit 1; fi
  sleep 0.1
done
curl --fail --silent http://127.0.0.1:4173/health/live >/dev/null

run() {
  (cd "$root/web" && HELIO_E2E_EXTERNAL_SERVER=1 npx playwright test --workers=1 --retries=0 --max-failures=1 "$@")
}

run --project=desktop-chromium
run --project=mobile-webkit e2e/acceptance.spec.ts e2e/auth-fidelity.spec.ts e2e/bootstrap.spec.ts
run --project=mobile-webkit e2e/history.spec.ts e2e/login.spec.ts e2e/now.spec.ts
run --project=mobile-webkit e2e/settings.spec.ts e2e/theme.spec.ts
