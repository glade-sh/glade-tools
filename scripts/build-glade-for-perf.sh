#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="${GLADE_TOOLS_PERF_BIN:-$repo_root/bin/glade-tools-perf}"

mkdir -p "$(dirname "$out")"

args=(-trimpath -o "$out")
if [[ -n "${PGO_PROFILE:-}" ]]; then
  args=(-trimpath -pgo="$PGO_PROFILE" -o "$out")
fi

go build "${args[@]}" "$repo_root/cmd/glade-tools"
printf '%s\n' "$out"
