import json
import os
import subprocess
import sys
import tempfile
import textwrap
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts" / "corpus-assurance" / "worker-health.py"


class WorkerHealthTest(unittest.TestCase):
    def test_public_orchestration_docs_use_neutral_hosts(self) -> None:
        paths = (
            ROOT / "docs" / "SALESFORCE_ADOPTION_WORKFLOW.md",
            ROOT / "docs" / "superpowers" / "plans" / "2026-08-20-salesforce-surface-proof-completion.md",
        )
        for path in paths:
            text = path.read_text(encoding="utf-8")
            for private_shape in ("/Users/", "/Volumes/", "smb://", "@localhost", ".local"):
                self.assertNotIn(private_shape, text, f"{path} contains {private_shape}")

    def test_normalizes_every_worker_and_drops_secrets(self) -> None:
        with tempfile.TemporaryDirectory() as tmp_name:
            tmp = Path(tmp_name)
            fake_bin = tmp / "bin"
            fake_bin.mkdir()
            ssh_log = tmp / "ssh.log"
            fake_ssh = fake_bin / "ssh"
            fake_ssh.write_text(
                textwrap.dedent(
                    """\
                    #!/usr/bin/env python3
                    import json, os, sys

                    with open(os.environ["SSH_LOG"], "a", encoding="utf-8") as stream:
                        stream.write(" ".join(sys.argv[1:]) + "\\n")
                    target = next(arg for arg in sys.argv[1:] if "@" in arg)
                    name = target.split("@", 1)[1]
                    if name == "unreachable":
                        print("force://DO_NOT_COPY", file=sys.stderr)
                        raise SystemExit(255)
                    if name == "malformed":
                        print("not-json")
                        raise SystemExit(0)

                    org_id = "00D000000000001"
                    connected = True
                    disk = 100 * 1024**3
                    heartbeat = "2099-01-01T00:00:00Z"
                    run = {"id": "surface-wave-01-shard-0", "phase": "salesforce-run", "heartbeatAt": heartbeat}
                    limits = {
                        "ActiveScratchOrgs": {"remaining": 3},
                        "DailyScratchOrgs": {"remaining": 6},
                    }
                    if name == "wrong-org":
                        org_id = "00D000000000999"
                    elif name == "missing-alias":
                        connected = False
                        org_id = None
                    elif name == "low-disk":
                        disk = 1024
                    elif name == "stale":
                        run["heartbeatAt"] = "2020-01-01T00:00:00Z"
                    elif name == "idle":
                        run = None
                    elif name == "missing-limits":
                        limits = {}

                    print(json.dumps({
                        "devHub": {
                            "status": 0 if connected else 2,
                            "orgId": org_id,
                            "username": "worker@example.invalid" if connected else None,
                            "connectedStatus": "Connected" if connected else "Missing",
                            "limits": limits,
                            "accessToken": "DO_NOT_COPY",
                            "sfdxAuthUrl": "force://DO_NOT_COPY",
                        },
                        "diskFreeBytes": disk,
                        "run": run,
                        "environment": {"SECRET": "DO_NOT_COPY"},
                        "cookie": "DO_NOT_COPY",
                    }))
                    """
                ),
                encoding="utf-8",
            )
            fake_ssh.chmod(0o755)
            output = tmp / "health.json"
            env = os.environ.copy()
            env["PATH"] = f"{fake_bin}{os.pathsep}{env['PATH']}"
            env["SSH_LOG"] = str(ssh_log)
            hosts = [
                "healthy",
                "idle",
                "missing-limits",
                "wrong-org",
                "missing-alias",
                "low-disk",
                "stale",
                "malformed",
                "unreachable",
            ]
            command = [sys.executable, str(SCRIPT)]
            for name in hosts:
                command.extend(("--host", f"{name}=ssh-user@{name}"))
            command.extend(
                (
                    "--disk",
                    "healthy=/proof-data",
                    "--alias",
                    "glade-dev-hub",
                    "--expected-org-id",
                    "00D000000000001",
                    "--min-disk-free-bytes",
                    str(20 * 1024**3),
                    "--stale-after-seconds",
                    "120",
                    "--output",
                    str(output),
                )
            )

            result = subprocess.run(command, env=env, text=True, capture_output=True, check=False)
            self.assertEqual(result.returncode, 0, result.stderr)
            document = json.loads(output.read_text(encoding="utf-8"))
            self.assertEqual([row["name"] for row in document["workers"]], hosts)

            rows = {row["name"]: row for row in document["workers"]}
            healthy = rows["healthy"]
            self.assertTrue(healthy["healthy"])
            self.assertTrue(healthy["reachable"])
            self.assertEqual(healthy["host"], "ssh-user@healthy")
            self.assertEqual(
                healthy["devHub"],
                {
                    "connected": True,
                    "alias": "glade-dev-hub",
                    "orgId": "00D000000000001",
                    "username": "worker@example.invalid",
                    "activeScratchOrgsRemaining": 3,
                    "dailyScratchOrgsRemaining": 6,
                },
            )
            self.assertEqual(healthy["diskFreeBytes"], 100 * 1024**3)
            self.assertEqual(healthy["run"]["phase"], "salesforce-run")
            self.assertEqual(healthy["issues"], [])

            self.assertTrue(rows["idle"]["healthy"])
            self.assertIsNone(rows["idle"]["run"])
            self.assertIn("scratch-org-limits-unavailable", rows["missing-limits"]["issues"])
            self.assertIn("dev-hub-org-id-mismatch", rows["wrong-org"]["issues"])
            self.assertIn("dev-hub-unavailable", rows["missing-alias"]["issues"])
            self.assertIn("low-disk", rows["low-disk"]["issues"])
            self.assertIn("stale-heartbeat", rows["stale"]["issues"])
            self.assertIn("malformed-worker-response", rows["malformed"]["issues"])
            self.assertIn("unreachable", rows["unreachable"]["issues"])
            self.assertFalse(rows["unreachable"]["reachable"])

            serialized = output.read_text(encoding="utf-8") + result.stdout + result.stderr
            for forbidden in ("DO_NOT_COPY", "force://", "accessToken", "sfdxAuthUrl", "cookie", "environment"):
                self.assertNotIn(forbidden, serialized)
            logged_ssh = ssh_log.read_text(encoding="utf-8")
            self.assertIn("/proof-data", logged_ssh)
            self.assertIn("/usr/local/bin/sf", logged_ssh)


if __name__ == "__main__":
    unittest.main()
