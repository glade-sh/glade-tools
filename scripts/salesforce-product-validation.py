import hashlib, json, os, sys, tempfile

out_dir, evidence, events_path, apex_package, binary_sha, binary_size_text = sys.argv[1:]

# DataWeave is excluded (D45). The caller passes the tests Glade's own CI defers when no
# DataWeave toolchain is installed; they may skip only with this exact output, and only here.
DEFERRED_MESSAGE = "requires explicitly installed DataWeave toolchain"
DEFERRED = frozenset(n for n in os.environ.get("SALESFORCE_DEFERRED_APEX_TESTS", "").split(",") if n)

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
packages_with_tests = set()
with open(non_apex_events, encoding="utf-8") as source:
    for number, line in enumerate(source, 1):
        if not line.strip():
            raise SystemExit(f"empty non-apextest event at line {number}")
        event = json.loads(line, object_pairs_hook=reject_duplicates)
        package = event.get("Package")
        if package not in packages:
            raise SystemExit("non-apextest event has unexpected package")
        if event.get("Test"):
            packages_with_tests.add(package)
        if not event.get("Test") and event.get("Action") in {"pass", "fail", "skip"}:
            package_terminals.setdefault(package, []).append(event["Action"])
if set(package_terminals) != set(packages) or any(actions not in (["pass"], ["skip"]) for actions in package_terminals.values()):
    raise SystemExit("non-apextest package terminal set is incomplete")
if any(package_terminals[package] == ["skip"] and package in packages_with_tests for package in packages):
    raise SystemExit("non-apextest package skip contains test events")

discovery = open(discovery_path, encoding="utf-8").read().splitlines()
plan = load_json(plan_path)
shards = plan.get("shards") if isinstance(plan, dict) else None
if not isinstance(shards, list) or len(shards) != 16:
    raise SystemExit("invalid final Apex shard plan")

artifacts = []
for path in [all_packages_path, packages_path, non_apex_events, os.path.join(evidence, "apextest-discovery-raw.txt"), discovery_path, plan_path]:
    artifacts.append({"path": relative(path), "sha256": digest(path)})

passed = []
deferred_skipped = []
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
    outputs = {}
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
            if isinstance(name, str) and "/" not in name and action == "output":
                outputs.setdefault(name, []).append(event.get("Output", ""))
            if not name and action in {"pass", "fail", "skip"}:
                package_terminals.append(action)
    deferred_here = [name for name in expected if terminals.get(name) == ["skip"] and name in DEFERRED and DEFERRED_MESSAGE in "".join(outputs.get(name, []))]
    if set(terminals) != set(expected) or any(terminals[name] != ["pass"] and name not in deferred_here for name in expected) or package_terminals != ["pass"]:
        raise SystemExit(f"Apex shard {index} does not contain one passing terminal per selected test")
    passed.extend(name for name in expected if name not in deferred_here)
    deferred_skipped.extend(deferred_here)
    shard_event_paths.append(shard_events)
    for path in [selection_path, regex_path, shard_events]:
        artifacts.append({"path": relative(path), "sha256": digest(path)})
    shard_summaries.append({"index": index, "testCount": len(expected), "eventsPath": relative(shard_events), "eventsSHA256": digest(shard_events)})
if len(passed) != len(set(passed)) or sorted(passed + deferred_skipped) != discovery:
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
