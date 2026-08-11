#!/usr/bin/env python3
"""Filter current-gap fixtures by fresh Salesforce API-67 compilation first."""
from __future__ import annotations

import argparse
import concurrent.futures
import hashlib
import json
import os
import re
import subprocess
import time
from pathlib import Path


MAX_APEX_IDENTIFIER_LENGTH = 40
MAX_REMOTE_ORGS = 2


def load(path: Path):
    with path.open() as f:
        return json.load(f)


def file_sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def project_file_manifest(root: Path) -> list[dict]:
    """Return the exact regular-file tree executed by the Salesforce worker."""
    records = []
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            raise ValueError(f"project tree contains symlink: {path}")
        if not path.is_file():
            continue
        relative = path.relative_to(root).as_posix()
        if not relative or relative.startswith("../"):
            raise ValueError(f"unsafe project path: {path}")
        records.append({"path": relative, "sha256": file_sha256(path)})
    if not records:
        raise ValueError("project tree has no regular files")
    return records


def validate_local_summary_binding(manifest_path: Path, summary_path: Path) -> None:
    summary = load(summary_path)
    if summary.get("manifestSha256") != file_sha256(manifest_path):
        raise ValueError("local summary manifest binding does not match fixture manifest")


def validate_worker_execution(sf_bin: str, orgs: list[str]) -> None:
    if sf_bin != "/usr/local/bin/sf":
        raise ValueError("Salesforce CLI path must be /usr/local/bin/sf")
    if not orgs or len(orgs) != len(set(orgs)):
        raise ValueError("org aliases must be non-empty and unique")
    if len(orgs) > MAX_REMOTE_ORGS:
        raise ValueError(f"org alias maximum is {MAX_REMOTE_ORGS}")

def sf_environment() -> dict[str, str]:
    environment = os.environ.copy()
    environment["SF_USE_GENERIC_UNIX_KEYCHAIN"] = "true"
    return environment


class BoundSalesforceProcess:
    def __init__(self, completed: subprocess.CompletedProcess, executable_sha256: str, executable_after_sha256: str):
        self.completed = completed
        self.executable_sha256 = executable_sha256
        self.executable_after_sha256 = executable_after_sha256

    def __getattr__(self, name):
        return getattr(self.completed, name)


def run_sf(sf_bin: str, args: list[str], cwd: Path, timeout: int, text: bool = True) -> BoundSalesforceProcess:
    executable_sha256 = file_sha256(Path(sf_bin))
    completed = subprocess.run([sf_bin, *args], cwd=cwd, env=sf_environment(), capture_output=True, text=text, timeout=timeout, check=False)
    executable_after_sha256 = file_sha256(Path(sf_bin))
    if executable_sha256 != executable_after_sha256:
        raise RuntimeError("Salesforce CLI changed during execution")
    return BoundSalesforceProcess(completed, executable_sha256, executable_after_sha256)


def run_sf_stream(sf_bin: str, args: list[str], cwd: Path, timeout: int, stdout, stderr) -> BoundSalesforceProcess:
    executable_sha256 = file_sha256(Path(sf_bin))
    completed = subprocess.run([sf_bin, *args], cwd=cwd, env=sf_environment(), stdout=stdout, stderr=stderr, timeout=timeout, check=False)
    executable_after_sha256 = file_sha256(Path(sf_bin))
    if executable_sha256 != executable_after_sha256:
        raise RuntimeError("Salesforce CLI changed during execution")
    return BoundSalesforceProcess(completed, executable_sha256, executable_after_sha256)


def remap(path: str) -> str:
    path = path.replace("\\", "/")
    path = path.replace("force-app/main/classes/", "force-app/main/default/classes/")
    path = path.replace("force-app/main/triggers/", "force-app/main/default/triggers/")
    return path


def remap_class_path(path: str, mappings: dict[str, str]) -> str:
    """Keep Salesforce class filenames aligned with their declarations."""
    suffix = ".cls-meta.xml" if path.endswith(".cls-meta.xml") else ".cls"
    if not path.endswith(suffix):
        return path
    stem = path[: -len(suffix)]
    directory, _, filename = stem.rpartition("/")
    filename = mappings.get(filename, filename)
    return f"{directory}/{filename}{suffix}" if directory else f"{filename}{suffix}"


def is_standard_object_metadata(path: str) -> bool:
    """Exclude fixture declarations for Salesforce-owned objects and fields."""
    parts = Path(path).as_posix().split("/")
    try:
        objects = parts.index("objects")
    except ValueError:
        return False
    if len(parts) <= objects + 2:
        return False
    object_name = parts[objects + 1]
    if object_name.endswith(("__c", "__mdt", "__x")):
        return False
    filename = parts[-1]
    if filename == f"{object_name}.object-meta.xml":
        return True
    return parts[objects + 2] == "fields" and not filename.removesuffix(".field-meta.xml").endswith("__c")


def probe_class_name(original: str, fixture_name: str) -> str:
    """Return a stable Salesforce-valid name for a fixture declaration."""
    if len(original) <= MAX_APEX_IDENTIFIER_LENGTH:
        return original
    digest = hashlib.sha256(f"{fixture_name}:{original}".encode()).hexdigest()[:24]
    return f"SfProbe{digest}"


def declared_names(content: str) -> list[str]:
    return sorted(set(re.findall(r"\b(?:class|interface|enum)\s+([A-Za-z_][A-Za-z0-9_]*)", content)))


def manifest_fixture_path(raw_path: str, root: Path) -> Path:
    path = Path(raw_path)
    if path.is_absolute() or ".." in path.parts:
        raise ValueError(f"manifest fixture path must stay below root: {raw_path}")
    resolved_root = root.resolve()
    resolved = (resolved_root / path).resolve()
    try:
        resolved.relative_to(resolved_root)
    except ValueError as exc:
        raise ValueError(f"manifest fixture path escapes root: {raw_path}") from exc
    return resolved


def manifest_fixture_key(entry: dict) -> str:
    return Path(str(entry.get("fixture") or entry.get("path", ""))).stem


def local_pass_fixture_keys(summary_path: Path) -> set[str]:
    summary = load(summary_path)
    if summary.get("sealed") is not True:
        raise ValueError("local replay summary is not sealed")
    passed = set()
    for result in summary.get("results", []):
        if result.get("status") != "exit-0":
            continue
        if result.get("kind") == "test":
            observed = result.get("result") or {}
            test_summary = observed.get("summary", {}) if isinstance(observed, dict) else {}
            if test_summary.get("total", 0) <= 0:
                continue
        passed.update({
            Path(str(result.get("fixture", ""))).stem,
            Path(str(result.get("path", ""))).stem,
        } - {""})
    return passed


