import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts" / "render-salesforce-dashboard.py"


class RenderSalesforceDashboardTest(unittest.TestCase):
    def test_renders_complete_escaped_static_status(self) -> None:
        with tempfile.TemporaryDirectory() as tmp_name:
            tmp = Path(tmp_name)
            status = tmp / "STATUS.json"
            output = tmp / "STATUS.html"
            status.write_text(
                json.dumps(
                    {
                        "schemaVersion": 1,
                        "generatedAt": "2026-08-20T20:00:00Z",
                        "programStatus": "NOT DONE",
                        "completion": {"percent": 69.1, "complete": 18427, "required": 26651, "remaining": 8224},
                        "candidate": {"glade": "a" * 40, "tools": "b" * 40},
                        "tiers": {
                            "inventory": {"complete": 10213, "required": 10213},
                            "localEvidence": {"complete": 10210, "required": 10213},
                            "salesforceComparison": {"complete": 0, "required": 8219},
                            "hostedDeferred": 3,
                            "openPacketRows": 0,
                        },
                        "salesforce": {
                            "state": "not-started",
                            "outcomes": {
                                "adjudicated": 0,
                                "matched": 0,
                                "explicitNonParity": 0,
                                "productMismatch": 0,
                                "inconclusive": 0,
                                "open": 8219,
                            },
                        },
                        "pipeline": {"phase": "local-closeout", "status": "running", "startedAt": None, "updatedAt": None},
                        "machines": [
                            {
                                "name": "<worker>",
                                "healthy": True,
                                "reachable": True,
                                "devHub": {
                                    "connected": True,
                                    "alias": "glade-dev-hub",
                                    "activeScratchOrgsRemaining": 3,
                                    "dailyScratchOrgsRemaining": 6,
                                },
                                "diskFreeBytes": 107374182400,
                                "run": None,
                                "issues": [],
                            }
                        ],
                        "action": {
                            "owner": "agent",
                            "summary": "Close local evidence",
                            "reason": "Three <needs> remain.",
                            "action": "Land the AppLauncher packet.",
                            "clearsWhen": "Local evidence is complete.",
                        },
                        "cleanup": {"state": "clean"},
                        "delivery": {"state": "PR #105 merged"},
                    }
                ),
                encoding="utf-8",
            )

            result = subprocess.run(
                [sys.executable, str(SCRIPT), "--status", str(status), "--output", str(output)],
                text=True,
                capture_output=True,
                check=False,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            rendered = output.read_text(encoding="utf-8")
            for expected in (
                '<meta http-equiv="refresh" content="30">',
                "69.1%",
                "18,427 / 26,651",
                "Inventory",
                "Local evidence",
                "Salesforce comparison",
                "local-closeout",
                "Active scratch orgs",
                "Close local evidence",
                "PR #105 merged",
                "clean",
                "aaaaaaaaaaaa",
                "bbbbbbbbbbbb",
                "&lt;worker&gt;",
                "Three &lt;needs&gt; remain.",
            ):
                self.assertIn(expected, rendered)
            self.assertNotIn("<worker>", rendered)
            self.assertNotIn("ssh-user@", rendered)
            self.assertNotIn("<script", rendered.lower())


if __name__ == "__main__":
    unittest.main()
