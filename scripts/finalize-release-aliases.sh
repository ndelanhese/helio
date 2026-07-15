#!/usr/bin/env bash
set -euo pipefail

for variable in GITHUB_REPOSITORY GITHUB_REF_NAME IMAGE_NAME IMMUTABLE_DIGEST; do
  if [[ -z "${!variable:-}" ]]; then
    printf 'release aliases: %s is required\n' "$variable" >&2
    exit 1
  fi
done
if [[ ! "$GITHUB_REF_NAME" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  printf 'release aliases: current tag is not strict stable SemVer\n' >&2
  exit 1
fi
if [[ ! "$IMMUTABLE_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]]; then
  printf 'release aliases: immutable digest is invalid\n' >&2
  exit 1
fi

temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/helio-release-aliases.XXXXXX")"
trap 'rm -rf "$temp_dir"' EXIT
releases="$temp_dir/releases.json"
gh release list --repo "$GITHUB_REPOSITORY" --limit 1000 --exclude-drafts --exclude-pre-releases \
  --json tagName,isDraft,isPrerelease,publishedAt > "$releases"

decision="$(python3 - "$GITHUB_REF_NAME" "$releases" <<'PY'
import json
import re
import sys

current_tag, releases_path = sys.argv[1:]
pattern = re.compile(r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")

def version(tag):
    match = pattern.fullmatch(tag)
    return tuple(int(part) for part in match.groups()) if match else None

with open(releases_path, encoding="utf-8") as source:
    records = json.load(source)
if not isinstance(records, list):
    raise SystemExit("release aliases: release query did not return a list")

stable = {}
for record in records:
    if not isinstance(record, dict):
        continue
    if record.get("isDraft") or record.get("isPrerelease") or not record.get("publishedAt"):
        continue
    tag = record.get("tagName")
    parsed = version(tag) if isinstance(tag, str) else None
    if parsed is not None:
        stable[tag] = parsed

if current_tag not in stable:
    raise SystemExit("release aliases: current tag is not a published stable release")
current = stable[current_tag]
global_max = max(stable.values())
line_max = max(value for value in stable.values() if value[:2] == current[:2])
print("true" if current == line_max else "false", "true" if current == global_max else "false", f"{current[0]}.{current[1]}")
PY
)"
read -r update_line update_latest line_alias <<< "$decision"

tags=()
if [[ "$update_line" == true ]]; then
  tags+=(--tag "${IMAGE_NAME}:${line_alias}")
fi
if [[ "$update_latest" == true ]]; then
  tags+=(--tag "${IMAGE_NAME}:latest")
fi
if [[ "${#tags[@]}" -eq 0 ]]; then
  printf 'aliases=unchanged\n'
  exit 0
fi

docker buildx imagetools create "${tags[@]}" "${IMAGE_NAME}@${IMMUTABLE_DIGEST}"
printf 'aliases=ensured\n'