def adapt_apex_source(content: str, fixture_name: str) -> tuple[str, dict[str, str]]:
    """Make fixture Apex deployable without changing its exercised behavior."""
    mapping = {
        name: probe_class_name(name, fixture_name)
        for name in declared_names(content)
        if len(name) > MAX_APEX_IDENTIFIER_LENGTH
    }
    adapted = content
    for original, replacement in sorted(mapping.items(), key=lambda item: (-len(item[0]), item[0])):
        adapted = re.sub(rf"\b{re.escape(original)}\b", replacement, adapted)
    return adapted, mapping


def class_names(fixture: dict, fixture_name: str | None = None):
    fixture_name = fixture_name or fixture.get("name", "salesforce-first-filter")
    names = []
    for source in [*fixture.get("source", []), *fixture.get("schema", [])]:
        if not source.get("path", "").endswith(".cls"):
            continue
        content = source.get("content")
        if not isinstance(content, str):
            continue
        adapted, _ = adapt_apex_source(content, fixture_name)
        names.extend(declared_names(adapted))
    return sorted(set(names))


def test_class_names(fixture: dict, fixture_name: str | None = None):
    fixture_name = fixture_name or fixture.get("name", "salesforce-first-filter")
    names = []
    pattern = re.compile(
        r"@isTest(?:\s*\([^)]*\))?\s+"
        r"(?:(?:private|public|global)\s+)?"
        r"(?:(?:with|without|inherited)\s+sharing\s+)?"
        r"class\s+([A-Za-z_][A-Za-z0-9_]*)",
        flags=re.IGNORECASE,
    )
    for source in [*fixture.get("source", []), *fixture.get("schema", [])]:
        if not source.get("path", "").endswith(".cls"):
            continue
        content = source.get("content")
        if not isinstance(content, str):
            continue
        adapted, _ = adapt_apex_source(content, fixture_name)
        names.extend(pattern.findall(adapted))
    return sorted(set(names))


def resolve_source_content(root: Path, fixture_path: Path, source: dict) -> str:
    content = source.get("content")
    if isinstance(content, str):
        return content
    relative = Path(source.get("path", ""))
    for candidate in (root / relative, fixture_path.parent / relative):
        if candidate.is_file():
            return candidate.read_text()
    raise FileNotFoundError(f"fixture source is unavailable: {source.get('path')}")


def external_source_records(root: Path, fixture_path: Path, fixture: dict) -> list[dict]:
    records = []
    for source in list(fixture.get("source", [])) + list(fixture.get("schema", [])):
        if "content" in source:
            continue
        relative = Path(source.get("path", ""))
        source_path = next(
            (candidate for candidate in ((root / relative).resolve(), (fixture_path.parent / relative).resolve()) if candidate.is_file()),
            None,
        )
        if source_path is None:
            raise FileNotFoundError(f"fixture source is unavailable: {source.get('path')}")
        records.append({"path": str(relative), "sha256": file_sha256(source_path)})
    return sorted(records, key=lambda record: record["path"])


def exec_source(fixture: dict, root: Path | None = None, fixture_path: Path | None = None) -> str:
    args = fixture.get("command", {}).get("args") or []
    source = args[0] if args else ""
    for entry in fixture.get("source", []):
        if source == Path(entry.get("path", "")).name:
            if isinstance(entry.get("content"), str):
                return entry["content"]
            if root is not None and fixture_path is not None:
                return resolve_source_content(root, fixture_path, entry)
    return source


def fixture_ids(fixture: dict, gaps: set[str]) -> set[str]:
    out = set()
    for evidence in fixture.get("evidence", []) or []:
        sid = evidence.get("surfaceId") or evidence.get("usageKey") or evidence.get("id")
        if sid in gaps:
            out.add(sid)
    return out


def surface_namespace(surface_id: str) -> str:
    body = surface_id.removeprefix("apex:")
    return body.split(".", 1)[0].split("(", 1)[0]


def deferred_namespaces(policy: dict) -> set[str]:
    return {
        rule["namespace"]
        for rule in policy.get("rules", [])
        if rule.get("namespace") and rule.get("disposition") == "hosted-deferred"
    }


def manifest_candidates(manifest_path: Path, root: Path, gaps: set[str]):
    manifest = load(manifest_path)
    candidates = []
    for manifest_index, entry in enumerate(manifest.get("fixtures", [])):
        fixture_path = manifest_fixture_path(entry["path"], root)
        expected_sha = entry.get("sha256")
        if expected_sha and file_sha256(fixture_path) != expected_sha:
            raise ValueError(f"fixture hash mismatch for {fixture_path}")
        fixture = load(fixture_path)
        expected_sources = entry.get("sourceFiles")
        if expected_sources is None or external_source_records(root, fixture_path, fixture) != sorted(expected_sources, key=lambda record: record["path"]):
            raise ValueError(f"fixture source SHA-256 mismatch for {fixture_path}")
        kind = fixture.get("command", {}).get("kind")
        if kind not in {"test", "check", "exec"}:
            continue
        # An explicit packet manifest is already bounded to the promoted
        # non-deferred queue. Re-intersecting it with an older profile snapshot
        # can silently drop rows when the manifest and profile were materialized
        # in different steps.
        ids = set(entry.get("surfaceIds", []))
        if not ids:
            ids = fixture_ids(fixture, gaps)
        if entry.get("salesforceEligible") is False:
            continue
        names = class_names(fixture, fixture.get("name", fixture_path.stem))
        if kind == "exec":
            try:
                exec_source(fixture, root, fixture_path)
            except FileNotFoundError:
                continue
        elif not any(remap(source.get("path", "")).startswith("force-app/") for source in fixture.get("source", [])):
            continue
        if ids and (names or kind in {"check", "exec"}):
            fixture["_fixturePath"] = str(fixture_path)
            fixture["_manifestIndex"] = manifest_index
            candidates.append((len(ids), fixture_path.name, fixture, ids))
    return candidates


