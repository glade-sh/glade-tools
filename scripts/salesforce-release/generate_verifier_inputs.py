#!/usr/bin/env python3
"""Generate API 68 verifier targets from an admitted public declaration review.

This prepares requests for a future verifier run. It never creates observations
or native proof, changes the published product pin, or contacts Salesforce.
"""

import argparse
import json
import re
from pathlib import Path
from textwrap import indent
from urllib.parse import urlparse


CASE_ID = "APEX-API68-CRYPTO-CBC-DECLARATION"
SURFACE_ID = "apex:System.Crypto.encrypt(String,Blob,Blob,Blob)"
TEST = "internal/vm/stdlib_test.go:TestExecCryptoEncryptAESCBCDeterministicLocalSubset"


def read(path):
    return json.loads(Path(path).read_text())


def plan(contract_path, catalog_path, runtime_path, project_path):
    contract_path, catalog_path, runtime_path, project_path = map(
        Path, (contract_path, catalog_path, runtime_path, project_path)
    )
    contract = read(contract_path)
    target = contract["releases"][-1]["apiVersion"]
    if target != "68.0":
        raise ValueError("the reviewed compiler template requires API 68.0")
    review = read(contract_path.parent / "salesforce-release-review-summer-26-to-winter-27.json")
    matches = [d for d in review["decisions"] if d["surfaceId"] == SURFACE_ID]
    if len(matches) != 1:
        raise ValueError("exact reviewed Crypto declaration is required")
    declaration = matches[0]
    source = urlparse(declaration["sourceURL"])
    if (
        declaration["disposition"] != "existing-case"
        or declaration["scope"] == "outside-claim"
        or TEST not in declaration["productTests"]
        or source.scheme != "https"
        or source.hostname != "developer.salesforce.com"
        or "264.0" not in source.path
        or not declaration["declaration"].startswith("public static Blob encrypt(")
    ):
        raise ValueError("implemented, test-bound public API 68 Crypto declaration is required")
    rule = {
        "id": CASE_ID,
        "area": "crypto-declarations",
        "docsPath": declaration["sourceURL"],
        "docsLines": declaration["declaration"],
        "apiVersion": 68,
        "sourceKind": "class",
        "source": "public class ReleaseCryptoDeclaration68 { public static Blob encrypt(Blob key, Blob iv, Blob clearText) { return System.Crypto.encrypt('AES256', key, iv, clearText); } }",
        "oracle": "accept",
        "owner": "vm",
        "status": "supported",
        "productTest": TEST,
    }
    catalog_text = catalog_path.read_text()
    catalog = json.loads(catalog_text)
    existing = [r for r in catalog["rules"] if r["id"] == CASE_ID]
    if existing and existing != [rule]:
        raise ValueError("the generated compiler case differs from the reviewed template")
    if not existing:
        # Preserve every existing catalog byte, including the CI product pin.
        end = re.search(r"\n  ]\n}\n?\Z", catalog_text)
        if end is None or not catalog["rules"]:
            raise ValueError("expected the existing nonempty catalog format")
        catalog_text = catalog_text[: end.start()] + ",\n" + indent(json.dumps(rule, indent=2), "    ") + "\n  ]\n}\n"
    runtime = read(runtime_path)
    if any(
        row.get("status") != "inconclusive" or row.get("sfObservation") or row.get("gladeObservation")
        for row in runtime["comparisons"]
    ):
        raise ValueError("cannot retarget a fixture containing observations or conclusive credit")
    runtime["apiVersion"] = target
    for window in contract["windows"]["source"]:
        if window["version"] == target:
            window["proofCases"] = sorted(set(window.get("proofCases", [])) | {CASE_ID})
    behavior = next(b for b in contract["behaviors"] if b["id"] == "apex.crypto.cbc-encrypt.declaration")
    behavior["proofCases"] = sorted(set(behavior.get("proofCases", [])) | {CASE_ID})
    project_file = project_path / "sfdx-project.json"
    project_text, count = re.subn(
        r'("sourceApiVersion"\s*:\s*")[^"]+(")',
        lambda m: m[1] + target + m[2], project_file.read_text()
    )
    if count != 1:
        raise ValueError("one project sourceApiVersion is required")
    updates = {
        catalog_path: catalog_text,
        runtime_path: json.dumps(runtime, indent=2) + "\n",
        contract_path: json.dumps(contract, indent=2) + "\n",
        project_file: project_text,
    }
    metadata = sorted(project_path.glob("force-app/**/*.cls-meta.xml"))
    if not metadata:
        raise ValueError("Apex lifecycle class metadata is required")
    for path in metadata:
        text, count = re.subn(r"(<apiVersion>)[^<]+(</apiVersion>)", lambda m: m[1] + target + m[2], path.read_text())
        if count != 1:
            raise ValueError("one API version per Apex class metadata file is required")
        updates[path] = text
    return updates


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--contract", type=Path, required=True)
    parser.add_argument("--catalog", type=Path, required=True)
    parser.add_argument("--runtime-cases", type=Path, required=True)
    parser.add_argument("--test-project", type=Path, required=True)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--write", action="store_true")
    mode.add_argument("--check", action="store_true")
    args = parser.parse_args()
    updates = plan(args.contract, args.catalog, args.runtime_cases, args.test_project)
    drift = False
    for path, text in updates.items():
        if path.read_text() == text:
            print("current:", path)
        elif args.write:
            path.write_text(text)
            print("wrote:", path)
        else:
            drift = True
            print("drift:", path)
    return int(drift)


if __name__ == "__main__":
    raise SystemExit(main())
