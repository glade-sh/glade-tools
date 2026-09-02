#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 2 ]]; then
	echo "usage: scripts/salesforce-product-tests.sh <glade-root> <out-dir>" >&2
	exit 2
fi

glade_root="$1"
out_dir="$2"
if [[ ! -d "$glade_root" ]]; then
	echo "Glade root is not a directory" >&2
	exit 2
fi
glade_root="$(cd "$glade_root" && pwd)"
mkdir -p "$out_dir"
out_dir="$(cd "$out_dir" && pwd)"
if [[ "${LC_ALL:-}" != C || "${GLADE_LWC_COMPILE:-}" != 1 || "${GLADE_ROOT:-}" != "$glade_root" ]]; then
	echo "Salesforce product-test environment is not exact" >&2
	exit 2
fi
if [[ ! -f "$glade_root/scripts/internal/cishard/main.go" ]]; then
	echo "Glade shard planner is missing" >&2
	exit 2
fi

package_name="github.com/glade-sh/glade/internal/apextest"
events="$out_dir/product-tests.jsonl"
evidence="$out_dir/product-test-evidence"
if [[ -e "$events" || -e "$evidence" ]]; then
	echo "Salesforce product-test output already exists" >&2
	exit 2
fi
mkdir "$evidence"

binary="$evidence/apextest.test"
complete=0
cleanup() {
	rm -f -- "$binary"
	if [[ "$complete" -ne 1 ]]; then
		rm -f -- "$events"
	fi
}
trap cleanup EXIT INT TERM

jq -S -c -n '{schemaVersion:1,status:"fail",stage:"started",shardCount:16}' >"$evidence/validation.json"

all_packages="$evidence/all-packages.txt"
non_apex_packages="$evidence/non-apextest-packages.txt"
non_apex_events="$evidence/non-apextest.jsonl"
go -C "$glade_root" list -f '{{.ImportPath}}' ./... >"$all_packages"
python3 - "$all_packages" "$non_apex_packages" "$package_name" <<'PY'
import re, sys

source, output, apex = sys.argv[1:]
packages = open(source, encoding="utf-8").read().splitlines()
if not packages or packages.count(apex) != 1 or len(packages) != len(set(packages)):
    raise SystemExit("invalid Glade package discovery")
if any(not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._~/-]*", package) for package in packages):
    raise SystemExit("invalid Glade package identity")
selected = [package for package in packages if package != apex]
if not selected:
    raise SystemExit("Glade non-apextest package set is empty")
with open(output, "w", encoding="utf-8") as target:
    target.write("\n".join(selected) + "\n")
PY

non_apex=()
while IFS= read -r package; do
	non_apex+=("$package")
done <"$non_apex_packages"
go -C "$glade_root" test -json -count=1 -p 1 -timeout=30m "${non_apex[@]}" >"$non_apex_events"

go -C "$glade_root" test -c -o "$binary" ./internal/apextest
if [[ ! -x "$binary" ]]; then
	echo "compiled Apex test binary is missing" >&2
	exit 1
fi
binary_sha256="$(shasum -a 256 "$binary" | awk '{print $1}')"
binary_size="$(wc -c <"$binary" | tr -d '[:space:]')"
[[ "$binary_sha256" =~ ^[0-9a-f]{64}$ && "$binary_size" =~ ^[1-9][0-9]*$ ]]

discovery_raw="$evidence/apextest-discovery-raw.txt"
discovery="$evidence/apextest-discovery.txt"
"$binary" -test.list '^Test' >"$discovery_raw"
python3 - "$discovery_raw" "$discovery" <<'PY'
import re, sys

source, output = sys.argv[1:]
names = open(source, encoding="utf-8").read().splitlines()
if not names or len(names) != len(set(names)):
    raise SystemExit("invalid Apex test discovery")
if any(not re.fullmatch(r"Test[A-Za-z0-9_]*", name) for name in names):
    raise SystemExit("invalid Apex test name")
with open(output, "w", encoding="utf-8") as target:
    target.write("\n".join(sorted(names)) + "\n")
PY

plan="$evidence/apextest-plan.json"
go -C "$glade_root" run ./scripts/internal/cishard \
	--package "$package_name" \
	--shards 16 \
	--tests "$discovery" >"$plan"
python3 - "$discovery" "$plan" "$evidence" "$package_name" <<'PY'
import json, os, re, sys

discovery_path, plan_path, evidence, package = sys.argv[1:]
names = open(discovery_path, encoding="utf-8").read().splitlines()
with open(plan_path, encoding="utf-8") as source:
    plan = json.load(source)
if not isinstance(plan, dict) or set(plan) != {"version", "package", "historyUsed", "shards"}:
    raise SystemExit("invalid Apex shard plan schema")
