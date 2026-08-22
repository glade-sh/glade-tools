"""Tests for stable checked release-source receipts."""

import hashlib
import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
import export_source_receipt as exporter


class ExportSourceReceiptTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.snapshot = self.root / "snapshot"
        self.snapshot.mkdir()
        families = {}
        family_versions = {}
        for family in exporter.FAMILIES:
            directory = self.snapshot / family
            directory.mkdir()
            (directory / "page.md").write_text(f"{family}\n", encoding="utf-8")
            families[family] = {"path": str(directory), "sha256": "a" * 64, "file_count": 1}
            family_versions[family] = "258.0"
        lwc = self.snapshot / "lwc"
        lwc.mkdir()
        (lwc / "reference-api-modules.md").write_text("modules\n", encoding="utf-8")
        (lwc / "_version.json").write_text('{"version":"65.0-derived"}\n', encoding="utf-8")
        filter_receipt = {
            "schema_version": 1,
            "target_api_version": "65.0",
            "source_directory": "/private/source",
            "source_version": "latest",
            "source_version_metadata": {"file": "_version.json", "sha256": "b" * 64, "version": "latest"},
            "source_version_sha256": "c" * 64,
            "availability_table": "reference-api-modules.md",
            "availability_table_sha256": "d" * 64,
            "copied_markdown_files": 1,
            "copied": [{"path": "reference-api-modules.md", "sha256": "e" * 64}],
            "excluded": [],
            "limitation": "Salesforce publishes the LWC reference as current-release-only; this is an availability-filtered view.",
        }
        (lwc / "_filter-receipt.json").write_text(json.dumps(filter_receipt), encoding="utf-8")
        metadata = {
            "version": "258.0",
            "atlas_version_label": "API v65.0 (Winter '26)",
            "target_api_version": "65.0",
            "total_pages": 6,
            "docsets": [*exporter.FAMILIES, "lwc"],
            "family_versions": {**family_versions, "lwc": {"source_version": "latest", "target_api_version": "65.0", "receipt": "lwc/_filter-receipt.json"}},
            "limitations": [filter_receipt["limitation"]],
            "assembly": {
                "assembler": {"path": "/private/assemble.py", "sha256": "f" * 64},
                "versioned_source": {"path": "/private/atlas", "sha256": "1" * 64},
                "families": families,
            },
        }
        (self.snapshot / "_scrape-meta.json").write_text(json.dumps(metadata), encoding="utf-8")
        self.inventory = self.root / "inventory.json"
        self.inventory.write_text('{"schemaVersion":1}\n', encoding="utf-8")
        self.manifest = self.root / "manifest.json"
        self.manifest.write_text(json.dumps({
            "schemaVersion": 1,
            "release": "Winter '26",
            "apiVersion": "65.0",
            "digest": "2" * 64,
        }), encoding="utf-8")

    def tearDown(self):
        self.tmp.cleanup()

    def test_export_binds_source_trees_without_local_paths(self):
        receipt = exporter.build_receipt(self.snapshot, self.inventory, self.manifest)

        self.assertEqual("Winter '26", receipt["release"])
        self.assertEqual("258.0", receipt["snapshot"]["atlasVersion"])
        self.assertEqual("latest", receipt["snapshot"]["lwc"]["sourceVersion"])
        self.assertEqual(hashlib.sha256(self.inventory.read_bytes()).hexdigest(), receipt["inventorySHA256"])
        self.assertNotIn(str(self.root), json.dumps(receipt))

    def test_export_rejects_release_version_mismatch(self):
        manifest = json.loads(self.manifest.read_text(encoding="utf-8"))
        manifest["apiVersion"] = "66.0"
        self.manifest.write_text(json.dumps(manifest), encoding="utf-8")

        with self.assertRaises(ValueError):
            exporter.build_receipt(self.snapshot, self.inventory, self.manifest)


if __name__ == "__main__":
    unittest.main()
