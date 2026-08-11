import importlib.util
import json
import shutil
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("salesforce-first-filter.py")
SPEC = importlib.util.spec_from_file_location("salesforce_first_filter", SCRIPT)
FILTER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(FILTER)


class SalesforceFirstFilterTest(unittest.TestCase):
    def test_project_manifest_uses_posix_string_order(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            folder = root / "force-app/main/default/email/Glade_Messaging"
            folder.mkdir(parents=True)
            (folder / "Trail_Template.email").write_text("body")
            (root / "force-app/main/default/email/Glade_Messaging.emailFolder-meta.xml").write_text("folder")

            manifest = FILTER.project_file_manifest(root)

            self.assertEqual(
                [entry["path"] for entry in manifest],
                [
                    "force-app/main/default/email/Glade_Messaging.emailFolder-meta.xml",
                    "force-app/main/default/email/Glade_Messaging/Trail_Template.email",
                ],
            )

    def test_fixture_source_survives_cleanup(self):
        with tempfile.TemporaryDirectory() as temp:
            out = Path(temp) / "out"
            cleaned = []
            cleanup_source_present = []

            def run_sf_stream(_bin, _args, _cwd, _timeout, stdout, stderr):
                stdout.write(json.dumps({"status": 0, "result": {"status": "Succeeded", "details": {"componentSuccesses": [{"fileName": "classes/Probe.cls"}], "componentFailures": []}}}))
                stdout.close()
                stderr.close()
                return SimpleNamespace(returncode=0, executable_sha256="a", executable_after_sha256="a")

            def destructive_cleanup(_bin, project, _org, _metadata, _protected):
                cleaned.append(project)
                cleanup_source_present.append((project / "force-app").is_dir())
                shutil.rmtree(project / "force-app", ignore_errors=True)
                return {"cleanupExitCode": 0, "residueAbsent": True}

            fixture = {"name": "fixture.json", "command": {"kind": "check"}, "source": [{"path": "force-app/main/default/classes/Probe.cls", "content": "public class Probe {}"}]}
            with patch.object(FILTER, "run_sf_stream", run_sf_stream), patch.object(FILTER, "org_cleanup", destructive_cleanup):
                FILTER.run_one((1, "fixture.json", fixture, ["apex:Probe"]), out, "assurance-sf0", "/usr/local/bin/sf")

            project = out / "projects" / "fixture"
            self.assertTrue((project / "force-app").is_dir())
            self.assertNotEqual(cleaned, [project])
            self.assertEqual(cleanup_source_present, [True])


if __name__ == "__main__":
    unittest.main()
