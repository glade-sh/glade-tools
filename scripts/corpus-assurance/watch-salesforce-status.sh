#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 [--once] [--interval SECONDS] --status STATUS.json --html STATUS.html -- REFRESH_COMMAND [ARG ...]" >&2
  exit 2
}

interval=30
once=false
status=""
html=""
while (($#)); do
  case "$1" in
    --once) once=true; shift ;;
    --interval) interval="${2:-}"; shift 2 ;;
    --status) status="${2:-}"; shift 2 ;;
    --html) html="${2:-}"; shift 2 ;;
    --) shift; break ;;
    *) usage ;;
  esac
done

[[ "$interval" =~ ^[1-9][0-9]*$ ]] || { echo "interval must be a positive integer" >&2; exit 2; }
[[ -n "$status" && -n "$html" && $# -gt 0 ]] || usage

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
while true; do
  "$@"
  python3 "$root/scripts/render-salesforce-dashboard.py" --status "$status" --output "$html"
  state="$(jq -r '.pipeline.status // ""' "$status")"
  if [[ "$once" == true || "$state" == closed || "$state" == blocked ]]; then
    break
  fi
  sleep "$interval"
done
