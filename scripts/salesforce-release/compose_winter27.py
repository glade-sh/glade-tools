#!/usr/bin/env python3
"""Compose Winter '27 provenance from a local 264 scrape and checked 262 receipts.

Carried inventory documents come from the immutable checked previous inventory,
not the mutable current-only docs directory. No network or Salesforce commands.
"""
import argparse
import copy
import hashlib
import json
from pathlib import Path

from assemble_versioned_docs import file_count, sha256, tree_sha256, write_json

GENERATOR = "scripts/salesforce-release/compose_winter27.py"
CARRIES = ("lightning-aura", "rest-api", "visualforce")
ADDITIONAL = ("apex-guide", "metadata-api", "object-reference", "release-notes")


def read(path):
    return json.loads(Path(path).read_text(encoding="utf-8"))


def compose(atlas, notes, previous_receipt, previous_inventory, inventory, manifest):
    atlas, notes = Path(atlas), Path(notes)
    prior, prior_inventory, current_inventory, release = map(read, (previous_receipt, previous_inventory, inventory, manifest))
    if prior["apiVersion"] != "67.0" or prior["snapshot"]["atlasVersion"] != "262.0" or release["apiVersion"] != "68.0":
        raise ValueError("composition requires API 68 and its previous GA API 67 receipt")
    meta = read(atlas / "_scrape-meta.json")
    if meta["version"] != "264.0":
        raise ValueError("Atlas source must be the explicit 264.0 scrape")
    if sha256(previous_inventory) != prior["inventorySHA256"]:
        raise ValueError("previous inventory differs from the checked previous GA receipt")
    def carried_documents(value):
        return [row for row in value["documents"] if row["sourcePath"].split("/")[0] in (*CARRIES, "lwc")]
    if carried_documents(prior_inventory) != carried_documents(current_inventory):
        raise ValueError("carried inventory documents differ from the checked previous GA")
    previous_hash = sha256(previous_receipt)
    carry = {"carriedFrom": "262.0", "carriedReceiptSHA256": previous_hash, "unpublished": True}
    families = []
    for row in prior["snapshot"]["families"]:
        name = row["name"]
        if name in CARRIES:
            families.append({**row, **carry})
        else:
            directory = atlas / name
            identity = read(directory / "_version.json")
            if identity["version"] != "264.0":
                raise ValueError(f"{name} must publish 264.0")
            families.append({"name": name, "version": "264.0", "fileCount": file_count(directory), "sha256": tree_sha256(directory)})
    additional = []
    for name in ADDITIONAL:
        directory = notes if name == "release-notes" else atlas / name
        additional.append({"name": name, "version": "264.0", "fileCount": file_count(directory), "sha256": tree_sha256(directory)})
    lwc = {**copy.deepcopy(prior["snapshot"]["lwc"]), **carry}
    composition = {
        "schemaVersion": 1, "atlasVersion": "264.0", "targetAPIVersion": "68.0",
        "versionedSourceSHA256": tree_sha256(atlas), "families": families,
        "additionalFamilies": additional, "carriedInventorySHA256": sha256(previous_inventory),
        "previousSourceReceiptSHA256": previous_hash, "lwc": lwc,
    }
    # The snapshot identity names this composition, not a nonexistent copied
    # tree. Its independently bound components are the physical 264 tree and
    # the immutable checked previous-GA receipt/inventory.
    composition_hash = hashlib.sha256((json.dumps(composition, indent=2, sort_keys=True) + "\n").encode()).hexdigest()
    receipt = {
        "schemaVersion": 1, "release": release["release"], "apiVersion": "68.0",
        "manifestDigest": release["digest"], "inventorySHA256": sha256(inventory),
        "generator": {"path": GENERATOR, "sha256": sha256(__file__)},
        "snapshot": {
            "sha256": composition_hash, "metadataSHA256": composition_hash,
            "compositionReceipt": {"path": "salesforce-release-composition-winter-27.json", "sha256": composition_hash},
            "atlasVersion": "264.0", "atlasVersionLabel": "API v68.0 (Winter '27)",
            "targetAPIVersion": "68.0", "totalPages": current_inventory["totalFiles"],
            "assembler": {"path": GENERATOR, "sha256": sha256(__file__)},
            "versionedSourceSHA256": composition["versionedSourceSHA256"],
            "families": families, "additionalFamilies": additional, "lwc": lwc,
            "limitations": [lwc["limitation"], "Aura, LWC, REST and Visualforce 264.0 docs are unpublished; explicit 262.0 carries bind the checked previous GA receipt and normalized inventory, not refreshed current-only documentation.", "Snapshot and metadata hashes identify salesforce-release-composition-winter-27.json: a composition of the physical 264.0 source and checked 262.0 receipt/inventory, not copied 262.0 source bytes."],
        },
    }
    return composition, receipt


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for name in ("atlas", "notes", "previous-receipt", "previous-inventory", "inventory", "manifest", "composition-output", "receipt-output"):
        p.add_argument("--" + name, required=True, type=Path)
    a = p.parse_args()
    composition, receipt = compose(a.atlas, a.notes, a.previous_receipt, a.previous_inventory, a.inventory, a.manifest)
    write_json(a.composition_output, composition)
    write_json(a.receipt_output, receipt)


if __name__ == "__main__":
    main()
