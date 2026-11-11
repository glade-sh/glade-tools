import json
import tempfile
import unittest
from copy import deepcopy
from pathlib import Path
from unittest.mock import patch

from corpus_manager import sources
from corpus_manager.sources import load_routes

ROUTES = {"route_a": ("private", "host-a"), "route_b": ("public", "host-b"),
          "route_c": ("broad3", "host-b"), "route_d": ("split", "host-b")}


def refresh_sources(config):
    return sources.refresh_sources(config, ROUTES)


class SourcesTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)

    def dispatch(self, name="synthetic", **changes):
        document = {
            "label": "SYNTHETIC", "sha": "a" * 40, "dispatched": "2026-11-09T00:00:00Z",
            "gates": {"private": True, "public": False, "broad3": False, "split": True},
            "outputs": {"route_a": "/synthetic/private-run", "route_b": "~/synthetic/public-run",
                        "route_c": "~/synthetic/broad-run", "route_d": "~/synthetic/{one,two}-run"},
            **changes,
        }
        path = self.directory / (name + ".json")
        path.write_text(json.dumps(document))
        return path

    def test_enabled_routes_and_brace_expansion_use_explicit_metadata(self):
        self.dispatch()
        result = refresh_sources({"dispatch_dir": str(self.directory), "sources": []})
        self.assertEqual(len(result["sources"]), 3)
        self.assertEqual({row["host"] for row in result["sources"]}, {"host-a", "host-b"})
        self.assertEqual({row["path"] for row in result["sources"]}, {
            "/synthetic/private-run/results.jsonl", "~/synthetic/one-run/results.jsonl", "~/synthetic/two-run/results.jsonl"})
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
        manual = {"host": "host-a", "path": "/synthetic/private-run/results.jsonl", "glade_sha": "a" * 40,
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

    def test_routes_load_from_env_path_and_missing_file_yields_none(self):
        path = self.directory / "routes.json"
        path.write_text(json.dumps({"routes": {"k": {"gate": "g", "host": "host-a"}, "bad": {"gate": 1}}}))
        with patch.dict("os.environ", {sources.ROUTES_ENV: str(path)}):
            self.assertEqual(load_routes(), ({"k": ("g", "host-a")}, None))
        self.dispatch()
        with patch.dict("os.environ", {sources.ROUTES_ENV: str(self.directory / "absent.json")}):
            result = sources.refresh_sources({"dispatch_dir": str(self.directory), "sources": []})
        self.assertEqual(result["sources"], [])
        self.assertTrue(result["warnings"])


if __name__ == "__main__":
    unittest.main()
