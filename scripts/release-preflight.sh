#!/usr/bin/env bash
set -euo pipefail

for variable in GITHUB_REPOSITORY GITHUB_REF_NAME IMAGE_NAME; do
  if [[ -z "${!variable:-}" ]]; then
    printf 'release preflight: %s is required\n' "$variable" >&2
    exit 1
  fi
done

emit() {
  printf '%s=%s\n' "$1" "$2"
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    printf '%s=%s\n' "$1" "$2" >> "$GITHUB_OUTPUT"
  fi
}

temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/helio-release-preflight.XXXXXX")"
trap 'rm -rf "$temp_dir"' EXIT

release_exists=false
release_body=""
if release_body="$(gh release view "$GITHUB_REF_NAME" --repo "$GITHUB_REPOSITORY" --json body --jq .body 2>"$temp_dir/release-error")"; then
  release_exists=true
elif grep -Eqi 'HTTP 404' "$temp_dir/release-error"; then
  release_exists=false
else
  printf 'release preflight: GitHub Release query failed: ' >&2
  sed -n '1p' "$temp_dir/release-error" >&2
  exit 1
fi

image_exists=false
image_digest=""
if image_digest="$(docker buildx imagetools inspect "${IMAGE_NAME}:${GITHUB_REF_NAME}" --format '{{.Manifest.Digest}}' 2>"$temp_dir/image-error")"; then
  if [[ ! "$image_digest" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    printf 'release preflight: registry returned an invalid digest\n' >&2
    exit 1
  fi
  image_exists=true
elif grep -Eqi 'manifest unknown|no such manifest|manifest[^[:cntrl:]]*not found|not found[^[:cntrl:]]*manifest' "$temp_dir/image-error"; then
  image_exists=false
else
  printf 'release preflight: registry query failed: ' >&2
  sed -n '1p' "$temp_dir/image-error" >&2
  exit 1
fi

if [[ "$release_exists" == false && "$image_exists" == false ]]; then
  emit state new
  exit 0
fi

if [[ "$release_exists" != "$image_exists" ]]; then
  printf 'release preflight: inconsistent state; immutable image and GitHub Release must both exist or both be absent\n' >&2
  exit 1
fi

recorded_digests="$(
  printf '%s\n' "$release_body" |
    sed -nE 's/^Helio-Image-Digest:[[:space:]]*(sha256:[0-9a-f]{64})[[:space:]]*$/\1/p'
)"
recorded_count="$(printf '%s\n' "$recorded_digests" | awk 'NF { count++ } END { print count + 0 }')"
if [[ "$recorded_count" -ne 1 ]]; then
  printf 'release preflight: existing release must contain exactly one Helio-Image-Digest metadata line\n' >&2
  exit 1
fi
if [[ "$recorded_digests" != "$image_digest" ]]; then
  printf 'release preflight: existing release digest mismatch with immutable registry tag\n' >&2
  exit 1
fi

emit state existing
emit digest "$image_digest"
