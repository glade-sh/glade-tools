"""Verifier request generation must not create or relabel observations."""

import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
import generate_verifier_inputs as generator


class VerifierInputsTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.contract = self.root / "contract.json"
        self.catalog = self.root / "catalog.json"
        self.runtime = self.root / "runtime.json"
        self.project = self.root / "project"
        self.project.mkdir()
        self.write(self.contract, {"releases": [{"apiVersion": "68.0"}], "windows": {"source": [{"version": "68.0"}]}, "behaviors": [{"id": "apex.crypto.cbc-encrypt.declaration"}]})
        self.write(self.catalog, {"gladeCommit": "a" * 40, "rules": [{"id": "old", "source": "public class Old {}"}]})
        self.write(self.runtime, {"apiVersion": "67.0", "comparisons": [{"status": "inconclusive", "sfObservation": "", "gladeObservation": ""}]})
        self.write(self.project / "sfdx-project.json", {"sourceApiVersion": "67.0"})
        self.metadata = self.project / "force-app/main/default/classes/Probe.cls-meta.xml"
        self.metadata.parent.mkdir(parents=True)
        self.metadata.write_text("<ApexClass><apiVersion>67.0</apiVersion></ApexClass>\n")
        self.review = self.root / "salesforce-release-review-summer-26-to-winter-27.json"
        self.write(self.review, {"decisions": [{"surfaceId": generator.SURFACE_ID, "scope": "t2", "disposition": "existing-case", "sourceURL": "https://developer.salesforce.com/docs/atlas.en-us.264.0.apexref.meta/apexref/apex_methods_system_crypto.htm", "declaration": "public static Blob encrypt(String algorithmName, Blob key, Blob iv, Blob value)", "productTests": [generator.TEST]}]})

    @staticmethod
    def write(path, value):
        path.write_text(json.dumps(value, indent=2) + "\n")

    def plan(self):
        return generator.plan(self.contract, self.catalog, self.runtime, self.project)

    def test_targets_and_public_case_are_generated_without_proof_credit(self):
        old = self.catalog.read_text()
        updates = self.plan()
        self.assertTrue(updates[self.catalog].startswith(old[: old.rindex("\n  ]")]))
        catalog = json.loads(updates[self.catalog])
        self.assertEqual(catalog["gladeCommit"], "a" * 40)
        self.assertEqual(catalog["rules"][-1]["apiVersion"], 68)
        self.assertEqual(catalog["rules"][-1]["docsPath"], json.loads(self.review.read_text())["decisions"][0]["sourceURL"])
        self.assertEqual(json.loads(updates[self.runtime])["comparisons"], json.loads(self.runtime.read_text())["comparisons"])
        self.assertEqual(json.loads(updates[self.runtime])["apiVersion"], "68.0")
        self.assertIn("<apiVersion>68.0</apiVersion>", updates[self.metadata])
        for path, text in updates.items():
            path.write_text(text)
        self.assertEqual(self.plan(), updates)

    def test_refuses_to_relabel_observed_data(self):
        value = json.loads(self.runtime.read_text())
        value["comparisons"][0]["sfObservation"] = "observed"
        self.write(self.runtime, value)
        with self.assertRaisesRegex(ValueError, "observations"):
            self.plan()
        self.assertEqual(json.loads(self.runtime.read_text())["apiVersion"], "67.0")

    def test_refuses_unsupported_or_nonpublic_declaration(self):
        for key, value in [("scope", "outside-claim"), ("sourceURL", "https://example.com/264.0"), ("productTests", [])]:
            original = json.loads(self.review.read_text())
            changed = json.loads(self.review.read_text())
            changed["decisions"][0][key] = value
            self.write(self.review, changed)
            with self.assertRaisesRegex(ValueError, "test-bound public"):
                self.plan()
            self.write(self.review, original)


if __name__ == "__main__":
    unittest.main()