def make_project(
    root: Path,
    fixture: dict,
    fixture_name: str | None = None,
    source_root: Path | None = None,
    fixture_path: Path | None = None,
):
    fixture_name = fixture_name or fixture.get("name", root.name)
    root.mkdir(parents=True, exist_ok=True)
    mappings: dict[str, str] = {}
    sources = []
    for source in [*fixture.get("source", []), *fixture.get("schema", [])]:
        rel = remap(source.get("path", ""))
        # Keep deployable force-app sources. Older fixtures may include prose or
        # runner-only files outside force-app; ignore those without guessing.
        if not rel.startswith("force-app/"):
            continue
        if rel.endswith("-meta.xml") and is_standard_object_metadata(rel):
            continue
        try:
            content = resolve_source_content(source_root, fixture_path, source) if source_root and fixture_path else source.get("content")
        except FileNotFoundError:
            content = None
        if not isinstance(content, str):
            # External-source fixtures are classified as setup/incomplete by the
            # caller. Writing an empty Apex file would create a false compiler
            # observation.
            continue
        if rel.endswith(".cls"):
            for name in declared_names(content):
                if len(name) > MAX_APEX_IDENTIFIER_LENGTH:
                    mappings[name] = probe_class_name(name, fixture_name)
        sources.append((rel, content))

    for rel, content in sources:
        if rel.endswith(".cls"):
            for original, replacement in sorted(mappings.items(), key=lambda item: (-len(item[0]), item[0])):
                content = re.sub(rf"\b{re.escape(original)}\b", replacement, content)
        rel = remap_class_path(rel, mappings)
        target = root / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content)
        if rel.endswith(".cls"):
            target.with_suffix(".cls-meta.xml").write_text(
                '<?xml version="1.0" encoding="UTF-8"?>\n'
                '<ApexClass xmlns="http://soap.sforce.com/2006/04/metadata">\n'
                '  <apiVersion>67.0</apiVersion>\n'
                '  <status>Active</status>\n'
                '</ApexClass>\n'
            )
    if fixture.get("command", {}).get("kind") == "exec":
        if not source_root or not fixture_path:
            content = exec_source(fixture)
        else:
            content = exec_source(fixture, source_root, fixture_path)
        (root / "anonymous.apex").write_text(content)
    (root / "sfdx-project.json").write_text(json.dumps({
        "packageDirectories": [{"path": "force-app", "default": True}],
        "name": fixture.get("name", "salesforce-first-filter"),
        "namespace": "",
        "sfdcLoginUrl": "https://login.salesforce.com",
        "sourceApiVersion": "67.0",
    }, separators=(",", ":")))
    return mappings


def safe_fixture_stem(fixture_name: str) -> str:
    path = Path(fixture_name)
    if (
        fixture_name != path.name
        or any(part in {".", ".."} for part in path.parts)
        or path.stem in {"", ".", ".."}
    ):
        raise ValueError(f"unsafe fixture name: {fixture_name}")
    return path.stem


def runtime_requested_for(kind: str, runtime: bool) -> bool:
    return kind == "exec" or (runtime and kind == "test")


def deploy_command(project: Path, org: str, runtime: bool = False) -> list[str]:
    command = ["project", "deploy", "start", "--source-dir", str(project / "force-app"), "--target-org", org]
    command.extend(["--ignore-conflicts"] if runtime else ["--dry-run"])
    return [*command, "--wait", "30", "--json"]


def test_command(org: str, test_classes: list[str]) -> list[str]:
    if not test_classes:
        raise ValueError("runtime Salesforce test requires at least one test class")
    command = ["apex", "run", "test", "--tests", ",".join(test_classes), "--target-org", org]
    if len(test_classes) == 1:
        command.append("--synchronous")
    return [*command, "--wait", "10", "--result-format", "json", "--json"]


def metadata_names_from_project(project: Path) -> list[str]:
    """Return only metadata actually materialized by this fixture project."""
    root = project / "force-app"
    names: set[str] = set()
    for path in root.rglob("*"):
        if not path.is_file():
            continue
        relative = path.relative_to(root).as_posix()
        parts = Path(relative).parts
        if relative.endswith(".cls"):
            names.update(f"ApexClass:{name}" for name in declared_names(path.read_text()))
        elif relative.endswith(".page"):
            names.add(f"ApexPage:{path.stem}")
        elif relative.endswith(".object-meta.xml") and "objects" in parts:
            index = parts.index("objects")
            if len(parts) > index + 2 and parts[-1] == f"{parts[index + 1]}.object-meta.xml":
                names.add(f"CustomObject:{parts[index + 1]}")
        elif relative.endswith(".field-meta.xml") and "objects" in parts and "fields" in parts:
            index = parts.index("objects")
            if len(parts) > index + 3 and parts[index + 2] == "fields":
                names.add(f"CustomField:{parts[index + 1]}.{path.name.removesuffix('.field-meta.xml')}")
        elif relative.endswith(".fieldSet-meta.xml") and "objects" in parts and "fieldSets" in parts:
            index = parts.index("objects")
            if len(parts) > index + 3 and parts[index + 2] == "fieldSets":
                names.add(f"FieldSet:{parts[index + 1]}.{path.name.removesuffix('.fieldSet-meta.xml')}")
        elif relative.endswith(".trigger"):
            names.add(f"ApexTrigger:{path.stem}")
        elif relative.endswith(".resource-meta.xml") and "staticresources" in parts:
            names.add(f"StaticResource:{path.name.removesuffix('.resource-meta.xml')}")
        elif relative.endswith(".cachePartition-meta.xml") and "cachePartitions" in parts:
            names.add(f"PlatformCachePartition:{path.name.removesuffix('.cachePartition-meta.xml')}")
    return sorted(names)


def _base_object_name(name: str) -> str:
    for suffix in ("__c", "__mdt", "__x"):
        if name.endswith(suffix):
            return name[: -len(suffix)]
    return name


def _metadata_records_present(inventory: dict[str, dict], requested: list[str]) -> list[str]:
    """Intersect fixture metadata with the live Tooling inventory."""
    object_ids = {}
    for record in inventory.get("CustomObject", {}).get("records", []):
        developer_name = record.get("DeveloperName")
        if developer_name:
            object_ids[developer_name] = record.get("Id")
            object_ids[f"{developer_name}__c"] = record.get("Id")
            object_ids[f"{developer_name}__mdt"] = record.get("Id")
    present = []
    for metadata in requested:
        metadata_type, _, name = metadata.partition(":")
        records = inventory.get(metadata_type, {}).get("records", [])
        if metadata_type in {"ApexClass", "ApexPage", "ApexTrigger", "StaticResource"}:
            field = "Name"
            if any(record.get(field) == name for record in records):
                present.append(metadata)
        elif metadata_type == "PlatformCachePartition":
            if any(record.get("DeveloperName") == name for record in records):
                present.append(metadata)
        elif metadata_type == "CustomObject":
            if any(record.get("DeveloperName") == _base_object_name(name) for record in records):
                present.append(metadata)
        elif metadata_type in {"CustomField", "FieldSet"}:
            object_name, separator, member_name = name.partition(".")
            if not separator:
                continue
            member_field = "DeveloperName"
            object_field = "TableEnumOrId" if metadata_type == "CustomField" else "EntityDefinitionId"
            object_id = object_ids.get(object_name)
            member_candidates = {member_name, _base_object_name(member_name)}
            object_candidates = {object_name, _base_object_name(object_name), object_id}
            if any(
                record.get(member_field) in member_candidates
                and record.get(object_field) in object_candidates
                for record in records
            ):
                present.append(metadata)
    return sorted(present)


