#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
	echo "usage: $0 <glade-binary> <glade-source-root>" >&2
	exit 2
fi

SUMMARY="${TMPDIR:-/tmp}/release-local-apex-summary.json"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# Keep the advisory lock open across exec and in children. A surviving candidate
# still excludes another writer, and the OS releases it after the last exit.
# The lock file must not be unlinked: waiters must always lock the same inode.
exec python3 -c '
import fcntl
import os
import sys

summary, root, script, *args = sys.argv[1:]
fd = os.open(summary + ".lockfile", os.O_CREAT | os.O_RDWR, 0o600)
try:
    fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
except BlockingIOError:
    sys.exit("local Apex gate already running for " + summary)
os.set_inheritable(fd, True)
os.execvp("bash", ["bash", "-c", script, "--", summary, root, *args])
' "$SUMMARY" "$ROOT" "$(cat <<'GATE'
set -euo pipefail
SUMMARY="$1"
ROOT="$2"
GLADE_BIN="$3"
GLADE_SOURCE_ROOT="$4"
rm -f "$SUMMARY"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/glade-release-local-apex.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

[[ -x "$GLADE_BIN" ]] || { echo "Glade binary is not executable: $GLADE_BIN" >&2; exit 2; }
GLADE_HEAD="$(git -C "$GLADE_SOURCE_ROOT" rev-parse --verify HEAD)"
worktree_status="$(git -C "$GLADE_SOURCE_ROOT" status --porcelain)" || { echo "cannot verify glade worktree status" >&2; exit 1; }
[[ -z "$worktree_status" ]] || { echo "glade worktree must be clean" >&2; exit 1; }
binary_info="$(go version -m "$GLADE_BIN" 2>/dev/null)" || { echo "cannot read glade binary build identity" >&2; exit 1; }
binary_revision="$(printf '%s\n' "$binary_info" | sed -n 's/^[[:space:]]*build[[:space:]]*vcs\.revision=//p')"
binary_modified="$(printf '%s\n' "$binary_info" | sed -n 's/^[[:space:]]*build[[:space:]]*vcs\.modified=//p')"
[[ "$binary_revision" == "$GLADE_HEAD" && "$binary_modified" == false ]] || { echo "glade candidate is not built from clean commit $GLADE_HEAD" >&2; exit 1; }

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

python3 - "$GLADE_BIN" "$GLADE_HEAD" "$ROOT" "$WORK" "$SUMMARY" "${fixtures[@]}" <<'PY'
import hashlib
import json
import os
import subprocess
import sys

binary, source_commit, tools_root, work, output, *fixtures = sys.argv[1:]

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
    "glade": {"binarySha256": binary_sha256, "commit": source_commit},
    "tools": {"commit": commit(tools_root)},
    "fixtures": rows,
    "summary": {"passed": sum(row["passed"] for row in rows), "total": sum(row["total"] for row in rows)},
}
staged = os.path.join(work, "receipt.json")
with open(staged, "w", encoding="utf-8") as handle:
    json.dump(receipt, handle, indent=2, sort_keys=True)
    handle.write("\n")
os.replace(staged, output)
print(output)
PY
GATE
)" "$@"
