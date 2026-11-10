import contextlib
import io
import json
import subprocess
import tempfile
import unittest
from copy import deepcopy
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock, patch

from corpus_manager import ops
from corpus_manager.store import Store


class OperationsTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.store = Store(self.temp.name)
        self.addCleanup(self.store.db.close)
        with patch.object(ops, "GitHub") as github:
            self.ops = ops.Operations(self.store)
        self.github = github.return_value

    def host(self, name="runner", **fields):
        return self.store.put("hosts", {"id": name, "label": name, "ssh": name + ".local", "roots": ["/synthetic"],
                                        "managed_root": "/synthetic-managed", **fields})

    def test_github_url_normalization_and_secret_rejection(self):
        for value in ("synthetic/example", "https://github.com/synthetic/example.git", "git@github.com:synthetic/example.git"):
            with self.subTest(value=value):
                self.assertEqual(ops.github_name(value), "synthetic/example")
        for value in ("https://user:synthetic-secret@github.com/synthetic/example", "https://github.com/synthetic/example?token=synthetic-secret",
                      "https://github.com.evil.test/synthetic/example", "synthetic/../example", "synthetic/example;echo injected"):
            with self.subTest(value=value):
                with self.assertRaises(ValueError) as error:
                    ops.github_name(value)
                self.assertNotIn("synthetic-secret", str(error.exception))

    def test_host_null_ssh_is_local_and_unsafe_values_rejected(self):
        host = ops.validate_host({"id": "local", "ssh": None, "managed_root": None, "roots": ["/synthetic"]})
        self.assertEqual(host["ssh"], "")
        self.assertEqual(host["managed_root"], "")
        for fields in ({"ssh": "-oProxyCommand=bad"}, {"ssh": "runner; command"}, {"roots": ["relative/path"]},
                       {"managed_root": "/Volumes/Photos/BACKUP/new"}):
            with self.subTest(fields=fields), self.assertRaises(ValueError):
                ops.validate_host(fields)

    def test_scan_preserves_catalog_choices_and_deduplicates_locations(self):
        self.host("one")
        self.host("two")
        repo = self.ops.add("synthetic/example")
        repo.update(status="archived", notes="Keep for old releases", tags=["regression"], tier="reference")
        self.store.put("repositories", repo)

        def inventory(host, script, data):
            return {"rows": [{"path": "/synthetic/" + host["id"], "name": "example", "head": "a" * 40,
                              "branch": "main", "remote": "git@github.com:synthetic/example.git", "dirty": None}], "errors": []}

        with patch.object(ops, "host_python", side_effect=inventory):
            self.ops.scan()
            self.ops.scan()
        repos = self.store.all("repositories")
        self.assertEqual(len(repos), 1)
        actual = repos[0]
        self.assertEqual(len(actual["locations"]), 2)
        self.assertEqual({location["host"] for location in actual["locations"]}, {"one", "two"})
        self.assertEqual(actual["status"], "archived")
        self.assertEqual(actual["notes"], "Keep for old releases")
        self.assertEqual(actual["tags"], ["regression"])
        self.assertTrue(all(location["dirty"] is None for location in actual["locations"]))

    def test_failed_git_status_never_reports_clean(self):
        directory = Path(self.temp.name) / "synthetic-checkout"
        (directory / ".git").mkdir(parents=True)

        def git_result(command, **kwargs):
            if "rev-parse" in command:
                return subprocess.CompletedProcess(command, 0, "a" * 40 + "\n", "")
            if "remote" in command:
                return subprocess.CompletedProcess(command, 0, "https://github.com/synthetic/example\n", "")
            if "status" in command:
                return subprocess.CompletedProcess(command, 128, "", "synthetic failure")
            return subprocess.CompletedProcess(command, 0, "main\n", "")

        output = io.StringIO()
        with patch("subprocess.run", side_effect=git_result), contextlib.redirect_stdout(output):
            exec(ops.INVENTORY, {"DATA": {"roots": [str(directory)]}})
        result = json.loads(output.getvalue())
        self.assertFalse(any(row.get("dirty") is False for row in result["rows"]))
        self.assertTrue(result["errors"] or any(row.get("dirty") is None for row in result["rows"]))

    def test_discovery_retains_dismissed_decision_after_refresh(self):
        self.store.put("queries", {"id": "search", "name": "Synthetic", "query": "language:Apex"})
        item = {"id": 123, "full_name": "synthetic/candidate", "html_url": "https://github.com/synthetic/candidate",
                "description": "Synthetic fixture", "stargazers_count": 8, "language": "Apex", "pushed_at": "2026-11-09T00:00:00Z",
                "archived": False, "license": {"spdx_id": "MIT"}}
        self.github.get.return_value = {"items": [item], "total_count": 1, "incomplete_results": False}
        self.ops.discover("search")
        candidate = self.store.get("candidates", "123")
        first_seen = candidate["first_seen"]
        candidate["status"] = "dismissed"
        self.store.put("candidates", candidate)
        self.ops.discover("search")
        candidate = self.store.get("candidates", "123")
        self.assertEqual(candidate["status"], "dismissed")
        self.assertEqual(candidate["first_seen"], first_seen)
        self.assertEqual(len(self.store.all("candidates")), 1)

    def test_snapshot_refuses_local_payload_copy(self):
        self.host("local", ssh="")
        repo = self.ops.add("synthetic/example")
        with patch.object(self.ops, "check") as check, patch.object(ops, "host_python") as execute:
            with self.assertRaises(ValueError):
                self.ops.snapshot(repo["id"], "local")
        check.assert_not_called()
        execute.assert_not_called()

    def test_snapshot_selects_new_remote_checkout_and_preserves_old_location_and_runs(self):
        self.host("runner")
        repo = self.ops.add("synthetic/example")
        old = {"id": "old-checkout", "path": "/synthetic/original", "host": "runner", "head": "a" * 40,
               "branch": "work", "dirty": True, "scanned_at": "2026-11-01T00:00:00Z"}
        repo.update(locations=[deepcopy(old)], upstream={"sha": "b" * 40, "size_kb": 100, "status": "updates"})
        self.store.put("repositories", repo)
        run = self.store.put("runs", {"id": "historical", "repo_id": repo["id"], "repo_sha": "a" * 40, "status": "passed"})
        prepared = {"path": "/synthetic-managed/new-checkout", "head": "b" * 40, "branch": "", "dirty": False}
        with patch.object(self.ops, "check"), patch.object(ops, "host_python", return_value=prepared) as remote:
            self.ops.snapshot(repo["id"], "runner")
        saved = self.store.get("repositories", repo["id"])
        self.assertEqual(saved["locations"][0], old)
        self.assertEqual(len(saved["locations"]), 2)
        self.assertEqual(saved["tracked_location_id"], saved["locations"][1]["id"])
        self.assertEqual(self.store.get("runs", "historical"), run)
        self.assertEqual(remote.call_args.args[0]["ssh"], "runner.local")
        self.assertEqual(remote.call_args.args[2]["sha"], "b" * 40)
        self.assertEqual(remote.call_args.args[2]["root"], "/synthetic-managed")

    def test_snapshot_git_commands_only_touch_new_pinned_checkout(self):
        root = Path(self.temp.name) / "managed"
        previous = root / "prior-checkout"
        previous.mkdir(parents=True)
        marker = previous / "keep.txt"
        marker.write_text("uncommitted synthetic work")
        commands = []

        def process(command, **kwargs):
            commands.append(command)
            if "clone" in command:
                Path(command[-1]).mkdir()
            if "rev-parse" in command:
                kwargs["stdout"].write(("b" * 40 + "\n").encode())
            return SimpleNamespace(returncode=0, poll=lambda: 0)

        output = io.StringIO()
        with patch("subprocess.Popen", side_effect=process), patch("shutil.disk_usage", return_value=SimpleNamespace(free=200 * 1024**3)), contextlib.redirect_stdout(output):
            exec(ops.SNAPSHOT, {"DATA": {"root": str(root), "name": "synthetic/example", "url": "https://github.com/synthetic/example",
                                         "sha": "b" * 40, "size_kb": 100}})
        result = json.loads(output.getvalue())
        self.assertNotEqual(result["path"], str(previous))
        self.assertEqual(marker.read_text(), "uncommitted synthetic work")
        self.assertEqual(result["head"], "b" * 40)
        self.assertTrue(any(command[-3:] == ["checkout", "--detach", "b" * 40] for command in commands))
        for command in commands:
            self.assertNotIn(str(previous), command)
            self.assertNotIn("pull", command)
            self.assertNotIn("reset", command)
            self.assertIn("core.hooksPath=/dev/null", command)

    def test_snapshot_reserve_failure_does_not_start_git_or_change_prior_checkout(self):
        root = Path(self.temp.name) / "managed"
        root.mkdir()
        marker = root / "prior-work.txt"
        marker.write_text("preserve")
        with patch("subprocess.Popen") as process, patch("shutil.disk_usage", return_value=SimpleNamespace(free=49 * 1024**3)):
            with self.assertRaises(ValueError):
                exec(ops.SNAPSHOT, {"DATA": {"root": str(root), "name": "synthetic/example", "url": "https://github.com/synthetic/example",
                                             "sha": "b" * 40, "size_kb": 100}})
        process.assert_not_called()
        self.assertEqual(marker.read_text(), "preserve")
        self.assertEqual(list(root.iterdir()), [marker])


if __name__ == "__main__":
    unittest.main()
