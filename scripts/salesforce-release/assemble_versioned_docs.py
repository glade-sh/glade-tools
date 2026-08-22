#!/usr/bin/env python3
"""Assemble a six-family API-version docs snapshot from local sources."""

import argparse
import hashlib
import json
import re
import shutil
from pathlib import Path
from urllib.parse import urlparse


FAMILIES = ("apex", "visualforce", "lightning", "rest-api", "tooling-api")
MODULE_ROW = re.compile(
    r"^\|\s*\[([^]]+)\]\(([^)]+)\).*?\|\s*([0-9]+(?:\.[0-9]+)?)\s*\|\s*$"
)
CANONICAL_API_VERSION = re.compile(r"[1-9][0-9]*\.0\Z")


def sha256(path):
    digest = hashlib.sha256()
    with Path(path).open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def tree_sha256(directory):
    digest = hashlib.sha256()
    directory = Path(directory)
    for path in sorted(path for path in directory.rglob("*") if path.is_file()):
        digest.update(path.relative_to(directory).as_posix().encode())
        digest.update(sha256(path).encode())
    return digest.hexdigest()


def file_count(directory):
    return sum(1 for path in Path(directory).rglob("*") if path.is_file())


def canonical_api_version(version):
    version = str(version)
    if not CANONICAL_API_VERSION.fullmatch(version):
        raise ValueError(f"API version must be canonical N.0, got {version!r}")
    return version


def unavailable_lwc_pages(availability_table, target_major):
    pages = {}
    for line in Path(availability_table).read_text(encoding="utf-8").splitlines():
        match = MODULE_ROW.match(line)
        if not match or int(match.group(3).split(".", 1)[0]) <= target_major:
            continue
        page = Path(urlparse(match.group(2)).path).name
        if page.endswith(".html"):
            pages[Path(page).with_suffix(".md").name] = (match.group(1), match.group(3))
    return pages


def copy_tree(source, destination):
    copied = []
    for path in sorted(path for path in Path(source).rglob("*") if path.is_file()):
        relative = path.relative_to(source)
        target = Path(destination, relative)
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, target)
        copied.append({"path": relative.as_posix(), "sha256": sha256(path)})
    return copied


def read_json(path):
    with Path(path).open(encoding="utf-8") as source:
        return json.load(source)


def root_resolves_target(source_metadata, target_api_version):
    return (
        source_metadata.get("target_api_version") == target_api_version
        or f"API v{target_api_version}" in source_metadata.get("atlas_version_label", "")
    )


def validate_atlas_root(source_metadata, target_api_version):
    target_major = int(target_api_version.split(".", 1)[0])
    expected_version = f"{2 * target_major + 128}.0"
    source_version = source_metadata.get("version")
    if source_version != expected_version and not (
        source_version == "latest" and root_resolves_target(source_metadata, target_api_version)
    ):
        raise ValueError(
            f"Atlas root version {source_version!r} does not resolve to API {target_api_version} "
            f"(expected {expected_version!r})"
        )


def validate_atlas_family(family, source_directory, source_metadata, target_api_version):
    version_file = Path(source_directory, "_version.json")
    if not version_file.is_file():
        return
    metadata = read_json(version_file)
    pages = metadata.get("pages", {})
    if pages.get("empty", 0) or pages.get("failed", 0):
        raise ValueError(f"{family} source has empty or failed pages")
    version = metadata.get("version")
    root_version = source_metadata.get("version")
    if version != root_version and not (version == "latest" and root_resolves_target(source_metadata, target_api_version)):
        raise ValueError(f"{family} source version {version!r} does not match root version {root_version!r}")


