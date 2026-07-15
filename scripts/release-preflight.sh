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
else
  release_error="$(sed 's/\r$//' "$temp_dir/release-error")"
  case "$release_error" in
    'release not found'|'gh: release not found (HTTP 404)'|'gh: Not Found (HTTP 404)') release_exists=false ;;
    *)
      printf 'release preflight: GitHub Release query failed: %s\n' "$release_error" >&2
      exit 1
      ;;
  esac
fi

image_exists=false
image_digest=""
image_ref="${IMAGE_NAME}:${GITHUB_REF_NAME}"
if image_digest="$(docker buildx imagetools inspect "$image_ref" --format '{{.Manifest.Digest}}' 2>"$temp_dir/image-error")"; then
  if [[ ! "$image_digest" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    printf 'release preflight: registry returned an invalid digest\n' >&2
    exit 1
  fi
  image_exists=true
else
  image_error="$(sed 's/\r$//' "$temp_dir/image-error")"
  case "$image_error" in
    "ERROR: ${image_ref}: not found"|'manifest unknown'|"manifest unknown: ${image_ref}"|"no such manifest: ${image_ref}") image_exists=false ;;
    *)
      printf 'release preflight: registry query failed: %s\n' "$image_error" >&2
      exit 1
      ;;
  esac
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
