#!/usr/bin/env bash
set -euo pipefail

if (($# < 2)); then
  echo "usage: scripts/release-asset-upload.sh <tag> <asset> [asset...]" >&2
  exit 2
fi

TAG="$1"
shift
GH_BIN="${GH_BIN:-gh}"

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

declare -A seen_assets=()
for asset in "$@"; do
  if [[ ! -f "$asset" ]]; then
    echo "release asset does not exist or is not a regular file: $asset" >&2
    exit 1
  fi
  name="$(basename "$asset")"
  if [[ -n "${seen_assets[$name]:-}" ]]; then
    echo "duplicate release asset basename: $name" >&2
    exit 1
  fi
  seen_assets["$name"]=1
done

existing_assets="$("$GH_BIN" release view "$TAG" --json assets --jq '.assets[].name')"
for asset in "$@"; do
  name="$(basename "$asset")"
  if grep -Fqx "$name" <<<"$existing_assets"; then
    existing_dir="$(mktemp -d "${TMPDIR:-/tmp}/glade-release-asset.XXXXXX")"
    existing_path="$existing_dir/$name"
    if ! "$GH_BIN" release download "$TAG" --pattern "$name" --dir "$existing_dir"; then
      rmdir "$existing_dir" 2>/dev/null || true
      echo "could not download published release asset for checksum comparison: $name" >&2
      exit 1
    fi
    if [[ ! -f "$existing_path" ]]; then
      rmdir "$existing_dir" 2>/dev/null || true
      echo "published release asset download is missing expected file: $name" >&2
      exit 1
    fi
    if cmp -s "$asset" "$existing_path"; then
      rm -f "$existing_path"
      rmdir "$existing_dir"
      echo "release asset $name already has identical bytes; skipping"
      continue
    fi
    expected="$(sha256_file "$existing_path")"
    actual="$(sha256_file "$asset")"
    rm -f "$existing_path"
    rmdir "$existing_dir"
    echo "published asset differs for $name (published=$expected candidate=$actual); refusing overwrite" >&2
    exit 1
  fi
  "$GH_BIN" release upload "$TAG" "$asset"
done