if type(plan["version"]) is not int or plan["version"] != 1 or plan["package"] != package or not isinstance(plan["historyUsed"], bool):
    raise SystemExit("invalid Apex shard plan identity")
shards = plan["shards"]
if not isinstance(shards, list) or len(shards) != 16:
    raise SystemExit("Apex shard plan must contain exactly 16 shards")
union = []
for index, shard in enumerate(shards):
    if not isinstance(shard, dict) or set(shard) != {"index", "tests", "estimatedDurationMillis", "regex"}:
        raise SystemExit(f"invalid Apex shard {index} schema")
    tests = shard["tests"]
    expected_regex = "^(?:" + "|".join(re.escape(name) for name in tests) + ")$"
    if type(shard["index"]) is not int or shard["index"] != index or not isinstance(tests, list) or not tests or tests != sorted(tests):
        raise SystemExit(f"invalid Apex shard {index} selection")
    if any(not isinstance(name, str) or not re.fullmatch(r"Test[A-Za-z0-9_]*", name) for name in tests):
        raise SystemExit(f"invalid Apex shard {index} test")
    if type(shard["estimatedDurationMillis"]) is not int or shard["estimatedDurationMillis"] < 0 or shard["regex"] != expected_regex:
        raise SystemExit(f"invalid Apex shard {index} metadata")
    union.extend(tests)
    shard_dir = os.path.join(evidence, f"shard-{index:02d}")
    os.mkdir(shard_dir)
    with open(os.path.join(shard_dir, "selection.json"), "w", encoding="utf-8") as target:
        json.dump(shard, target, sort_keys=True, separators=(",", ":"))
        target.write("\n")
    with open(os.path.join(shard_dir, "regex.txt"), "w", encoding="utf-8") as target:
        target.write(shard["regex"] + "\n")
if len(union) != len(set(union)) or sorted(union) != names:
    raise SystemExit("Apex shard union does not exactly match discovery")
PY

for index in {0..15}; do
	shard_dir="$evidence/shard-$(printf '%02d' "$index")"
	regex="$(<"$shard_dir/regex.txt")"
	current_sha256="$(shasum -a 256 "$binary" | awk '{print $1}')"
	if [[ "$current_sha256" != "$binary_sha256" ]]; then
		echo "compiled Apex test binary changed before shard $index" >&2
		exit 1
	fi
	set +e
	"$binary" -test.v=test2json -test.count=1 -test.timeout=30m -test.run="$regex" | \
		go -C "$glade_root" tool test2json -p "$package_name" >"$shard_dir/events.jsonl"
	statuses=("${PIPESTATUS[@]}")
	set -e
	if [[ "${statuses[0]}" -ne 0 ]]; then
		exit "${statuses[0]}"
	fi
	if [[ "${statuses[1]}" -ne 0 ]]; then
		exit "${statuses[1]}"
	fi
done

rm -f -- "$binary"
if [[ -e "$binary" ]]; then
	echo "compiled Apex test binary remains after cleanup" >&2
	exit 1
fi

python3 - "$out_dir" "$evidence" "$events" "$package_name" "$binary_sha256" "$binary_size" <<'PY'
import hashlib, json, os, sys, tempfile

out_dir, evidence, events_path, apex_package, binary_sha, binary_size_text = sys.argv[1:]

