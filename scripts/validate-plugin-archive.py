#!/usr/bin/env python3
"""Fail closed unless a plugin archive contains complete, safe license evidence."""
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys
import tarfile
import tempfile


CHECKSUM_ROW = re.compile(r"^([0-9a-f]{64})  (.+)$")


def fail(message):
    raise SystemExit(message)


def safe_path(name, label):
    path = PurePosixPath(name)
    if not name or "\\" in name or path.is_absolute() or ".." in path.parts or str(path) != name.rstrip("/"):
        fail(f"unsafe {label}: {name}")
    return name.rstrip("/")


def linked_modules(binary):
    with tempfile.NamedTemporaryFile() as executable:
        executable.write(binary)
        executable.flush()
        try:
            output = subprocess.check_output(
                ["go", "version", "-m", executable.name],
                text=True,
                stderr=subprocess.STDOUT,
            )
        except (OSError, subprocess.CalledProcessError) as error:
            fail(f"cannot read linked module metadata: {error}")
    modules = set()
    for line in output.splitlines():
        fields = line.split()
        if len(fields) >= 3 and fields[0] == "dep":
            modules.add((fields[1], fields[2]))
    return modules


def main(archive_arg, binary):
    archive = Path(archive_arg)
    if not archive.is_file() or archive.is_symlink():
        fail(f"archive is missing or unsafe: {archive}")

    files = {}
    names = set()
    with tarfile.open(archive, "r:gz") as source:
        for member in source:
            name = safe_path(member.name, "archive member")
            if name in names:
                fail(f"duplicate archive member: {name}")
            names.add(name)
            if not member.isfile() and not member.isdir():
                fail(f"unsafe archive member type: {name}")
            if member.isfile():
                extracted = source.extractfile(member)
                if extracted is None:
                    fail(f"cannot read archive member: {name}")
                files[name] = extracted.read()

    required = [f"bin/{binary}", "plugin.json", "LICENSE", "NOTICE", "checksums.txt", "THIRD_PARTY_NOTICES/NOTICE-MANIFEST.json"]
    for name in required:
        if name not in files:
            fail(f"missing {name}")
        if not files[name]:
            fail(f"empty {name}")

    try:
        manifest = json.loads(files["THIRD_PARTY_NOTICES/NOTICE-MANIFEST.json"])
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        fail(f"invalid notice manifest: {error}")
    if manifest.get("schemaVersion") != 1:
        fail("invalid notice manifest schema")
    binary_sha = hashlib.sha256(files[f"bin/{binary}"]).hexdigest()
    if manifest.get("binarySHA256") != binary_sha:
        fail("notice manifest binary checksum mismatch")

    references = []
    go_license = manifest.get("goLicense")
    if not isinstance(go_license, str):
        fail("missing Go license reference")
    references.append(go_license)
    components = manifest.get("components")
    if not isinstance(components, list) or not components:
        fail("notice manifest has no linked components")
    declared_modules = set()
    for component in components:
        notice_files = component.get("noticeFiles") if isinstance(component, dict) else None
        module = component.get("module") if isinstance(component, dict) else None
        version = component.get("version") if isinstance(component, dict) else None
        if not isinstance(module, str) or not module or not isinstance(version, str) or not version:
            fail("linked component has invalid module metadata")
        if (module, version) in declared_modules:
            fail("linked component is duplicated")
        declared_modules.add((module, version))
        if not isinstance(notice_files, list) or not notice_files:
            fail("linked component has no notice files")
        references.extend(notice_files)
    if declared_modules != linked_modules(files[f"bin/{binary}"]):
        fail("linked component set mismatch")

    referenced_paths = set()
    for reference in references:
        if not isinstance(reference, str):
            fail("unsafe notice reference")
        path = safe_path(reference, "notice reference")
        if path.startswith("THIRD_PARTY_NOTICES/") or path in referenced_paths:
            fail(f"unsafe notice reference: {reference}")
        referenced_paths.add(path)
        archive_path = "THIRD_PARTY_NOTICES/" + path
        if archive_path not in files:
            fail(f"missing referenced notice: {path}")
        if not files[archive_path]:
            fail(f"empty referenced notice: {path}")

    packaged_notices = {name.removeprefix("THIRD_PARTY_NOTICES/") for name in files if name.startswith("THIRD_PARTY_NOTICES/") and name != "THIRD_PARTY_NOTICES/NOTICE-MANIFEST.json"}
    if packaged_notices != referenced_paths:
        fail("notice manifest does not exactly cover packaged notices")

    try:
        rows = files["checksums.txt"].decode().splitlines()
    except UnicodeDecodeError as error:
        fail(f"invalid checksums.txt: {error}")
    checksums = {}
    for row in rows:
        match = CHECKSUM_ROW.fullmatch(row)
        if not match:
            fail(f"invalid checksum row: {row}")
        digest, name = match.groups()
        safe_path(name, "checksum path")
        if name in checksums:
            fail(f"duplicate checksum row: {name}")
        checksums[name] = digest
    checked_files = {name: contents for name, contents in files.items() if name != "checksums.txt"}
    if set(checksums) != set(checked_files):
        fail("checksums do not exactly cover archive files")
    for name, contents in checked_files.items():
        if checksums[name] != hashlib.sha256(contents).hexdigest():
            fail(f"checksum mismatch: {name}")


if __name__ == "__main__":
    if len(sys.argv) != 3:
        fail("usage: validate-plugin-archive.py <archive> <binary-name>")
    main(*sys.argv[1:])
