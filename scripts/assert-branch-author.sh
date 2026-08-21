#!/usr/bin/env bash
set -euo pipefail

base=''
expected_name=''
expected_email=''
while (($#)); do
  case "$1" in
    --base) base="${2:-}"; shift 2 ;;
    --name) expected_name="${2:-}"; shift 2 ;;
    --email) expected_email="${2:-}"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [[ -z "$base" || -z "$expected_name" || -z "$expected_email" ]]; then
  echo 'usage: assert-branch-author.sh --base REF --name NAME --email EMAIL' >&2
  exit 2
fi

authors="$(git log --format='%an%x09%ae' "$base"..HEAD)"
if [[ -z "$authors" ]]; then
  echo "empty branch range: $base..HEAD" >&2
  exit 1
fi

while IFS=$'\t' read -r name email; do
  if [[ "$name" != "$expected_name" || "$email" != "$expected_email" ]]; then
    echo "unexpected branch author: $name <$email>" >&2
    exit 1
  fi
done <<<"$authors"
