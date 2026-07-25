#!/usr/bin/env python3
"""Build the first-party plugin registry from released plugin archives."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import sys
import tarfile
import tempfile
from pathlib import Path
from urllib.parse import urlsplit


# Command roots deliberately do not live here. They are derived from each
# packaged manifest so the registry cannot drift from the archive users install.
PLUGIN_METADATA = {
    "compat": {
        "name": "@glade/compat",
        "aliases": ["compat"],
        "summary": "Maintainer support tools, fixtures, surface ledgers, and parity scanners.",
        "docsURL": "https://glade.sh/maintainer/glade-tools",
    },
    "orgpackage": {
        "name": "@glade/orgpackage",
        "aliases": ["orgpackage"],
        "summary": "Capture installed Salesforce package artifacts from an org.",
        "docsURL": "https://glade.sh/guide/rich-local-workflows",
    },
    "performance": {
        "name": "@glade/performance",
        "aliases": ["performance"],
        "summary": "Advisory Salesforce performance scanner.",
        "docsURL": "https://glade.sh/guide/plugins/first-party",
    },
}


def fail(message: str) -> None:
    raise ValueError(message)


def release_version(value: str) -> str:
    version = value.removeprefix("v")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", version) or version in {".", ".."} or ".." in version:
        fail(f"invalid version: {value}")
    return version


def asset_base_url(value: str) -> str:
    parsed = urlsplit(value)
    if (
        parsed.scheme != "https"
        or not parsed.netloc
        or parsed.username is not None
        or parsed.password is not None
        or parsed.query
        or parsed.fragment
        or any(character.isspace() for character in value)
    ):
        fail(f"invalid asset base URL: {value!r}")
    return value.rstrip("/")


def read_checksums(path: Path) -> dict[str, str]:
    rows: dict[str, str] = {}
    try:
        contents = path.read_text(encoding="utf-8")
    except OSError as error:
        fail(f"read checksums {path}: {error}")
    for line_number, raw_line in enumerate(contents.splitlines(), start=1):
        line = raw_line.strip()
        if not line:
            continue
        fields = line.split()
        if len(fields) != 2 or not re.fullmatch(r"[0-9a-fA-F]{64}", fields[0]):
            fail(f"invalid checksum row at {path}:{line_number}")
        checksum, name = fields
        if Path(name).name != name:
            fail(f"checksum row must name an archive basename at {path}:{line_number}")
        if name in rows:
            fail(f"duplicate checksum row for {name}")
        rows[name] = checksum.lower()
    return rows


def archive_checksum(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as archive:
        for chunk in iter(lambda: archive.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def manifest_from_archive(path: Path) -> dict[str, object]:
    try:
        with tarfile.open(path, "r:gz") as archive:
            members = [member for member in archive.getmembers() if member.name == "plugin.json"]
            if not members:
                fail(f"archive {path.name} is missing plugin.json")
            if len(members) != 1:
                fail(f"archive {path.name} contains duplicate plugin.json members")
            member = members[0]
            if not member.isfile():
                fail(f"archive {path.name} plugin.json is not a regular file")
            handle = archive.extractfile(member)
            if handle is None:
                fail(f"archive {path.name} cannot read plugin.json")
            with handle:
                manifest = json.load(handle)
    except (OSError, tarfile.TarError, json.JSONDecodeError) as error:
        fail(f"read plugin manifest from {path.name}: {error}")
    if not isinstance(manifest, dict):
        fail(f"archive {path.name} plugin.json must be an object")
    return manifest


def command_roots(manifest: dict[str, object], archive_name: str) -> list[str]:
    commands = manifest.get("commands")
    if not isinstance(commands, list):
        fail(f"archive {archive_name} plugin.json commands must be an array")
    roots: set[str] = set()
    for command in commands:
        if not isinstance(command, dict):
            fail(f"archive {archive_name} plugin.json command must be an object")
        path = command.get("path")
        if not isinstance(path, list) or not path or not isinstance(path[0], str) or not path[0]:
            fail(f"archive {archive_name} plugin.json command path must start with a non-empty string")
        roots.add(command["path"][0])
    if not roots:
        fail(f"archive {archive_name} plugin.json must declare at least one command root")
    return sorted(roots)


def build_registry(version: str, base_url: str, archive_dir: Path, checksums: dict[str, str]) -> dict[str, object]:
    archive_pattern = re.compile(
        rf"^glade-plugin-([A-Za-z0-9][A-Za-z0-9-]*)_{re.escape(version)}_([a-z0-9]+)_([a-z0-9]+)\.tar\.gz$"
    )
    rows: dict[str, dict[str, object]] = {}
    command_roots_by_plugin: dict[str, list[str]] = {}
    seen_platforms: set[tuple[str, str, str]] = set()
    archives = sorted(archive_dir.glob("glade-plugin-*.tar.gz"), key=lambda path: path.name)
    if not archives:
        fail(f"no plugin archives found in {archive_dir}")
    archive_names = {archive.name for archive in archives}
    checksum_names = set(checksums)
    if archive_names != checksum_names:
        missing = sorted(archive_names - checksum_names)
        unexpected = sorted(checksum_names - archive_names)
        fail(f"checksum rows do not exactly cover plugin archives (missing={missing}, unexpected={unexpected})")

    for archive in archives:
        match = archive_pattern.fullmatch(archive.name)
        if match is None:
            fail(f"invalid plugin archive name or version: {archive.name}")
        plugin, goos, goarch = match.groups()
        if plugin not in PLUGIN_METADATA:
            fail(f"unsupported first-party plugin archive: {archive.name}")
        platform = (plugin, goos, goarch)
        if platform in seen_platforms:
            fail(f"duplicate plugin archive platform: {archive.name}")
        seen_platforms.add(platform)

        expected_checksum = checksums.get(archive.name)
        if expected_checksum is None:
            fail(f"checksums file is missing {archive.name}")
        actual_checksum = archive_checksum(archive)
        if actual_checksum != expected_checksum:
            fail(f"checksum mismatch for {archive.name}")

        manifest = manifest_from_archive(archive)
        if manifest.get("name") != plugin:
            fail(f"archive {archive.name} manifest name does not match archive plugin")
        if manifest.get("version") != version:
            fail(f"archive {archive.name} manifest version does not match release version")
        roots = command_roots(manifest, archive.name)
        previous_roots = command_roots_by_plugin.setdefault(plugin, roots)
        if previous_roots != roots:
            fail(f"manifest command roots differ across platform archives for {plugin}")

        if plugin not in rows:
            metadata = PLUGIN_METADATA[plugin]
            rows[plugin] = {
                **metadata,
                "version": version,
                "publisher": "glade",
                "trust": "first-party",
                "commands": roots,
                "sourceURL": "https://github.com/glade-sh/glade-tools",
                "minimumGladeVersion": "0.1.0",
                "assets": [],
            }
        assets = rows[plugin]["assets"]
        assert isinstance(assets, list)
        assets.append(
            {
                "os": goos,
                "arch": goarch,
                "url": f"{base_url.rstrip('/')}/{archive.name}",
                "sha256": actual_checksum,
            }
        )

    for row in rows.values():
        assets = row["assets"]
        assert isinstance(assets, list)
        assets.sort(key=lambda asset: (asset["os"], asset["arch"], asset["url"]))
    missing_plugins = sorted(set(PLUGIN_METADATA) - set(rows))
    if missing_plugins:
        fail(f"missing required first-party plugin archives: {', '.join(missing_plugins)}")
    return {"version": 1, "plugins": [rows[name] for name in sorted(rows)]}


def write_registry(path: Path, registry: dict[str, object]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    contents = json.dumps(registry, indent=2, sort_keys=True) + "\n"
    descriptor, temporary_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as output:
            output.write(contents)
            output.flush()
            os.fsync(output.fileno())
        try:
            os.link(temporary_name, path)
        except FileExistsError:
            fail(f"output already exists: {path}")
    finally:
        try:
            os.unlink(temporary_name)
        except FileNotFoundError:
            pass


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--asset-base-url", required=True)
    parser.add_argument("--archive-dir", required=True, type=Path)
    parser.add_argument("--checksums", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()

    try:
        version = release_version(args.version)
        base_url = asset_base_url(args.asset_base_url)
        registry = build_registry(version, base_url, args.archive_dir, read_checksums(args.checksums))
        write_registry(args.output, registry)
    except ValueError as error:
        print(f"build-plugin-registry: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
