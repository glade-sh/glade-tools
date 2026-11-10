import json
import tempfile
import unittest
from copy import deepcopy
from pathlib import Path
from unittest.mock import patch

from corpus_manager.sources import refresh_sources


class SourcesTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)

    def dispatch(self, name="synthetic", **changes):
        document = {
            "label": "SYNTHETIC", "sha": "a" * 40, "dispatched": "2026-11-09T00:00:00Z",
            "gates": {"private": True, "public": False, "broad3": False, "bmo": True},
            "outputs": {"casper_priv": "/synthetic/private-run", "razor_public": "~/synthetic/public-run",
                        "razor_b3": "~/synthetic/broad-run", "bmo": "~/synthetic/{BUCKET2,BROAD}-run"},
            **changes,
        }
        path = self.directory / (name + ".json")
        path.write_text(json.dumps(document))
        return path

    def test_enabled_routes_and_bmo_expansion_use_explicit_metadata(self):
        self.dispatch()
        result = refresh_sources({"dispatch_dir": str(self.directory), "sources": []})
        self.assertEqual(len(result["sources"]), 3)
        self.assertEqual({row["host"] for row in result["sources"]}, {"casper", "bmo"})
        self.assertEqual({row["path"] for row in result["sources"]}, {
            "/synthetic/private-run/results.jsonl", "~/synthetic/BUCKET2-run/results.jsonl", "~/synthetic/BROAD-run/results.jsonl"})
        for row in result["sources"]:
            self.assertEqual(row["glade_sha"], "a" * 40)
            self.assertEqual(row["started_at"], "2026-11-09T00:00:00Z")
        self.assertEqual(result["warnings"], [])

    def test_new_dispatch_is_found_on_next_refresh_without_config_write(self):
        self.dispatch()
        config = {"dispatch_dir": str(self.directory), "sources": [], "schemaVersion": 1}
        first = refresh_sources(config)
        self.dispatch("new", sha="b" * 40, dispatched="2026-11-10T00:00:00Z")
        second = refresh_sources(first)
        self.assertEqual(len(second["sources"]), 6)
        self.assertEqual(second["sources"][0]["glade_sha"], "b" * 40)
        self.assertEqual(config["sources"], [])
        self.assertEqual(second["schemaVersion"], 1)

    def test_manual_entries_survive_and_duplicate_keys_do_not_multiply(self):
        self.dispatch()
        manual = {"host": "casper", "path": "/synthetic/private-run/results.jsonl", "glade_sha": "a" * 40,
                  "started_at": "2026-11-09T00:00:00Z", "provenance": "Manual source", "note": "keep this"}
        extra = {"host": "custom", "path": "/synthetic/extra/results.jsonl", "glade_sha": "c" * 40,
                 "started_at": "2026-11-08T00:00:00Z"}
        config = {"dispatch_dir": str(self.directory), "sources": [manual, extra, deepcopy(manual)]}
        expected = deepcopy(config)
        first = refresh_sources(config)
        second = refresh_sources(first)
        self.assertEqual(len(second["sources"]), 4)
        self.assertIn(manual, second["sources"])
        self.assertIn(extra, second["sources"])
        self.assertEqual(config, expected)

    def test_missing_dispatch_directory_retains_existing_routes_with_warning(self):
        existing = [{"host": "runner", "path": "~/synthetic/results.jsonl", "glade_sha": "d" * 40}]
        result = refresh_sources({"dispatch_dir": str(self.directory / "missing"), "sources": existing})
        self.assertEqual(result["sources"], existing)
        self.assertTrue(result["warnings"])

    def test_no_inferred_sha_or_date_from_invalid_dispatch(self):
        self.dispatch("missing-sha", sha=None)
        self.dispatch("short-sha", sha="abcdef")
        self.dispatch("missing-date", dispatched=None)
        self.dispatch("invalid-date", dispatched="later")
        result = refresh_sources({"dispatch_dir": str(self.directory), "sources": []})
        self.assertEqual(result["sources"], [])
        self.assertIn("4", result["warnings"][0])

    def test_oversized_or_malformed_records_do_not_remove_prior_sources(self):
        self.dispatch()
        existing = [{"host": "runner", "path": "~/synthetic/results.jsonl", "glade_sha": "d" * 40,
                     "provenance": "Local dispatch record old.json"}]
        (self.directory / "malformed.json").write_text("{")
        with patch("corpus_manager.sources.MAX_DISPATCH_BYTES", 10):
            result = refresh_sources({"dispatch_dir": str(self.directory), "sources": existing})
        self.assertEqual(result["sources"], existing)
        self.assertTrue(result["warnings"])


if __name__ == "__main__":
    unittest.main()