def metadata_names_from_inventory(inventory: dict[str, dict]) -> set[str]:
    """Return metadata names that existed before this packet and must be preserved."""
    names = set()
    for metadata_type in ("ApexClass", "ApexPage", "ApexTrigger", "StaticResource"):
        field = "Name"
        names.update(
            f"{metadata_type}:{record[field]}"
            for record in inventory.get(metadata_type, {}).get("records", [])
            if record.get(field)
        )
    object_names = {}
    for record in inventory.get("CustomObject", {}).get("records", []):
        developer_name = record.get("DeveloperName")
        record_id = record.get("Id")
        if not developer_name:
            continue
        object_names[record_id] = f"{developer_name}__c"
        for suffix in ("__c", "__mdt", "__x"):
            names.add(f"CustomObject:{developer_name}{suffix}")
    for metadata_type, owner_field in (("CustomField", "TableEnumOrId"), ("FieldSet", "EntityDefinitionId")):
        for record in inventory.get(metadata_type, {}).get("records", []):
            owner = object_names.get(record.get(owner_field), record.get(owner_field))
            member = record.get("DeveloperName")
            if not owner or not member:
                continue
            names.add(f"{metadata_type}:{owner}.{member}")
            if metadata_type == "CustomField":
                names.add(f"{metadata_type}:{owner}.{member}__c")
    names.update(
        f"PlatformCachePartition:{record['DeveloperName']}"
        for record in inventory.get("PlatformCachePartition", {}).get("records", [])
        if record.get("DeveloperName")
    )
    return names


def org_cleanup(
    sf_bin: str,
    project: Path,
    target_org: str,
    metadata_names: list[str],
    protected_metadata_names: set[str] | None = None,
) -> dict:
    metadata_types = sorted({metadata.partition(":")[0] for metadata in metadata_names})
    before = metadata_inventory(sf_bin, project, target_org, metadata_types)
    protected = protected_metadata_names or set()
    requested = [
        metadata
        for metadata in _metadata_records_present(before, metadata_names)
        if metadata not in protected
    ]
    if not requested:
        executable_sha256 = file_sha256(Path(sf_bin))
        return {
            "requested": [],
            "cleanupExitCode": 0,
            "sfExecutableSha256": executable_sha256,
            "sfExecutableAfterSha256": executable_sha256,
            "verification": {"metadataTypes": metadata_types, "remaining": []},
            "residueAbsent": True,
        }
    deleted = run_sf(sf_bin, ["project", "delete", "source", *sum((["--metadata", metadata] for metadata in requested), []), "--target-org", target_org, "--no-prompt", "--wait", "30", "--json"], project, 120)
    if deleted.returncode != 0:
        return {
            "requested": requested,
            "cleanupExitCode": deleted.returncode,
            "sfExecutableSha256": deleted.executable_sha256,
            "sfExecutableAfterSha256": deleted.executable_after_sha256,
            "residueAbsent": False,
            "error": deleted.stderr[-2000:],
        }
    after = metadata_inventory(sf_bin, project, target_org, metadata_types)
    remaining = _metadata_records_present(after, requested)
    return {
        "requested": requested,
        "cleanupExitCode": deleted.returncode,
        "sfExecutableSha256": deleted.executable_sha256,
        "sfExecutableAfterSha256": deleted.executable_after_sha256,
        "verification": {"metadataTypes": metadata_types, "remaining": remaining},
        "residueAbsent": not remaining,
    }


def deployable_counts(results: list[dict]) -> tuple[int, int]:
    deployable = [result for result in results if result.get("deployable") is True]
    return len(deployable), sum(result.get("coverage", 0) for result in deployable)


def result_failed(result: dict) -> bool:
    if result.get("status") in {"skipped", "not-selected", "excluded"}:
        return False
    return result.get("exitCode") is None or result.get("exitCode") != 0


def expected_result_indexes(manifest_count: int, selected: list[tuple], partitioned: bool) -> set[int]:
    if partitioned:
        return {item[2]["_manifestIndex"] for item in selected}
    return set(range(manifest_count))


def exec_command(project: Path, org: str) -> list[str]:
    return ["apex", "run", "--file", str(project / "anonymous.apex"), "--target-org", org, "--api-version", "67.0", "--json"]


def org_identity(sf_bin: str, root: Path, org: str) -> dict:
    command = ["org", "display", "--target-org", org, "--json"]
    completed = None
    for attempt in range(3):
        completed = run_sf(sf_bin, command, root, 30)
        if completed.returncode == 0:
            break
        if attempt < 2:
            time.sleep(1)
    assert completed is not None
    if completed.returncode != 0:
        detail = (completed.stderr or completed.stdout or "").strip().replace("\n", " ")
        raise RuntimeError(
            f"org identity lookup failed for {org}: exit {completed.returncode}"
            + (f": {detail[-500:]}" if detail else "")
        )
    try:
        payload = json.loads(completed.stdout)
        result = payload["result"]
    except (json.JSONDecodeError, KeyError, TypeError) as exc:
        raise RuntimeError(f"org identity lookup returned invalid JSON for {org}") from exc
    org_id = result.get("id")
    api_version = result.get("apiVersion")
    if not org_id or not api_version:
        raise RuntimeError(f"org identity is incomplete for {org}")
    return {
        "alias": org,
        "orgId": org_id,
        "apiVersion": api_version,
        "username": result.get("username"),
        "instanceUrl": result.get("instanceUrl"),
        "connectedStatus": result.get("connectedStatus") or result.get("status"),
        "command": command,
    }


def tooling_query(sf_bin: str, root: Path, org: str, query: str) -> dict:
    completed = run_sf(sf_bin, ["data", "query", "--query", query, "--target-org", org, "--use-tooling-api", "--json"], root, 60)
    if completed.returncode != 0:
        detail = (completed.stderr or completed.stdout or "").strip().replace("\n", " ")
        raise RuntimeError(f"Tooling query failed for {org}: {detail[-500:]}")
    try:
        result = json.loads(completed.stdout)["result"]
    except (json.JSONDecodeError, KeyError, TypeError) as exc:
        raise RuntimeError(f"Tooling query returned invalid JSON for {org}") from exc
    return {
        "records": result.get("records", []),
        "totalSize": result.get("totalSize"),
    }


ORG_INVENTORY_QUERIES = {
    "ApexClass": "SELECT Id,Name,NamespacePrefix FROM ApexClass ORDER BY Name",
    "ApexPage": "SELECT Id,Name FROM ApexPage ORDER BY Name",
    "ApexTrigger": "SELECT Id,Name,TableEnumOrId FROM ApexTrigger ORDER BY Name",
    "CustomObject": "SELECT Id,DeveloperName,NamespacePrefix FROM CustomObject ORDER BY DeveloperName",
    "CustomField": "SELECT Id,DeveloperName,TableEnumOrId FROM CustomField ORDER BY DeveloperName",
    "FieldSet": "SELECT Id,DeveloperName,EntityDefinitionId FROM FieldSet ORDER BY DeveloperName",
    "StaticResource": "SELECT Id,Name,Body,BodyLength,ContentType FROM StaticResource ORDER BY Name",
    "PlatformCachePartition": "SELECT FIELDS(ALL) FROM PlatformCachePartition LIMIT 200",
}


