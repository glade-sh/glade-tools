#!/usr/bin/env python3
"""Generate one explicit Salesforce release-note route per inventory document."""

import argparse
import json
import os
import re
import tempfile
from pathlib import Path


INVENTORY_KEYS = {"schemaVersion", "totalFiles", "totalMembers", "namespaces", "documents"}
POLICY_KEYS = {"schemaVersion", "previousRelease", "currentRelease", "inventoryDigest", "branchDefaults", "routeOverrides"}
ROUTE_KEYS = {"sourcePath", "behaviorIds", "surfaceIds", "outOfScopeReason"}


def article_filename(topic_id):
    stem = topic_id.removeprefix("release-notes.").removesuffix(".htm")
    return re.sub(r"[^A-Za-z0-9_.-]", "_", stem) + ".md"


def read_json(path):
    try:
        return json.loads(Path(path).read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise ValueError(f"cannot read {path}: {error}") from error


def object_with_keys(value, label, required, allowed=None):
    if not isinstance(value, dict):
        raise ValueError(f"{label} must be an object")
    unknown = set(value) - (allowed if allowed is not None else required)
    missing = required - set(value)
    if unknown or missing:
        detail = ", ".join(sorted([*(f"unknown {key}" for key in unknown), *(f"missing {key}" for key in missing)]))
        raise ValueError(f"{label} keys: {detail}")
    return value


def nonblank(value, label):
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"{label} must be a nonblank string")
    return value


def unique_paths(paths, label):
    if not paths:
        raise ValueError(f"{label} SourcePath set is empty")
    if len(paths) != len(set(paths)):
        raise ValueError(f"{label} SourcePath set has duplicates")
    return set(paths)


def inventory_paths(inventory):
    object_with_keys(inventory, "inventory", INVENTORY_KEYS)
    if inventory["schemaVersion"] != 1:
        raise ValueError("inventory schemaVersion must be 1")
    documents = inventory["documents"]
    if not isinstance(documents, list) or inventory["totalFiles"] != len(documents):
        raise ValueError("inventory totalFiles must equal documents length")
    paths = []
    for index, document in enumerate(documents):
        if not isinstance(document, dict):
            raise ValueError(f"inventory document {index} must be an object")
        paths.append(nonblank(document.get("sourcePath"), f"inventory document {index} sourcePath"))
    return unique_paths(paths, "inventory")


def toc_routes(toc):
    if not isinstance(toc, dict) or toc.get("schemaVersion") != 1:
        raise ValueError("TOC schemaVersion must be 1")
    entries = toc.get("entries")
    if not isinstance(entries, list):
        raise ValueError("TOC entries must be an array")
    routes = {}
    for index, entry in enumerate(entries):
        if not isinstance(entry, dict):
            raise ValueError(f"TOC entry {index} must be an object")
        topic_id = nonblank(entry.get("topicId"), f"TOC entry {index} topicId")
        ancestors = entry.get("ancestorTopicIds")
        if not isinstance(ancestors, list) or not all(isinstance(item, str) and item for item in ancestors):
            raise ValueError(f"TOC entry {index} ancestorTopicIds must be a string array")
        path = article_filename(topic_id)
        if path in routes:
            raise ValueError("TOC SourcePath set has duplicates")
        routes[path] = ancestors[1] if len(ancestors) >= 2 else "__root__"
    unique_paths(list(routes), "TOC")
    return routes


def route_value(value, label):
    object_with_keys(value, label, set(), {"requireExplicit", "outOfScopeReason"})
    if set(value) == {"requireExplicit"} and value["requireExplicit"] is True:
        return None
    if set(value) == {"outOfScopeReason"}:
        return nonblank(value["outOfScopeReason"], f"{label} outOfScopeReason")
    raise ValueError(f"{label} must be requireExplicit true or a nonblank outOfScopeReason")


