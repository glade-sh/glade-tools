#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
	echo "usage: $0 <glade-binary> <glade-source-root>" >&2
	exit 2
fi

GLADE_BIN="$1"
GLADE_SOURCE_ROOT="$2"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SUMMARY="${TMPDIR:-/tmp}/release-local-apex-summary.json"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/glade-release-local-apex.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

[[ -x "$GLADE_BIN" ]] || { echo "Glade binary is not executable: $GLADE_BIN" >&2; exit 2; }
git -C "$GLADE_SOURCE_ROOT" rev-parse --verify HEAD >/dev/null
rm -f "$SUMMARY"

fixtures=(
	enterprise-composed:2
	org-like-runner:2
	files-email:2
	flow:1
	resources-labels:2
)

for fixture in "${fixtures[@]}"; do
	name="${fixture%:*}"
	project="$ROOT/testdata/local-tests/$name"
	"$GLADE_BIN" check --project "$project" --json --no-progress >"$WORK/$name-check.json"
	"$GLADE_BIN" test --project "$project" --json --no-progress >"$WORK/$name-test.json"
done

python3 - "$GLADE_BIN" "$GLADE_SOURCE_ROOT" "$ROOT" "$WORK" "$SUMMARY" "${fixtures[@]}" <<'PY'
import hashlib
import json
import os
import subprocess
import sys

binary, source_root, tools_root, work, output, *fixtures = sys.argv[1:]

def commit(root):
    return subprocess.check_output(
        ["git", "-C", root, "rev-parse", "HEAD"], text=True
    ).strip()

def read_report(path):
    with open(path, encoding="utf-8") as handle:
        return json.load(handle)

rows = []
for fixture in fixtures:
    name, expected_text = fixture.rsplit(":", 1)
    expected = int(expected_text)
    check = read_report(os.path.join(work, name + "-check.json"))
    test = read_report(os.path.join(work, name + "-test.json"))
    if check.get("status") != "passed" or check.get("exitCode") != 0:
        raise SystemExit(f"{name} check report did not pass")
    summary = test.get("summary", {})
    if (
        test.get("status") != "passed"
        or test.get("exitCode") != 0
        or summary.get("total") != expected
        or summary.get("passed") != expected
        or any(summary.get(key, 0) != 0 for key in (
            "failed", "errors", "compileErrors", "runtimeErrors", "unsupported"
        ))
    ):
        raise SystemExit(f"{name} expected {expected}/{expected} passing tests, got {summary}")
    rows.append({"name": name, "passed": expected, "total": expected})

with open(binary, "rb") as handle:
    binary_sha256 = hashlib.sha256(handle.read()).hexdigest()
receipt = {
    "schemaVersion": 1,
    "glade": {"binarySha256": binary_sha256, "commit": commit(source_root)},
    "tools": {"commit": commit(tools_root)},
    "fixtures": rows,
    "summary": {"passed": sum(row["passed"] for row in rows), "total": sum(row["total"] for row in rows)},
}
with open(output + ".tmp", "w", encoding="utf-8") as handle:
    json.dump(receipt, handle, indent=2, sort_keys=True)
    handle.write("\n")
os.replace(output + ".tmp", output)
print(output)
PY
