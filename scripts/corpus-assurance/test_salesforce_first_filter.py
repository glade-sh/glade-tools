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
    def test_semantic_failure_is_still_complete_evidence(self):
        semantic = {
            "status": "Failed",
            "exitCode": 1,
            "kind": "exec",
            "surfaceIds": ["apex:Missing.run()"],
            "runtimeResult": {"status": 1, "result": {"success": False, "compiled": False, "compileProblem": "Invalid type: Missing"}},
        }
        self.assertFalse(FILTER.result_failed(semantic))
        typed = {**semantic, "runtimeResult": {"status": 1, "name": "executeCompileFailure", "message": "Compilation failed at Line 1 column 1 with the error:\n\nInvalid type: Missing"}}
        self.assertFalse(FILTER.result_failed(typed))
        self.assertTrue(FILTER.result_failed({**semantic, "surfaceIds": ["apex:Missing.a()", "apex:Missing.b()"]}))
        self.assertTrue(FILTER.result_failed({"status": "Failed", "exitCode": 1, "kind": "exec", "surfaceIds": ["apex:Missing.run()"], "runtimeResult": {"status": 1, "message": "request failed"}}))
        self.assertTrue(FILTER.result_failed({"status": "Succeeded", "exitCode": 0, "kind": "test", "runtimeRequested": True, "runtimePassed": False}))

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
            sf_bin = Path(temp) / "sf"
            sf_bin.write_text("#!/bin/sh\n")
            sf_bin.chmod(0o700)
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
                FILTER.run_one((1, "fixture.json", fixture, ["apex:Probe"]), out, "assurance-sf0", str(sf_bin))

            project = out / "projects" / "fixture"
            self.assertTrue((project / "force-app").is_dir())
            self.assertNotEqual(cleaned, [project])
            self.assertEqual(cleanup_source_present, [True])


if __name__ == "__main__":
    unittest.main()