def assemble(versioned_source, lwc_source, output, target_version):
    """Copy five versioned families and an availability-filtered LWC family."""
    versioned_source = Path(versioned_source)
    lwc_source = Path(lwc_source)
    output = Path(output)
    target_api_version = canonical_api_version(target_version)
    target_major = int(target_api_version.split(".", 1)[0])
    if output.exists():
        raise FileExistsError(f"output already exists: {output}")

    source_metadata = read_json(versioned_source / "_scrape-meta.json")
    if not source_metadata.get("version"):
        raise ValueError("versioned source metadata is missing version")
    validate_atlas_root(source_metadata, target_api_version)

    source_families = {}
    for family in FAMILIES:
        source_family = versioned_source / family
        if family == "lightning" and not source_family.is_dir():
            source_family = versioned_source / "lightning-aura"
        if not source_family.is_dir():
            raise FileNotFoundError(f"missing source family: {source_family}")
        destination_name = "lightning-aura" if family == "lightning" else family
        validate_atlas_family(destination_name, source_family, source_metadata, target_api_version)
        source_families[destination_name] = source_family
    assembly = {
        "assembler": {"path": str(Path(__file__).resolve()), "sha256": sha256(__file__)},
        "versioned_source": {"path": str(versioned_source.resolve()), "sha256": tree_sha256(versioned_source)},
        "families": {
            family: {
                "path": str(source.resolve()),
                "sha256": tree_sha256(source),
                "file_count": file_count(source),
            }
            for family, source in source_families.items()
        },
    }

    output.mkdir(parents=True)
    copied_families = {}
    for destination_name, source_family in source_families.items():
        copied_families[destination_name] = copy_tree(source_family, output / destination_name)

    availability_table = lwc_source / "reference-api-modules.md"
    unavailable = unavailable_lwc_pages(availability_table, target_major)
    source_version_file = lwc_source / "_version.json"
    source_version_metadata = {
        "file": source_version_file.name,
        "sha256": sha256(source_version_file),
        "version": read_json(source_version_file).get("version"),
    }
    lwc_destination = output / "lwc"
    copied_lwc = []
    excluded = []
    for path in sorted(path for path in lwc_source.rglob("*") if path.is_file()):
        relative = path.relative_to(lwc_source)
        if relative == Path("_version.json"):
            continue
        unavailable_module = unavailable.get(relative.as_posix())
        if unavailable_module:
            module, first_available = unavailable_module
            excluded.append(
                {
                    "file": relative.as_posix(),
                    "source_sha256": sha256(path),
                    "module": module,
                    "first_available_api_version": first_available,
                }
            )
            continue
        target = lwc_destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, target)
        copied_lwc.append({"path": relative.as_posix(), "sha256": sha256(path)})

    receipt = {
        "schema_version": 1,
        "target_api_version": target_api_version,
        "source_directory": str(lwc_source.resolve()),
        "source_version": source_version_metadata["version"],
        "source_version_metadata": source_version_metadata,
        "source_version_sha256": tree_sha256(lwc_source),
        "availability_table": availability_table.name,
        "availability_table_sha256": sha256(availability_table),
        "copied_markdown_files": sum(item["path"].endswith(".md") for item in copied_lwc),
        "copied": copied_lwc,
        "excluded": excluded,
        "limitation": "Salesforce publishes the LWC reference as current-release-only; this is an availability-filtered view.",
    }
    write_json(lwc_destination / "_filter-receipt.json", receipt)
    write_json(
        lwc_destination / "_version.json",
        {
            "name": "LWC Reference",
            "version": f"{target_api_version}-derived",
            "source_version": source_version_metadata["version"],
            "target_api_version": target_api_version,
            "filter_basis": "reference-api-modules.md First Available in API Version",
            "pages": {"total": receipt["copied_markdown_files"]},
            "excluded": [{"file": row["file"], "first_available_api_version": row["first_available_api_version"]} for row in excluded],
        },
    )
    family_versions = dict(source_metadata.get("family_versions", {}))
    for family in copied_families:
        family_versions.setdefault(family, source_metadata["version"])
    family_versions["lwc"] = {
        "source_version": source_version_metadata["version"],
        "target_api_version": target_api_version,
        "receipt": "lwc/_filter-receipt.json",
    }
    total_pages = sum(1 for path in output.rglob("*.md") if path.is_file())
    write_json(
        output / "_scrape-meta.json",
        {
            **source_metadata,
            "target_api_version": target_api_version,
            "total_pages": total_pages,
            "assembly": assembly,
            "docsets": ["apex", "lightning-aura", "lwc", "rest-api", "tooling-api", "visualforce"],
            "family_versions": family_versions,
            "limitations": [*source_metadata.get("limitations", []), receipt["limitation"]],
        },
    )


def write_json(path, value):
    Path(path).write_text(json.dumps(value, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", required=True, type=Path, help="versioned Atlas source directory")
    parser.add_argument("--lwc-source", required=True, type=Path, help="current-only LWC source directory")
    parser.add_argument("--output", required=True, type=Path, help="new snapshot output directory")
    parser.add_argument("--target-api-version", "--version", required=True)
    args = parser.parse_args()
    assemble(args.source, args.lwc_source, args.output, args.target_api_version)


if __name__ == "__main__":
    main()