def static_resource_body_sha256(sf_bin: str, root: Path, org: str, record_id: str) -> str:
    completed = run_sf(sf_bin, ["api", "request", "rest", f"/services/data/v67.0/tooling/sobjects/StaticResource/{record_id}/Body", "--target-org", org], root, 60, text=False)
    if completed.returncode != 0:
        detail = (completed.stderr or completed.stdout or b"").decode(errors="replace").strip().replace("\n", " ")
        raise RuntimeError(f"StaticResource body query failed for {org}: {detail[-500:]}")
    return hashlib.sha256(completed.stdout).hexdigest()


def sobject_exists(sf_bin: str, root: Path, org: str, sobject: str) -> bool:
    completed = run_sf(sf_bin, ["sobject", "describe", "--sobject", sobject, "--target-org", org, "--json"], root, 60)
    if completed.returncode != 0:
        return False
    try:
        payload = json.loads(completed.stdout)
    except json.JSONDecodeError:
        return False
    return payload.get("status") == 0


def live_field_keys(sf_bin: str, root: Path, org: str, records: list[dict]) -> set[tuple[str, str]]:
    """Return active FieldDefinition keys for deleted-field tombstone checks."""
    tombstone_owners = {
        (str(record.get("TableEnumOrId")), str(record.get("DeveloperName")))
        for record in records
        if re.search(r"_del\d*$", str(record.get("DeveloperName", "")))
    }
    if not tombstone_owners:
        return set()
    live = set()
    for owner in sorted({owner for owner, _ in tombstone_owners}):
        query = (
            "SELECT DeveloperName,EntityDefinitionId,EntityDefinition.QualifiedApiName "
            "FROM FieldDefinition "
            f"WHERE EntityDefinition.QualifiedApiName = '{owner}'"
        )
        for record in tooling_query(sf_bin, root, org, query).get("records", []):
            entity = record.get("EntityDefinition") or {}
            qualified_name = entity.get("QualifiedApiName") or owner
            live.add((str(qualified_name), str(record.get("DeveloperName"))))
    return live


def metadata_inventory(sf_bin: str, root: Path, org: str, metadata_types) -> dict:
    inventory = {
        name: tooling_query(sf_bin, root, org, query)
        for name, query in ORG_INVENTORY_QUERIES.items()
        if name in metadata_types
    }
    if "CustomField" in inventory:
        records = inventory["CustomField"].get("records", [])
        live_keys = live_field_keys(sf_bin, root, org, records)
        inventory["CustomField"]["records"] = [
            record
            for record in records
            if not re.search(r"_del\d*$", str(record.get("DeveloperName", "")))
            or (
                str(record.get("TableEnumOrId")),
                str(record.get("DeveloperName")),
            ) in live_keys
        ]
        inventory["CustomField"]["totalSize"] = len(inventory["CustomField"]["records"])
    if "CustomObject" not in inventory:
        return inventory
    all_custom_object_ids = {
        record.get("Id")
        for record in inventory["CustomObject"].get("records", [])
        if record.get("Id")
    }
    active_custom_objects = []
    for record in inventory["CustomObject"].get("records", []):
        developer_name = record.get("DeveloperName")
        if developer_name and any(
            sobject_exists(sf_bin, root, org, f"{developer_name}{suffix}")
            for suffix in ("__c", "__mdt", "__x")
        ):
            active_custom_objects.append(record)
    inventory["CustomObject"]["records"] = active_custom_objects
    inventory["CustomObject"]["totalSize"] = len(active_custom_objects)
    active_custom_object_ids = {
        record.get("Id")
        for record in active_custom_objects
        if record.get("Id")
    }
    for metadata_type, owner_field in (("CustomField", "TableEnumOrId"), ("FieldSet", "EntityDefinitionId")):
        if metadata_type not in inventory:
            continue
        inventory[metadata_type]["records"] = [
            record
            for record in inventory[metadata_type].get("records", [])
            if record.get(owner_field) not in all_custom_object_ids
            or record.get(owner_field) in active_custom_object_ids
        ]
        inventory[metadata_type]["totalSize"] = len(inventory[metadata_type]["records"])
    return inventory


def org_inventory(sf_bin: str, root: Path, org: str) -> dict:
    inventory = metadata_inventory(sf_bin, root, org, ORG_INVENTORY_QUERIES)
    for record in inventory["StaticResource"].get("records", []):
        record_id = record.get("Id")
        if record_id and record.get("Body"):
            record["BodySha256"] = static_resource_body_sha256(sf_bin, root, org, record_id)
    return inventory


def canonical_inventory(inventory: dict) -> dict:
    audit_fields = {
        "attributes",
        "CreatedDate",
        "CreatedById",
        "LastModifiedDate",
        "LastModifiedById",
        "SystemModstamp",
    }
    return {
        name: {
            "totalSize": value.get("totalSize"),
            "records": sorted(
                [
                    {key: field for key, field in record.items() if key not in audit_fields}
                    for record in value.get("records", [])
                ],
                key=lambda record: (str(record.get("Id", "")), str(record.get("Name", record.get("DeveloperName", "")))),
            ),
        }
        for name, value in inventory.items()
    }


def acquire_org_postflight(sf_bin: str, root: Path, preflight: dict, orgs: list[str]) -> dict:
    baseline = {row.get("alias"): row for row in preflight.get("orgs", [])}
    rows = []
    for org in orgs:
        current = org_inventory(sf_bin, root, org)
        expected = (baseline.get(org) or {}).get("inventory", {})
        matches = canonical_inventory(current) == canonical_inventory(expected)
        rows.append({
            "alias": org,
            "orgId": (baseline.get(org) or {}).get("orgId"),
            "inventory": current,
            "matchesPreflight": matches,
        })
    return {
        "schemaVersion": 1,
        "status": "verified" if all(row["matchesPreflight"] for row in rows) else "mismatch",
        "preflightId": preflight.get("preflightId"),
        "orgs": rows,
        "matchesPreflight": all(row["matchesPreflight"] for row in rows),
    }


def acquire_org_preflight(sf_bin: str, root: Path, orgs: list[str], identities: dict[str, dict]) -> dict:
    queries = dict(ORG_INVENTORY_QUERIES)
    rows = []
    for org in orgs:
        inventory = org_inventory(sf_bin, root, org)
        identity = identities[org]
        if any(inventory[name].get("totalSize") != 0 for name in ("ApexClass", "ApexPage")):
            raise RuntimeError(f"org preflight is not clean for {org}")
        rows.append({
            "alias": org,
            "orgId": identity.get("orgId"),
            "apiVersion": identity.get("apiVersion"),
            "inventory": inventory,
        })
    return {
        "schemaVersion": 3,
        "source": "acquired-by-salesforce-first-filter",
		"preflightId": f"preflight-{hashlib.sha256(json.dumps(identities, sort_keys=True).encode()).hexdigest()[:12]}",
        "acquiredAtUtc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "queries": queries,
        "orgs": rows,
    }


