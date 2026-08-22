#!/usr/bin/env python3
"""Export a stable checked receipt from an assembled Salesforce docs snapshot."""

import argparse
import json
import re
from pathlib import Path, PurePosixPath

from assemble_versioned_docs import canonical_api_version, sha256, tree_sha256, write_json


FAMILIES = ("apex", "lightning-aura", "rest-api", "tooling-api", "visualforce")
SHA256 = re.compile(r"[0-9a-f]{64}\Z")
GENERATOR_PATH = "scripts/salesforce-release/export_source_receipt.py"
ASSEMBLER_PATH = "scripts/salesforce-release/assemble_versioned_docs.py"


def read_exact_json(path):
    def exact_object(pairs):
        value = {}
        for key, item in pairs:
            if key in value:
                raise ValueError(f"duplicate JSON key {key!r} in {path}")
            value[key] = item
        return value

    with Path(path).open(encoding="utf-8") as source:
        return json.load(source, object_pairs_hook=exact_object)


def require_sha256(value, label):
    if not isinstance(value, str) or not SHA256.fullmatch(value):
        raise ValueError(f"{label} must be a lowercase SHA-256")
    return value


def require_relative_path(value, label):
    if not isinstance(value, str) or not value or "\\" in value:
        raise ValueError(f"{label} must be a relative POSIX path")
    path = PurePosixPath(value)
    if path.is_absolute() or ".." in path.parts:
        raise ValueError(f"{label} must be a relative POSIX path")
    return value


def checked_rows(rows, label, excluded=False):
    result = []
    for index, row in enumerate(rows):
        item = {
            "path" if not excluded else "file": require_relative_path(
                row["path" if not excluded else "file"], f"{label}[{index}] path"
            ),
            "sha256" if not excluded else "sourceSHA256": require_sha256(
                row["sha256" if not excluded else "source_sha256"], f"{label}[{index}] SHA-256"
            ),
        }
        if excluded:
            item["module"] = row["module"]
            item["firstAvailableAPIVersion"] = canonical_api_version(row["first_available_api_version"])
        result.append(item)
    return result


def build_receipt(snapshot, inventory, manifest):
    snapshot, inventory, manifest = Path(snapshot), Path(inventory), Path(manifest)
    metadata_path = snapshot / "_scrape-meta.json"
    filter_path = snapshot / "lwc" / "_filter-receipt.json"
    metadata = read_exact_json(metadata_path)
    lwc = read_exact_json(filter_path)
    manifest_data = read_exact_json(manifest)

    api_version = canonical_api_version(manifest_data["apiVersion"])
    if metadata.get("target_api_version") != api_version or lwc.get("target_api_version") != api_version:
        raise ValueError("snapshot, LWC, and manifest API versions must match")
    atlas_version = f"{2 * int(api_version.split('.', 1)[0]) + 128}.0"
    if metadata.get("version") != atlas_version:
        raise ValueError(f"Atlas version must be {atlas_version} for API {api_version}")
    if set(metadata.get("docsets", [])) != {*FAMILIES, "lwc"}:
        raise ValueError("snapshot docsets must contain the five Atlas families and LWC")

    assembly = metadata["assembly"]
    family_versions = metadata["family_versions"]
    if set(assembly["families"]) != set(FAMILIES):
        raise ValueError("assembly family set mismatch")
    families = []
    for family in FAMILIES:
        source = assembly["families"][family]
        if family_versions.get(family) != atlas_version or not isinstance(source.get("file_count"), int) or source["file_count"] <= 0:
            raise ValueError(f"invalid {family} source identity")
        families.append({
            "name": family,
            "version": atlas_version,
            "fileCount": source["file_count"],
            "sha256": require_sha256(source.get("sha256"), f"{family} source"),
        })

    lwc_family = family_versions.get("lwc", {})
    if lwc.get("schema_version") != 1 or lwc.get("source_version") != "latest":
        raise ValueError("LWC source must retain current-only latest provenance")
    if lwc_family != {"receipt": "lwc/_filter-receipt.json", "source_version": "latest", "target_api_version": api_version}:
        raise ValueError("LWC family identity mismatch")
    copied = checked_rows(lwc.get("copied", []), "copied")
    excluded = checked_rows(lwc.get("excluded", []), "excluded", excluded=True)
    copied_markdown = sum(row["path"].endswith(".md") for row in copied)
    if copied_markdown != lwc.get("copied_markdown_files") or copied_markdown <= 0:
        raise ValueError("LWC copied Markdown count mismatch")
    limitation = lwc.get("limitation", "")
    if "current-release-only" not in limitation or "availability-filtered" not in limitation:
        raise ValueError("LWC limitation must state its current-only filtered provenance")

    return {
        "schemaVersion": 1,
        "release": manifest_data["release"],
        "apiVersion": api_version,
        "manifestDigest": require_sha256(manifest_data["digest"], "manifest digest"),
        "inventorySHA256": sha256(inventory),
        "generator": {"path": GENERATOR_PATH, "sha256": sha256(__file__)},
        "snapshot": {
            "sha256": tree_sha256(snapshot),
            "metadataSHA256": sha256(metadata_path),
            "atlasVersion": atlas_version,
            "atlasVersionLabel": metadata["atlas_version_label"],
            "targetAPIVersion": api_version,
            "totalPages": metadata["total_pages"],
            "assembler": {
                "path": ASSEMBLER_PATH,
                "sha256": require_sha256(assembly["assembler"]["sha256"], "assembler"),
            },
            "versionedSourceSHA256": require_sha256(assembly["versioned_source"]["sha256"], "versioned source"),
            "families": families,
            "lwc": {
                "filterReceiptSHA256": sha256(filter_path),
                "sourceVersion": "latest",
                "sourceVersionSHA256": require_sha256(lwc["source_version_sha256"], "LWC source"),
                "sourceVersionMetadata": {
                    "file": require_relative_path(lwc["source_version_metadata"]["file"], "LWC version metadata"),
                    "version": lwc["source_version_metadata"]["version"],
                    "sha256": require_sha256(lwc["source_version_metadata"]["sha256"], "LWC version metadata"),
                },
                "availabilityTable": require_relative_path(lwc["availability_table"], "LWC availability table"),
                "availabilityTableSHA256": require_sha256(lwc["availability_table_sha256"], "LWC availability table"),
                "copiedMarkdownFiles": copied_markdown,
                "copied": copied,
                "excluded": excluded,
                "limitation": limitation,
            },
            "limitations": metadata.get("limitations", []),
        },
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--snapshot", required=True, type=Path)
    parser.add_argument("--inventory", required=True, type=Path)
    parser.add_argument("--manifest", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    write_json(args.output, build_receipt(args.snapshot, args.inventory, args.manifest))


if __name__ == "__main__":
    main()
