"""Tests for assembling a versioned Salesforce docs snapshot."""

import hashlib
import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
import assemble_versioned_docs as assembler


class AssembleVersionedDocsTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.source = self.root / "versioned"
        self.lwc_source = self.root / "lwc-current"
        self._write_versioned_source()
        self._write_lwc_source()

    def tearDown(self):
        self.tmp.cleanup()

    def _write_versioned_source(self):
        for family in ("apex", "visualforce", "lightning", "rest-api", "tooling-api"):
            (self.source / family).mkdir(parents=True)
            (self.source / family / "page.md").write_text(f"{family}\n", encoding="utf-8")
            (self.source / family / "_version.json").write_text(
                json.dumps({"version": "258.0", "pages": {"empty": 0, "failed": 0}}), encoding="utf-8"
            )
        (self.source / "_scrape-meta.json").write_text(
            json.dumps(
                {
                    "version": "258.0",
                    "atlas_version_label": "API v65.0 (Winter '26')",
                    "total_pages": 99,
                    "tool": "tools/scrape.py",
                    "tool_sha256": "atlas-tool-hash",
                    "family_versions": {
                        **{family: "258.0" for family in ("apex", "lightning-aura", "rest-api", "tooling-api")},
                        "visualforce": "258.0-source-receipt",
                    },
                }
            ),
            encoding="utf-8",
        )

    def _write_lwc_source(self):
        self.lwc_source.mkdir()
        (self.lwc_source / "reference-api-modules.md").write_text(
            "| API Module Name | First Available in API Version |\n"
            "| --- | --- |\n"
            "| [experience/blockBuilderApi](reference-experience-block-builder-api.html) | 66.0 |\n"
            "| [lightning/currentApi](reference-lightning-current-api.html) | 65.0 |\n",
            encoding="utf-8",
        )
        (self.lwc_source / "reference-experience-block-builder-api.md").write_text(
            "# `experience/blockBuilderApi` Module\n", encoding="utf-8"
        )
        (self.lwc_source / "reference-lightning-current-api.md").write_text(
            "# `lightning/currentApi` Module\n", encoding="utf-8"
        )
        (self.lwc_source / "guide.md").write_text("always copied\n", encoding="utf-8")
        (self.lwc_source / "_version.json").write_text(
            json.dumps({"version": "latest"}), encoding="utf-8"
        )

    def _assemble(self, version):
        output = self.root / f"out-{version}"
        assembler.assemble(self.source, self.lwc_source, output, version)
        return output

    def _set_atlas_source_version(self, version, api_version):
        metadata_path = self.source / "_scrape-meta.json"
        metadata = json.loads(metadata_path.read_text(encoding="utf-8"))
        metadata["version"] = version
        metadata["atlas_version_label"] = f"API v{api_version}"
        metadata_path.write_text(json.dumps(metadata), encoding="utf-8")
        for family in ("apex", "visualforce", "lightning", "rest-api", "tooling-api"):
            version_path = self.source / family / "_version.json"
            family_metadata = json.loads(version_path.read_text(encoding="utf-8"))
            family_metadata["version"] = version
            version_path.write_text(json.dumps(family_metadata), encoding="utf-8")

    def test_target_65_excludes_module_first_available_in_66(self):
        output = self._assemble("65.0")

        self.assertFalse((output / "lwc/reference-experience-block-builder-api.md").exists())
        self.assertTrue((output / "lwc/reference-lightning-current-api.md").exists())

    def test_target_66_keeps_module_first_available_in_66(self):
        self._set_atlas_source_version("260.0", "66.0")
        output = self._assemble("66.0")

        self.assertTrue((output / "lwc/reference-experience-block-builder-api.md").exists())

    def test_lightning_source_is_normalized_to_lightning_aura(self):
        output = self._assemble("65.0")

        self.assertEqual("lightning\n", (output / "lightning-aura/page.md").read_text(encoding="utf-8"))
        self.assertFalse((output / "lightning").exists())

    def test_receipt_records_copied_file_sha256(self):
        output = self._assemble("65.0")

        receipt = json.loads((output / "lwc/_filter-receipt.json").read_text(encoding="utf-8"))
        expected = hashlib.sha256(b"# `lightning/currentApi` Module\n").hexdigest()
        copied = {item["path"]: item["sha256"] for item in receipt["copied"]}
        self.assertEqual(expected, copied["reference-lightning-current-api.md"])
        self.assertEqual("65.0", json.loads((output / "lwc/_version.json").read_text())["target_api_version"])
        self.assertEqual("65.0", json.loads((output / "_scrape-meta.json").read_text())["target_api_version"])

    def test_rejects_noncanonical_target_api_version(self):
        with self.assertRaises(ValueError):
            self._assemble("65.1")

    def test_lwc_source_version_metadata_is_not_falsely_receipted_as_copied(self):
        output = self._assemble("65.0")

        receipt = json.loads((output / "lwc/_filter-receipt.json").read_text(encoding="utf-8"))
        self.assertNotIn("_version.json", {item["path"] for item in receipt["copied"]})
        self.assertEqual("latest", receipt["source_version_metadata"]["version"])
        self.assertEqual(
            hashlib.sha256((self.lwc_source / "_version.json").read_bytes()).hexdigest(),
            receipt["source_version_metadata"]["sha256"],
        )

    def test_root_metadata_retains_atlas_source_identity(self):
        output = self._assemble("65.0")

        metadata = json.loads((output / "_scrape-meta.json").read_text(encoding="utf-8"))
        self.assertEqual("258.0", metadata["version"])
        self.assertEqual("API v65.0 (Winter '26')", metadata["atlas_version_label"])
        self.assertEqual(8, metadata["total_pages"])
        self.assertEqual("tools/scrape.py", metadata["tool"])
        self.assertEqual("atlas-tool-hash", metadata["tool_sha256"])
        self.assertEqual("258.0", metadata["family_versions"]["apex"])
        self.assertEqual("258.0-source-receipt", metadata["family_versions"]["visualforce"])
        assembly = metadata["assembly"]
        self.assertEqual(str(Path(assembler.__file__).resolve()), assembly["assembler"]["path"])
        self.assertEqual(
            hashlib.sha256(Path(assembler.__file__).read_bytes()).hexdigest(), assembly["assembler"]["sha256"]
        )
        self.assertEqual(assembler.tree_sha256(self.source / "apex"), assembly["families"]["apex"]["sha256"])
        self.assertEqual(2, assembly["families"]["apex"]["file_count"])
        self.assertEqual("latest", metadata["family_versions"]["lwc"]["source_version"])

    def test_rejects_atlas_family_version_that_does_not_match_root(self):
        (self.source / "apex/_version.json").write_text(
            json.dumps({"version": "257.0", "pages": {"empty": 0, "failed": 0}}), encoding="utf-8"
        )

        with self.assertRaises(ValueError):
            self._assemble("65.0")

    def test_rejects_atlas_root_version_not_mapped_to_target_api(self):
        with self.assertRaises(ValueError):
            self._assemble("66.0")

    def test_accepts_latest_atlas_only_when_label_resolves_target(self):
        self._set_atlas_source_version("latest", "65.0")

        self._assemble("65.0")


if __name__ == "__main__":
    unittest.main()