def reject_duplicates(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError(f"duplicate JSON key {key!r}")
        value[key] = item
    return value

def load_json(path):
    with open(path, encoding="utf-8") as source:
        return json.load(source, object_pairs_hook=reject_duplicates)

def digest(path):
    value = hashlib.sha256()
    with open(path, "rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()

def relative(path):
    result = os.path.relpath(path, out_dir)
    if result == ".." or result.startswith("../") or os.path.isabs(result):
        raise ValueError("evidence path escapes output directory")
    return result

packages_path = os.path.join(evidence, "non-apextest-packages.txt")
all_packages_path = os.path.join(evidence, "all-packages.txt")
non_apex_events = os.path.join(evidence, "non-apextest.jsonl")
discovery_path = os.path.join(evidence, "apextest-discovery.txt")
plan_path = os.path.join(evidence, "apextest-plan.json")
packages = open(packages_path, encoding="utf-8").read().splitlines()
if not packages or len(packages) != len(set(packages)) or apex_package in packages:
    raise SystemExit("invalid non-apextest package evidence")

package_terminals = {}
with open(non_apex_events, encoding="utf-8") as source:
    for number, line in enumerate(source, 1):
        if not line.strip():
            raise SystemExit(f"empty non-apextest event at line {number}")
        event = json.loads(line, object_pairs_hook=reject_duplicates)
        package = event.get("Package")
        if package not in packages:
            raise SystemExit("non-apextest event has unexpected package")
        if not event.get("Test") and event.get("Action") in {"pass", "fail", "skip"}:
            package_terminals.setdefault(package, []).append(event["Action"])
if set(package_terminals) != set(packages) or any(actions != ["pass"] for actions in package_terminals.values()):
    raise SystemExit("non-apextest package terminal set is incomplete")

discovery = open(discovery_path, encoding="utf-8").read().splitlines()
plan = load_json(plan_path)
shards = plan.get("shards") if isinstance(plan, dict) else None
if not isinstance(shards, list) or len(shards) != 16:
    raise SystemExit("invalid final Apex shard plan")

artifacts = []
for path in [all_packages_path, packages_path, non_apex_events, os.path.join(evidence, "apextest-discovery-raw.txt"), discovery_path, plan_path]:
    artifacts.append({"path": relative(path), "sha256": digest(path)})

passed = []
shard_summaries = []
shard_event_paths = []
for index, shard in enumerate(shards):
    shard_dir = os.path.join(evidence, f"shard-{index:02d}")
    selection_path = os.path.join(shard_dir, "selection.json")
    regex_path = os.path.join(shard_dir, "regex.txt")
    shard_events = os.path.join(shard_dir, "events.jsonl")
    selection = load_json(selection_path)
    if selection != shard:
        raise SystemExit(f"Apex shard {index} selection differs from plan")
    expected = shard["tests"]
    terminals = {}
    package_terminals = []
    with open(shard_events, encoding="utf-8") as source:
        for number, line in enumerate(source, 1):
            if not line.strip():
                raise SystemExit(f"empty Apex shard {index} event at line {number}")
            event = json.loads(line, object_pairs_hook=reject_duplicates)
            if event.get("Package") != apex_package:
                raise SystemExit(f"Apex shard {index} event has wrong package")
            name, action = event.get("Test"), event.get("Action")
            if isinstance(name, str) and "/" not in name and action in {"pass", "fail", "skip"}:
                terminals.setdefault(name, []).append(action)
            if not name and action in {"pass", "fail", "skip"}:
                package_terminals.append(action)
    if set(terminals) != set(expected) or any(terminals[name] != ["pass"] for name in expected) or package_terminals != ["pass"]:
        raise SystemExit(f"Apex shard {index} does not contain one passing terminal per selected test")
    passed.extend(expected)
    shard_event_paths.append(shard_events)
    for path in [selection_path, regex_path, shard_events]:
        artifacts.append({"path": relative(path), "sha256": digest(path)})
    shard_summaries.append({"index": index, "testCount": len(expected), "eventsPath": relative(shard_events), "eventsSHA256": digest(shard_events)})
if len(passed) != len(set(passed)) or sorted(passed) != discovery:
    raise SystemExit("passing Apex shard union does not exactly match discovery")

descriptor, temporary = tempfile.mkstemp(prefix=".product-tests.", dir=out_dir)
try:
    with os.fdopen(descriptor, "wb") as target:
        for path in [non_apex_events, *shard_event_paths]:
            with open(path, "rb") as source:
                data = source.read()
            if data and not data.endswith(b"\n"):
                raise SystemExit("product-test event artifact is not newline terminated")
            target.write(data)
    os.replace(temporary, events_path)
except BaseException:
    try:
        os.unlink(temporary)
    except FileNotFoundError:
        pass
    raise

canonical_names = "".join(name + "\n" for name in discovery).encode()
summary = {
    "schemaVersion": 1,
    "status": "pass",
    "shardCount": 16,
    "nonApex": {"packageCount": len(packages), "allPackagesPath": relative(all_packages_path), "allPackagesSHA256": digest(all_packages_path), "packagesPath": relative(packages_path), "packagesSHA256": digest(packages_path), "eventsPath": relative(non_apex_events), "eventsSHA256": digest(non_apex_events)},
    "apexTestBinary": {"sha256": binary_sha, "sizeBytes": int(binary_size_text), "removed": True},
    "apexDiscovery": {"path": relative(discovery_path), "count": len(discovery), "sha256": digest(discovery_path)},
    "apexPlan": {"path": relative(plan_path), "sha256": digest(plan_path)},
    "shards": shard_summaries,
    "union": {"valid": True, "count": len(discovery), "namesSHA256": hashlib.sha256(canonical_names).hexdigest()},
    "artifacts": artifacts,
    "testEvents": {"path": relative(events_path), "sha256": digest(events_path)},
}
validation_path = os.path.join(evidence, "validation.json")
descriptor, temporary = tempfile.mkstemp(prefix=".validation.", dir=evidence, text=True)
try:
    with os.fdopen(descriptor, "w", encoding="utf-8") as target:
        json.dump(summary, target, sort_keys=True, separators=(",", ":"))
        target.write("\n")
    os.replace(temporary, validation_path)
except BaseException:
    try:
        os.unlink(temporary)
    except FileNotFoundError:
        pass
    raise
PY

complete=1
echo "Glade product tests passed in 16 sequential Apex shards." >&2
