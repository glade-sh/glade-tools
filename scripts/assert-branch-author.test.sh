#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
guard="$root/scripts/assert-branch-author.sh"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/glade-author-guard-test.XXXXXX")"
trap 'find "$tmp" -depth -delete' EXIT

git -C "$tmp" init -q -b main
git -C "$tmp" config user.name mattsimonis
git -C "$tmp" config user.email 720686+mattsimonis@users.noreply.github.com
git -C "$tmp" commit -q --allow-empty -m base
git -C "$tmp" switch -q -c feature
git -C "$tmp" commit -q --allow-empty -m good

(cd "$tmp" && "$guard" --base main --name mattsimonis \
  --email 720686+mattsimonis@users.noreply.github.com)

git -C "$tmp" -c user.name='Other User' -c user.email=other@example.invalid \
  commit -q --allow-empty -m bad
if (cd "$tmp" && "$guard" --base main --name mattsimonis \
  --email 720686+mattsimonis@users.noreply.github.com) 2>"$tmp/bad.err"; then
  echo 'author guard accepted an unexpected author' >&2
  exit 1
fi
grep -Fx 'unexpected branch author: Other User <other@example.invalid>' "$tmp/bad.err"

git -C "$tmp" switch -q main
if (cd "$tmp" && "$guard" --base main --name mattsimonis \
  --email 720686+mattsimonis@users.noreply.github.com) 2>"$tmp/empty.err"; then
  echo 'author guard accepted an empty branch range' >&2
  exit 1
fi
grep -Fx 'empty branch range: main..HEAD' "$tmp/empty.err"

echo 'branch author guard tests passed'