def validate_org_preflight(path: Path, identities: dict[str, dict], orgs: list[str]) -> str:
    payload = load(path)
    rows = {row.get("alias"): row for row in payload.get("orgs", [])}
    for org in orgs:
        row = rows.get(org)
        identity = identities.get(org)
        if not row or not identity or row.get("orgId") != identity.get("orgId") or row.get("apiVersion") != identity.get("apiVersion"):
            raise ValueError(f"org preflight identity mismatch for {org}")
        inventory = row.get("inventory") or {}
        if any(
            not isinstance(inventory.get(name), dict)
            or inventory[name].get("totalSize") != 0
            for name in ("ApexClass", "ApexPage")
        ):
            raise ValueError(f"org preflight is not clean for {org}")
    return file_sha256(path)

def run_one(
    item,
    out: Path,
    org: str,
    sf_bin: str,
    source_root: Path | None = None,
    provenance: dict | None = None,
    runtime: bool = False,
    org_identity: dict | None = None,
    org_baseline_inventory: dict | None = None,
):
    count, name, fixture, ids = item
    stem = safe_fixture_stem(name)
    project = out / "projects" / stem
    fixture_path = Path(fixture.get("_fixturePath")) if fixture.get("_fixturePath") else (source_root / name if source_root else None)
    class_name_map = make_project(project, fixture, name, source_root, fixture_path)
    project_manifest = project_file_manifest(project)
    protected_metadata_names = metadata_names_from_inventory(org_baseline_inventory or {})
    kind = fixture.get("command", {}).get("kind", "unknown")
    target_org = (org_identity or {}).get("username") or org
    test_classes = test_class_names(fixture, name)
    deploy = project / f"salesforce-{org}.json"
    stderr = project / f"salesforce-{org}.stderr"
    setup = project / f"salesforce-{org}.setup"
    runtime_output = project / f"salesforce-{org}-tests.json"
    runtime_stderr = project / f"salesforce-{org}-tests.stderr"
    cleanup_receipt = None
    runtime_proc = None
    runtime_payload = {}
    invocation = {
        "sfBinary": sf_bin,
        "environment": {"SF_USE_GENERIC_UNIX_KEYCHAIN": "true"},
        "targetOrg": target_org,
        "commands": [],
    }
    try:
        setup.write_text("direct Salesforce worker execution\n")
        command = exec_command(project, target_org) if kind == "exec" else deploy_command(project, target_org, runtime=runtime)
        proc = run_sf_stream(sf_bin, command, project, 180, deploy.open("w"), stderr.open("w"))
        invocation["commands"].append({"purpose": "deploy-or-exec", "args": [sf_bin, *command], "executableSha256": proc.executable_sha256, "executableAfterSha256": proc.executable_after_sha256})
        payload = load(deploy) if deploy.exists() and deploy.stat().st_size else {}
        if runtime and kind == "test" and proc.returncode == 0 and test_classes:
            runtime_command = test_command(target_org, test_classes)
            runtime_proc = run_sf_stream(sf_bin, runtime_command, project, 180, runtime_output.open("w"), runtime_stderr.open("w"))
            invocation["commands"].append({"purpose": "runtime-test", "args": [sf_bin, *runtime_command], "executableSha256": runtime_proc.executable_sha256, "executableAfterSha256": runtime_proc.executable_after_sha256})
            runtime_payload = load(runtime_output) if runtime_output.exists() and runtime_output.stat().st_size else {}
    except subprocess.TimeoutExpired:
        proc = None
        payload = {"status": "timeout"}
    finally:
        metadata_names = metadata_names_from_project(project)
        try:
            cleanup_project = out / "cleanup" / stem
            cleanup_project.mkdir(parents=True, exist_ok=True)
            (cleanup_project / "sfdx-project.json").write_bytes((project / "sfdx-project.json").read_bytes())
            (cleanup_project / "force-app").mkdir()
            cleanup_receipt = org_cleanup(sf_bin, cleanup_project, target_org, metadata_names, protected_metadata_names)
        except (OSError, RuntimeError, subprocess.TimeoutExpired) as exc:
            cleanup_receipt = {"requested": metadata_names, "cleanupExitCode": 1, "residueAbsent": False, "error": f"{type(exc).__name__}: {exc}"}
    result = payload.get("result", payload)
    details = result.get("details", {}) if isinstance(result, dict) else {}
    failures = details.get("componentFailures", []) if isinstance(details, dict) else []
    successes = details.get("componentSuccesses", []) if isinstance(details, dict) else []
    if kind == "exec":
        if proc is not None and proc.returncode == 0:
            successes = [{"fileName": "anonymous.apex"}]
        else:
            problem = result.get("compileProblem") or result.get("message") or result.get("exceptionMessage") if isinstance(result, dict) else "anonymous Apex failed"
            failures = [{"fileName": "anonymous.apex", "problem": problem or "anonymous Apex failed", "problemType": "ApexRun"}]
    result_status = result.get("status", payload.get("status", "error")) if isinstance(result, dict) else "error"
    if kind == "exec":
        result_status = "Succeeded" if successes else "Failed"
    runtime_result = runtime_payload.get("result", runtime_payload) if isinstance(runtime_payload, dict) else {}
    runtime_summary = runtime_result.get("summary", {}) if isinstance(runtime_result, dict) else {}
    if kind == "exec":
        runtime_payload = payload
    runtime_requested = runtime_requested_for(kind, runtime)
    if runtime and kind == "test" and not test_classes:
        runtime_payload = {"error": "runtime Salesforce test requires at least one @isTest class"}
    runtime_passed = (
        True
        if kind == "exec" and proc is not None and proc.returncode == 0 and bool(successes)
        else bool(
            runtime and kind == "test"
            and runtime_proc is not None
            and runtime_proc.returncode == 0
            and runtime_summary.get("outcome") == "Passed"
            and runtime_summary.get("failing", 0) == 0
            and runtime_summary.get("testsRan", 0) > 0
        )
        if runtime_requested
        else None
    )
    return {
        "fixture": name,
        "manifestIndex": fixture.get("_manifestIndex"),
        "coverage": count,
        "surfaceIds": sorted(ids),
        "kind": kind,
        "classNameMap": class_name_map,
        "testClasses": test_classes,
        "org": org,
        "orgIdentity": org_identity,
        "project": str(project),
        "execution": "salesforce-worker",
        "projectManifest": project_manifest,
        "invocation": invocation,
        "orgCleanup": cleanup_receipt,
        "exitCode": None if proc is None else proc.returncode,
        "status": result_status,
        "deployable": bool(successes) and not failures,
        "componentFailures": failures,
        "componentSuccesses": successes,
        "runtimeRequested": runtime_requested,
        "runtimeStatus": "Passed" if runtime_passed is True else "Failed" if runtime_requested else "not-run",
        "runtimePassed": runtime_passed,
        "runtimeExitCode": None if runtime_proc is None else runtime_proc.returncode,
        "runtimeResult": runtime_payload,
        "candidateCommit": (provenance or {}).get("candidateCommit"),
        "candidateSha256": (provenance or {}).get("candidateSha256"),
        "toolsCommit": (provenance or {}).get("toolsCommit"),
        "workflowScriptSha256": (provenance or {}).get("workflowScriptSha256"),
    }


