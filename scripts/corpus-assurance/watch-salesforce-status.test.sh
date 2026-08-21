#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
watcher="$root/scripts/corpus-assurance/watch-salesforce-status.sh"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/glade-status-watch-test.XXXXXX")"
trap 'find "$tmp" -depth -delete' EXIT

printf '%s\n' '{"schemaVersion":1,"generatedAt":"2026-08-21T00:00:00Z","programStatus":"NOT DONE","completion":{"percent":0,"complete":0,"required":1,"remaining":1},"candidate":{"glade":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","tools":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"tiers":{},"salesforce":{"state":"not-started","outcomes":{}},"pipeline":{"phase":"test","status":"closed"},"machines":[],"action":{},"cleanup":{},"delivery":{}}' >"$tmp/fixture.json"
printf '%s\n' '#!/bin/sh' 'echo run >>"$COUNT_PATH"' 'cp "$FIXTURE_PATH" "$STATUS_PATH"' >"$tmp/refresh.sh"
chmod +x "$tmp/refresh.sh"

COUNT_PATH="$tmp/count" FIXTURE_PATH="$tmp/fixture.json" STATUS_PATH="$tmp/status.json" \
  "$watcher" --once --status "$tmp/status.json" --html "$tmp/status.html" -- "$tmp/refresh.sh"
test "$(wc -l <"$tmp/count" | tr -d ' ')" = 1
grep -F 'Salesforce proof status' "$tmp/status.html"

: >"$tmp/count"
COUNT_PATH="$tmp/count" FIXTURE_PATH="$tmp/fixture.json" STATUS_PATH="$tmp/status.json" \
  "$watcher" --interval 1 --status "$tmp/status.json" --html "$tmp/status.html" -- "$tmp/refresh.sh"
test "$(wc -l <"$tmp/count" | tr -d ' ')" = 1

if "$watcher" --interval 0 --status "$tmp/status.json" --html "$tmp/status.html" -- "$tmp/refresh.sh" 2>"$tmp/error"; then
  echo 'watcher accepted a zero interval' >&2
  exit 1
fi
grep -F 'interval must be a positive integer' "$tmp/error"

echo 'salesforce status watch tests passed'