def ids(value, label):
    if not isinstance(value, list) or not value or any(not isinstance(item, str) or not item.strip() for item in value):
        raise ValueError(f"{label} must be a nonempty string array")
    if len(value) != len(set(value)):
        raise ValueError(f"{label} has duplicates")
    return value


def parse_policy(policy, source_paths, branches):
    object_with_keys(policy, "policy", POLICY_KEYS)
    if policy["schemaVersion"] != 1:
        raise ValueError("policy schemaVersion must be 1")
    for key in ("previousRelease", "currentRelease", "inventoryDigest"):
        nonblank(policy[key], f"policy {key}")
    defaults = policy["branchDefaults"]
    if not isinstance(defaults, dict):
        raise ValueError("policy branchDefaults must be an object")
    expected_branches = {"__root__", *branches}
    if set(defaults) != expected_branches:
        detail = ", ".join(sorted([*(f"missing {key}" for key in expected_branches - set(defaults)), *(f"stale {key}" for key in set(defaults) - expected_branches)]))
        raise ValueError(f"branchDefaults keys: {detail}")
    defaults = {branch: route_value(value, f"branch policy {branch}") for branch, value in defaults.items()}

    overrides = {}
    if not isinstance(policy["routeOverrides"], list):
        raise ValueError("policy routeOverrides must be an array")
    for index, route in enumerate(policy["routeOverrides"]):
        object_with_keys(route, f"route override {index}", {"sourcePath"}, ROUTE_KEYS)
        path = nonblank(route["sourcePath"], f"route override {index} sourcePath")
        if path in overrides:
            raise ValueError(f"duplicate route override sourcePath {path}")
        if path not in source_paths:
            raise ValueError(f"stale route override sourcePath {path}")
        has_ids = False
        result = {"sourcePath": path}
        for key in ("behaviorIds", "surfaceIds"):
            if key in route:
                result[key] = ids(route[key], f"route override {index} {key}")
                has_ids = True
        if "outOfScopeReason" in route:
            if has_ids:
                raise ValueError(f"route override {index} must choose IDs or outOfScopeReason")
            result["outOfScopeReason"] = nonblank(route["outOfScopeReason"], f"route override {index} outOfScopeReason")
        elif not has_ids:
            raise ValueError(f"route override {index} must choose IDs or outOfScopeReason")
        overrides[path] = result
    return defaults, overrides


def generate(inventory_path, toc_path, policy_path):
    source_paths = inventory_paths(read_json(inventory_path))
    toc = toc_routes(read_json(toc_path))
    if set(toc) != source_paths:
        raise ValueError("TOC and inventory SourcePath sets differ")
    policy = read_json(policy_path)
    defaults, overrides = parse_policy(policy, source_paths, set(toc.values()))
    output = []
    for path in sorted(source_paths):
        if path in overrides:
            output.append(overrides[path])
            continue
        reason = defaults[toc[path]]
        if reason is None:
            raise ValueError(f"requireExplicit branch {toc[path]} needs route override for {path}")
        output.append({"sourcePath": path, "outOfScopeReason": reason})
    return {
        "schemaVersion": 1,
        "previousRelease": policy["previousRelease"],
        "currentRelease": policy["currentRelease"],
        "inventoryDigest": policy["inventoryDigest"],
        "routes": output,
    }


def write_json_atomically(path, value):
    path = Path(path)
    with tempfile.NamedTemporaryFile("w", encoding="utf-8", dir=path.parent, prefix=f".{path.name}.", suffix=".tmp", delete=False) as temporary:
        json.dump(value, temporary, indent=2)
        temporary.write("\n")
        temporary_path = temporary.name
    try:
        os.replace(temporary_path, path)
    except BaseException:
        Path(temporary_path).unlink(missing_ok=True)
        raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inventory", type=Path, required=True)
    parser.add_argument("--toc", type=Path, required=True)
    parser.add_argument("--policy", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    try:
        result = generate(args.inventory, args.toc, args.policy)
    except ValueError as error:
        parser.error(str(error))
    write_json_atomically(args.output, result)


if __name__ == "__main__":
    main()
