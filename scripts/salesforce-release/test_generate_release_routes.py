import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import generate_release_routes as routes


ROOT = "release-notes.salesforce_release_notes.htm"
APEX = "release-notes.rn_apex.htm"
FEATURE = "release-notes.rn_apex_feature.htm"


class GenerateReleaseRoutesTests(unittest.TestCase):
    def write_json(self, directory, name, value):
        path = Path(directory, name)
        path.write_text(json.dumps(value), encoding="utf-8")
        return path

    def inputs(self, directory, *, inventory=None, toc=None, policy=None):
        inventory = inventory or {
            "schemaVersion": 1,
            "totalFiles": 3,
            "totalMembers": 0,
            "namespaces": [],
            "documents": [
                {"sourcePath": "salesforce_release_notes.md"},
                {"sourcePath": "rn_apex.md"},
                {"sourcePath": "rn_apex_feature.md"},
            ],
        }
        toc = toc or {
            "schemaVersion": 1,
            "entries": [
                {"topicId": ROOT, "title": "Summer", "ancestorTopicIds": []},
                {"topicId": APEX, "title": "Apex", "ancestorTopicIds": [ROOT]},
                {"topicId": FEATURE, "title": "Feature", "ancestorTopicIds": [ROOT, APEX]},
            ],
        }
        policy = policy or {
            "schemaVersion": 1,
            "previousRelease": "260.0.0",
            "currentRelease": "262.0.0",
            "inventoryDigest": "abc123",
            "branchDefaults": {
                "__root__": {"outOfScopeReason": "Release-note navigation only."},
                APEX: {"requireExplicit": True},
            },
            "routeOverrides": [{"sourcePath": "rn_apex_feature.md", "behaviorIds": ["behavior.apex.feature"]}],
        }
        return (
            self.write_json(directory, "inventory.json", inventory),
            self.write_json(directory, "_toc.json", toc),
            self.write_json(directory, "policy.json", policy),
        )

    def generate(self, directory, **kwargs):
        inventory, toc, policy = self.inputs(directory, **kwargs)
        return routes.generate(inventory, toc, policy)

    def test_generates_sorted_exact_routes_from_topic_filenames(self):
        with tempfile.TemporaryDirectory() as directory:
            actual = self.generate(directory)

        self.assertEqual(actual, {
            "schemaVersion": 1,
            "previousRelease": "260.0.0",
            "currentRelease": "262.0.0",
            "inventoryDigest": "abc123",
            "routes": [
                {"sourcePath": "rn_apex.md", "outOfScopeReason": "Release-note navigation only."},
                {"sourcePath": "rn_apex_feature.md", "behaviorIds": ["behavior.apex.feature"]},
                {"sourcePath": "salesforce_release_notes.md", "outOfScopeReason": "Release-note navigation only."},
            ],
        })

    def test_rejects_stale_override_path(self):
        with tempfile.TemporaryDirectory() as directory:
            inventory, toc, policy = self.inputs(directory)
            data = json.loads(policy.read_text())
            data["routeOverrides"] = [{"sourcePath": "stale.md", "outOfScopeReason": "Stale."}]
            policy.write_text(json.dumps(data))
            with self.assertRaisesRegex(ValueError, "stale"):
                routes.generate(inventory, toc, policy)

    def test_rejects_duplicate_override_path(self):
        with tempfile.TemporaryDirectory() as directory:
            inventory, toc, policy = self.inputs(directory)
            data = json.loads(policy.read_text())
            data["routeOverrides"] *= 2
            policy.write_text(json.dumps(data))
            with self.assertRaisesRegex(ValueError, "duplicate"):
                routes.generate(inventory, toc, policy)

    def test_rejects_missing_inventory_path(self):
        with tempfile.TemporaryDirectory() as directory:
            inventory = {
                "schemaVersion": 1, "totalFiles": 2, "totalMembers": 0, "namespaces": [],
                "documents": [{"sourcePath": "salesforce_release_notes.md"}, {"sourcePath": "rn_apex.md"}],
            }
            with self.assertRaisesRegex(ValueError, "TOC and inventory"):
                self.generate(directory, inventory=inventory)

    def test_rejects_toc_inventory_mismatch(self):
        with tempfile.TemporaryDirectory() as directory:
            inventory = {
                "schemaVersion": 1, "totalFiles": 3, "totalMembers": 0, "namespaces": [],
                "documents": [
                    {"sourcePath": "salesforce_release_notes.md"},
                    {"sourcePath": "rn_apex.md"},
                    {"sourcePath": "other.md"},
                ],
            }
            with self.assertRaisesRegex(ValueError, "TOC and inventory"):
                self.generate(directory, inventory=inventory)

    def test_rejects_missing_branch_policy(self):
        with tempfile.TemporaryDirectory() as directory:
            policy = {
                "schemaVersion": 1,
                "previousRelease": "260.0.0",
                "currentRelease": "262.0.0",
                "inventoryDigest": "abc123",
                "branchDefaults": {"__root__": {"outOfScopeReason": "Navigation."}},
                "routeOverrides": [{"sourcePath": "rn_apex_feature.md", "behaviorIds": ["behavior.apex.feature"]}],
            }
            with self.assertRaisesRegex(ValueError, "branchDefaults keys"):
                self.generate(directory, policy=policy)

    def test_require_explicit_branch_rejects_missing_override(self):
        with tempfile.TemporaryDirectory() as directory:
            policy = {
                "schemaVersion": 1,
                "previousRelease": "260.0.0",
                "currentRelease": "262.0.0",
                "inventoryDigest": "abc123",
                "branchDefaults": {
                    "__root__": {"outOfScopeReason": "Navigation."},
                    APEX: {"requireExplicit": True},
                },
                "routeOverrides": [],
            }
            with self.assertRaisesRegex(ValueError, "requireExplicit"):
                self.generate(directory, policy=policy)

    def test_rejects_unknown_policy_key(self):
        with tempfile.TemporaryDirectory() as directory:
            inventory, toc, policy = self.inputs(directory)
            data = json.loads(policy.read_text())
            data["unexpected"] = True
            policy.write_text(json.dumps(data))
            with self.assertRaisesRegex(ValueError, "unknown unexpected"):
                routes.generate(inventory, toc, policy)

    def test_rejects_extra_branch_policy(self):
        with tempfile.TemporaryDirectory() as directory:
            inventory, toc, policy = self.inputs(directory)
            data = json.loads(policy.read_text())
            data["branchDefaults"]["release-notes.rn_retired.htm"] = {"outOfScopeReason": "Stale."}
            policy.write_text(json.dumps(data))
            with self.assertRaisesRegex(ValueError, "branchDefaults keys"):
                routes.generate(inventory, toc, policy)

    def test_cli_replaces_destination_only_after_writing_temp_file(self):
        with tempfile.TemporaryDirectory() as directory:
            inventory, toc, policy = self.inputs(directory)
            output = Path(directory, "routes.json")
            output.write_text("old\n", encoding="utf-8")
            original_replace = os.replace

            def replace(source, destination):
                self.assertEqual(output.read_text(encoding="utf-8"), "old\n")
                self.assertEqual(Path(source).parent, output.parent)
                return original_replace(source, destination)

            with patch("generate_release_routes.os.replace", side_effect=replace) as mocked_replace:
                with patch.object(sys, "argv", [
                    "generate_release_routes.py", "--inventory", str(inventory), "--toc", str(toc),
                    "--policy", str(policy), "--output", str(output),
                ]):
                    routes.main()

            self.assertEqual(mocked_replace.call_count, 1)
            self.assertEqual(json.loads(output.read_text(encoding="utf-8"))["schemaVersion"], 1)


if __name__ == "__main__":
    unittest.main()
