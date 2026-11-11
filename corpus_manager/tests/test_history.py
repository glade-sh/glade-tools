import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from corpus_manager.history import parse_history, parse_history_data


class HistoryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def write(self, name, document):
        path = self.root / name
        path.write_text(json.dumps(document), encoding="utf-8")
        return path

    def test_gate_rows_preserve_partial_and_unknown_repo_revision(self):
        path = self.root / "results.jsonl"
        rows = [
            {"repo": "synthetic-example", "phase": "check", "exit": 0, "status": "passed", "summary": {}},
            {"repo": "synthetic-example", "phase": "test", "exit": 0, "status": "passed", "part": "s0of2", "shard": "0/2",
             "summary": {"total": 4, "passed": 4, "failed": 0, "skipped": 0}},
        ]
        path.write_text("\n".join(json.dumps(row) for row in rows) + '\n{"repo":', encoding="utf-8")
        imported = parse_history(path, glade_sha="a" * 40, host="runner")
        self.assertEqual(len(imported["runs"]), 2)
        partial = imported["runs"][1]
        self.assertEqual(partial["status"], "partial")
        self.assertEqual(partial["result_status"], "passed")
        self.assertEqual(partial["passed"], 4)
        self.assertIsNone(partial["repo_sha"])
        self.assertEqual(partial["glade_sha"], "a" * 40)
        self.assertTrue(any("malformed" in warning for warning in imported["warnings"]))

    def test_legacy_baseline_and_document_metadata(self):
        path = self.write("baseline.json", {
            "generatedAt": "2026-11-09T00:00:00Z", "glade": {"version": "0.1.0", "sha": "b" * 40},
            "projects": [{"project": "synthetic-label", "ready": False, "summary": {"total": 5, "pass": 2, "fail": 1, "compileError": 2}}],
        })
        row = parse_history(path)["runs"][0]
        self.assertEqual((row["passed"], row["failed"], row["compile_errors"]), (2, 1, 2))
        self.assertEqual(row["status"], "failed")
        self.assertEqual(row["started_at"], "2026-11-09T00:00:00Z")
        self.assertEqual(row["glade_version"], "0.1.0")
        self.assertEqual(row["glade_sha"], "b" * 40)

    def test_exported_runs_keep_recorded_revision_and_api(self):
        path = self.write("export.json", {"runs": [{
            "repo_url": "https://github.com/synthetic/example", "repo_sha": "c" * 40, "glade_sha": "d" * 40,
            "glade_version": "0.2.0", "api_version": 67.0, "status": "passed", "passed": 9, "failed": 0,
            "skipped": 1, "total": 10, "host": "test-host", "started_at": "2026-11-08T12:00:00Z",
        }]})
        row = parse_history(path, glade_sha="e" * 40)["runs"][0]
        self.assertEqual(row["glade_sha"], "d" * 40)
        self.assertEqual(row["repo_sha"], "c" * 40)
        self.assertEqual(row["api_version"], "67.0")
        self.assertEqual(row["host"], "test-host")
        self.assertNotIn("unknown", row["provenance"])

    def test_dispatch_is_not_a_run_and_generic_sha_is_not_glade_sha(self):
        path = self.write("dispatch.json", {"label": "SYNTHETIC", "sha": "a" * 40, "outputs": {}})
        self.assertEqual(parse_history(path)["runs"], [])
        path = self.write("summary.json", {"repo": "synthetic", "sha": "a" * 40, "status": "passed"})
        self.assertIsNone(parse_history(path)["runs"][0]["glade_sha"])

    def test_timeouts_are_not_passes_or_failures(self):
        path = self.write("timeout.json", {"repo": "synthetic", "timedOut": True, "status": "passed", "summary": {"timeout": 1}})
        row = parse_history(path)["runs"][0]
        self.assertEqual(row["status"], "timeout")
        self.assertIsNone(row["failed"])
        self.assertIsNone(row["passed"])

    def test_refuses_logs_and_oversized_or_invalid_files(self):
        path = self.write("test.stdout", {"repo": "synthetic", "status": "passed"})
        self.assertEqual(parse_history(path)["runs"], [])
        self.assertTrue(parse_history(path)["warnings"])
        path = self.write("large.json", {"repo": "synthetic", "status": "passed"})
        with patch("corpus_manager.history.MAX_HISTORY_BYTES", 10):
            self.assertEqual(parse_history(path)["runs"], [])
        path.write_bytes(b"\xff")
        self.assertTrue(parse_history(path)["warnings"])

    def test_count_validation_and_rows_without_evidence(self):
        path = self.write("summary.json", [{"repo": "synthetic", "status": "unknown", "passed": True, "failed": -1, "total": 4.5},
                                          {"repo": "synthetic-registry-only", "repo_sha": "f" * 40}])
        imported = parse_history(path)
        self.assertEqual(len(imported["runs"]), 1)
        self.assertIsNone(imported["runs"][0]["passed"])
        self.assertIsNone(imported["runs"][0]["failed"])
        self.assertIsNone(imported["runs"][0]["total"])

    def test_remote_aggregate_text_accepts_dispatch_timestamp(self):
        content = json.dumps({"repo": "synthetic", "status": "passed", "summary": {"passed": 5}}).encode("utf-8")
        row = parse_history_data(content, source="runner:aggregate", glade_sha="a" * 40,
                                 host="runner", started_at="2026-11-09T00:00:00Z")["runs"][0]
        self.assertEqual(row["started_at"], "2026-11-09T00:00:00Z")
        self.assertEqual(row["source"], "runner:aggregate:1")
        self.assertEqual(row["glade_sha"], "a" * 40)


if __name__ == "__main__":
    unittest.main()