def select_candidates(candidates, limit: int):
    if limit < 0:
        raise ValueError("--limit must be non-negative")
    return candidates[:limit]


def filter_manifest_index(candidates, modulus: int | None, remainder: int = 0):
    if modulus is None:
        if remainder != 0:
            raise ValueError("--manifest-index-remainder requires --manifest-index-modulus")
        return candidates
    if modulus <= 0:
        raise ValueError("--manifest-index-modulus must be positive")
    if remainder < 0 or remainder >= modulus:
        raise ValueError("--manifest-index-remainder must be between 0 and modulus - 1")
    filtered = []
    for item in candidates:
        manifest_index = item[2].get("_manifestIndex")
        if not isinstance(manifest_index, int):
            raise ValueError("manifest-index partitioning requires an explicit fixture manifest")
        if manifest_index % modulus == remainder:
            filtered.append(item)
    return filtered


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--profile", type=Path, required=True)
    ap.add_argument("--fixtures", type=Path, required=True)
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--orgs", required=True, help="comma-separated scratch-org aliases")
    ap.add_argument("--policy", type=Path, help="support policy; defaults to apex-local-support-policy.json beside fixtures")
    ap.add_argument("--limit", type=int, default=16)
    ap.add_argument("--manifest-index-modulus", type=int, help="select one disjoint manifest-index partition")
    ap.add_argument("--manifest-index-remainder", type=int, default=0)
    ap.add_argument("--manifest", type=Path, help="explicit packet-bound fixture manifest; disables fixture discovery")
    ap.add_argument("--root", type=Path, default=Path.cwd(), help="root used to resolve relative manifest fixture paths")
    ap.add_argument("--sf-bin", required=True)
    ap.add_argument("--candidate-commit")
    ap.add_argument("--candidate-sha256")
    ap.add_argument("--tools-commit")
    ap.add_argument("--tools-amd64-sha256")
    ap.add_argument("--queue-sha256")
    ap.add_argument("--selector-sha256")
    ap.add_argument("--selector-receipt-sha256")
    ap.add_argument("--runtime", action="store_true", help="deploy fixture classes and run their Salesforce tests after deployment")
    ap.add_argument("--local-summary", type=Path, help="only execute fixtures that passed the sealed local replay")
    ap.add_argument("--org-preflight", type=Path, help="runtime mode acquires a fresh receipt")
    args = ap.parse_args()
    if args.out.exists() and any(args.out.iterdir()):
        ap.error(f"Salesforce output must be a new empty directory: {args.out}")
    gaps = {row["surfaceId"] for row in load(args.profile)["nonDeferredGaps"]}
    policy_path = args.policy or (args.fixtures / "apex-local-support-policy.json")
    policy = load(policy_path) if policy_path.exists() else {}
    deferred = deferred_namespaces(policy)
    candidates = []
    manifest_entries = load(args.manifest).get("fixtures", []) if args.manifest else []
    if args.manifest and args.local_summary:
        validate_local_summary_binding(args.manifest, args.local_summary)
    allowed_local = local_pass_fixture_keys(args.local_summary) if args.local_summary else None
    skipped_deferred = []
    if args.manifest:
        candidates = manifest_candidates(args.manifest, args.root.resolve(), gaps)
        if allowed_local is not None:
            candidates = [
                item for item in candidates
                if Path(item[1]).stem in allowed_local or Path(item[2].get("name", item[1])).stem in allowed_local
            ]
    else:
        for path in sorted(args.fixtures.glob("*.json")):
            try:
                fixture = load(path)
            except Exception:
                continue
            if fixture.get("command", {}).get("kind") != "test":
                continue
            ids = fixture_ids(fixture, gaps)
            deferred_ids = sorted(sid for sid in ids if surface_namespace(sid) in deferred)
            if deferred_ids:
                skipped_deferred.append({"fixture": path.name, "surfaceIds": deferred_ids})
                ids -= set(deferred_ids)
            names = class_names(fixture, fixture.get("name", path.stem))
            if not ids or not names:
                continue
            candidates.append((len(ids), path.name, fixture, ids))
    candidates.sort(key=lambda item: (-item[0], item[1]))
    try:
        partitioned = filter_manifest_index(
            candidates, args.manifest_index_modulus, args.manifest_index_remainder
        )
        selected = select_candidates(partitioned, args.limit)
    except ValueError as exc:
        ap.error(str(exc))
    args.out.mkdir(parents=True, exist_ok=True)
    selection = [
        {
            "fixture": n,
            "coverage": c,
            "kind": f.get("command", {}).get("kind", "unknown"),
            "surfaceIds": sorted(ids),
            "testClasses": class_names(f, n),
            "sourceClassNames": sorted({
                name
                for source in f.get("source", [])
                for name in declared_names(source.get("content", ""))
                if isinstance(source.get("content"), str)
            }),
        }
        for c, n, f, ids in selected
    ]
    (args.out / "selection.json").write_text(json.dumps(selection, indent=2) + "\n")
    selection_sha = file_sha256(args.out / "selection.json")
    orgs = [x.strip() for x in args.orgs.split(",") if x.strip()]
    try:
        validate_worker_execution(args.sf_bin, orgs)
    except ValueError as exc:
        ap.error(str(exc))
    if args.limit < 0:
        ap.error("--limit must be non-negative")
    results = []
    acquired_preflight = None
    provenance = {
        "candidateCommit": args.candidate_commit,
        "candidateSha256": args.candidate_sha256,
        "toolsCommit": args.tools_commit,
        "toolsAmd64Sha256": args.tools_amd64_sha256,
        "workflowScriptSha256": file_sha256(Path(__file__).resolve()),
    }
    org_identities = {}
    org_preflight_sha = None
    for org in orgs:
        org_identities[org] = org_identity(args.sf_bin, args.root.resolve(), org)
    if args.runtime:
        acquired_preflight = acquire_org_preflight(args.sf_bin, args.root.resolve(), orgs, org_identities)
        preflight_path = args.out / "org-preflight.json"
        preflight_path.write_text(json.dumps(acquired_preflight, indent=2, sort_keys=True) + "\n")
        org_preflight_sha = file_sha256(preflight_path)

    org_baseline_inventories = {
        row.get("alias"): row.get("inventory", {})
        for row in (acquired_preflight or {}).get("orgs", [])
    }

    def run_org_queue(org: str, items: list[tuple]) -> list[dict]:
        return [
            run_one(
                item,
                args.out,
                org,
                args.sf_bin,
                args.root.resolve(),
                provenance,
                args.runtime,
                org_identities.get(org),
                org_baseline_inventories.get(org),
            )
            for item in items
        ]

    assigned = {org: selected[index::len(orgs)] for index, org in enumerate(orgs)}
    org_postflight = None
    try:
        with concurrent.futures.ThreadPoolExecutor(max_workers=len(orgs)) as pool:
            futures = [pool.submit(run_org_queue, org, assigned[org]) for org in orgs]
            for future in futures:
                results.extend(future.result())
    finally:
        if args.runtime and acquired_preflight is not None:
            try:
                org_postflight = acquire_org_postflight(args.sf_bin, args.root.resolve(), acquired_preflight, orgs)
            except (OSError, RuntimeError, subprocess.TimeoutExpired) as exc:
                org_postflight = {
                    "schemaVersion": 1,
                    "status": "error",
                    "matchesPreflight": False,
                    "error": f"{type(exc).__name__}: {exc}",
                }
    selected_indexes = {result.get("manifestIndex") for result in results}
    seen_indexes = set(selected_indexes)
    excluded_entries = [
        (manifest_index, entry)
        for manifest_index, entry in enumerate(manifest_entries)
        if entry.get("salesforceEligible") is False
        and (
            args.manifest_index_modulus is None
            or manifest_index % args.manifest_index_modulus == args.manifest_index_remainder
        )
    ]
    for manifest_index, entry in excluded_entries:
        if manifest_index in seen_indexes:
            continue
        results.append({
            "fixture": entry.get("fixture", Path(entry["path"]).stem),
            "path": entry["path"],
            "manifestIndex": manifest_index,
            "fixtureSha256": entry.get("sha256"),
            "sourceFiles": entry.get("sourceFiles", []),
            "coverage": 0,
            "surfaceIds": entry.get("surfaceIds", []),
            "kind": "excluded",
            "status": "excluded",
            "salesforceEligible": False,
            "reason": entry.get("salesforceExclusionReason"),
            "org": None,
            "orgIdentity": None,
            "execution": "not-run",
            "exitCode": None,
            "deployable": False,
            "runtimeRequested": False,
            "runtimeStatus": "not-run",
            "runtimePassed": None,
        })
        seen_indexes.add(manifest_index)
    if args.manifest_index_modulus is None:
        for manifest_index, entry in enumerate(manifest_entries):
            key = manifest_fixture_key(entry)
            if manifest_index in seen_indexes:
                continue
            results.append({
                "fixture": entry.get("fixture", Path(entry["path"]).stem),
                "path": entry["path"],
                "manifestIndex": manifest_index,
                "fixtureSha256": entry.get("sha256"),
                "sourceFiles": entry.get("sourceFiles", []),
                "coverage": 0,
                "surfaceIds": [],
                "kind": "skipped",
                "status": "skipped-local-failure" if allowed_local is not None and key not in allowed_local else "not-selected",
                "reason": "local replay did not pass" if allowed_local is not None and key not in allowed_local else "outside bounded selection",
                "org": None,
                "orgIdentity": None,
            })
    expected_fixture_hashes = {}
    expected_source_files = {}
    if args.manifest:
        for entry in manifest_entries:
            expected_fixture_hashes[Path(entry["path"]).name] = entry.get("sha256")
            expected_fixture_hashes[str(entry.get("fixture", ""))] = entry.get("sha256")
            expected_source_files[Path(entry["path"]).name] = entry.get("sourceFiles", [])
            expected_source_files[str(entry.get("fixture", ""))] = entry.get("sourceFiles", [])
    for result in results:
        result["fixtureSha256"] = expected_fixture_hashes.get(result["fixture"])
        result["sourceFiles"] = expected_source_files.get(result["fixture"], result.get("sourceFiles", []))
        result["manifestSha256"] = file_sha256(args.manifest) if args.manifest else None
    results.sort(key=lambda r: (-r["coverage"], r["fixture"]))
    binding = {
        "manifestSha256": file_sha256(args.manifest) if args.manifest else None,
        "profileSha256": file_sha256(args.profile),
        "queueSha256": args.queue_sha256,
        "selectorSha256": args.selector_sha256,
        "selectorReceiptSha256": args.selector_receipt_sha256,
        "selectionSha256": selection_sha,
        "candidateCommit": args.candidate_commit,
        "candidateSha256": args.candidate_sha256,
        "toolsCommit": args.tools_commit,
        "toolsAmd64Sha256": args.tools_amd64_sha256,
        "workflowScriptSha256": provenance["workflowScriptSha256"],
        "orgPreflightSha256": org_preflight_sha,
        "localSummarySha256": file_sha256(args.local_summary) if args.local_summary else None,
    }
    if args.manifest_index_modulus is not None:
        binding["manifestIndexModulus"] = args.manifest_index_modulus
        binding["manifestIndexRemainder"] = args.manifest_index_remainder
    expected_indexes = expected_result_indexes(
        len(manifest_entries), selected, args.manifest_index_modulus is not None
    )
    expected_indexes.update(index for index, _entry in excluded_entries)
    result_payload = {
        "schemaVersion": 1,
        "sealed": (
            len(results) == len(expected_indexes)
            and {result.get("manifestIndex") for result in results} == expected_indexes
        ),
        "orgs": orgs,
        "binding": binding,
        "selectedFixtures": len(selected),
        "excludedFixtures": len(excluded_entries),
        "selectedRows": sum(r["coverage"] for r in results),
        "excludedRows": sum(entry.get("rowCount", 0) for _index, entry in excluded_entries),
        "runtimeRequested": args.runtime,
        "orgIdentities": org_identities,
        "workerExecution": {"sfBinary": args.sf_bin, "environment": {"SF_USE_GENERIC_UNIX_KEYCHAIN": "true"}},
        "orgPreflightSha256": org_preflight_sha,
        "localSummarySha256": binding["localSummarySha256"],
        "selectionSha256": selection_sha,
        "selectedManifestIndexes": sorted(selected_indexes),
        "orgPostflight": org_postflight,
        "skippedDeferredFixtures": skipped_deferred,
        "results": results,
    }
    if args.manifest_index_modulus is not None:
        result_payload["manifestIndexModulus"] = args.manifest_index_modulus
        result_payload["manifestIndexRemainder"] = args.manifest_index_remainder
    (args.out / "results.json").write_text(json.dumps(result_payload, indent=2) + "\n")
    failed = (
        bool(selected) and not results
        or any(result_failed(r) for r in results)
        or any(r.get("status") == "skipped-local-failure" for r in results)
        or any(args.runtime and r.get("kind") == "test" and r.get("runtimePassed") is not True for r in results)
        or any(r.get("orgCleanup") and not r["orgCleanup"].get("residueAbsent", False) for r in results)
        or bool(args.runtime and (not org_postflight or org_postflight.get("matchesPreflight") is not True))
    )
    deployable_fixtures, deployable_rows = deployable_counts(results)
    print(json.dumps({
        "selectedFixtures": len(selected),
        "selectedRows": sum(r["coverage"] for r in results),
        "salesforceDeployableFixtures": deployable_fixtures,
        "salesforceDeployableRows": deployable_rows,
        "out": str(args.out),
    }, indent=2))
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
