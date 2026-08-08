#!/usr/bin/env python3
import json
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent
VALIDATOR = ROOT / "validate-dual-rail-manifest.py"

def run_validator(payload):
    with tempfile.TemporaryDirectory() as tmp:
        path = Path(tmp) / "manifest.json"
        path.write_text(json.dumps(payload))
        return subprocess.run(["python3", str(VALIDATOR), str(path)], capture_output=True, text=True)

class DualRailManifestTest(unittest.TestCase):
    def test_accepts_complete_pass_manifest(self):
        result = run_validator({"status": "pass", "surfaceIds": ["apex:System.List.toString()"], "candidate": {"commit": "a" * 40, "binarySha256": "b" * 64}, "observations": {"localResultSha256": "c" * 64, "salesforceResultSha256": "d" * 64, "comparison": "pass"}})
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_rejects_missing_salesforce_observation(self):
        result = run_validator({"status": "pass", "surfaceIds": ["apex:System.List.toString()"], "candidate": {"commit": "a" * 40, "binarySha256": "b" * 64}, "observations": {"localResultSha256": "c" * 64, "comparison": "pass"}})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("salesforceResultSha256", result.stderr)

    def test_rejects_non_pass_comparison(self):
        result = run_validator({"status": "pass", "surfaceIds": ["apex:System.List.toString()"], "candidate": {"commit": "a" * 40, "binarySha256": "b" * 64}, "observations": {"localResultSha256": "c" * 64, "salesforceResultSha256": "d" * 64, "comparison": "mismatch"}})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("comparison", result.stderr)

if __name__ == "__main__":
    unittest.main()
