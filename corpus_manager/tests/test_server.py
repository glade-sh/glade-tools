import http.client
import json
import tempfile
import threading
import unittest
from http.server import ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch

from corpus_manager.history import parse_history_data
from corpus_manager.server import App, handler


class AppTestCase(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        with patch("corpus_manager.ops.GitHub"):
            self.app = App(self.root)
        self.addCleanup(self.app.store.db.close)
        self.addCleanup(self.app.executor.shutdown, wait=True)

    def repo(self, owner="synthetic", path="/synthetic/example", host="runner"):
        row = self.app.ops.add(owner + "/example")
        row["locations"] = [{"id": owner + "-location", "host": host, "path": path, "head": "a" * 40,
                             "branch": "main", "dirty": False}]
        self.app.store.put("repositories", row)
        return row


class AppTests(AppTestCase):
    def test_history_reimport_is_idempotent_and_never_infers_current_head(self):
        repo = self.repo()
        content = json.dumps({"repo": "example", "phase": "test", "status": "passed", "summary": {"total": 3, "passed": 3, "failed": 0}})
        for _ in range(2):
            self.app.save_runs(parse_history_data(content, source="synthetic/results.jsonl", host="runner", glade_sha="b" * 40), "2026-11-09T00:00:00Z")
        runs = self.app.store.all("runs")
        self.assertEqual(len(runs), 1)
        self.assertEqual(runs[0]["repo_id"], repo["id"])
        self.assertIsNone(runs[0]["repo_sha"])
        self.assertEqual(runs[0]["glade_sha"], "b" * 40)
        self.assertEqual(runs[0]["status"], "passed")
        self.assertIn("timestamp from dispatch", runs[0]["provenance"])

    def test_ambiguous_basename_is_not_attached_to_arbitrary_repo(self):
        self.repo("first", "/synthetic/one/example")
        self.repo("second", "/synthetic/two/example")
        parsed = parse_history_data(json.dumps({"repo": "example", "status": "passed"}), host="runner")
        self.app.save_runs(parsed)
        row = self.app.store.all("runs")[0]
        self.assertIsNone(row["repo_id"])
        self.assertEqual(row["identity_match"], "ambiguous")

    def test_history_reimport_after_inventory_does_not_duplicate_run(self):
        content = json.dumps({"repo": "example", "status": "passed", "summary": {"passed": 1}})
        self.app.save_runs(parse_history_data(content, source="synthetic/results.jsonl", host="runner"))
        self.assertIsNone(self.app.store.all("runs")[0]["repo_id"])
        repo = self.repo()
        self.app.save_runs(parse_history_data(content, source="synthetic/results.jsonl", host="runner"))
        runs = self.app.store.all("runs")
        self.assertEqual(len(runs), 1)
        self.assertEqual(runs[0]["repo_id"], repo["id"])

    def test_partial_history_stays_partial_in_catalog(self):
        self.repo()
        parsed = parse_history_data(json.dumps({"repo": "example", "status": "passed", "part": "s0of2", "shard": "0/2", "summary": {"passed": 2}}), host="runner")
        self.app.save_runs(parsed)
        self.assertEqual(self.app.store.all("runs")[0]["status"], "partial")

    def test_bootstrap_resolves_configured_host_id_and_dispatch_metadata(self):
        self.app.store.put("hosts", {"id": "runner", "ssh": "runner.local", "roots": [], "managed_root": "~/managed"})
        self.repo()
        config = {"sources": [{"host": "runner", "path": "~/history/results.jsonl", "glade_sha": "c" * 40,
                               "started_at": "2026-11-08T12:00:00Z", "label": "SYNTHETIC", "kind": "public"}]}
        (self.root / "history-sources.json").write_text(json.dumps(config))
        payload = [{"index": 0, "content": json.dumps({"repo": "example", "status": "passed", "summary": {"passed": 1}})}]
        with patch("corpus_manager.server.host_python", return_value=payload) as remote:
            message = self.app.import_bootstrap()
        self.assertIn("Imported 1", message)
        self.assertEqual(remote.call_args.args[0]["ssh"], "runner.local")
        self.assertEqual(remote.call_args.args[2]["sources"][0]["path"], "~/history/results.jsonl")
        row = self.app.store.all("runs")[0]
        self.assertEqual(row["host"], "runner")
        self.assertEqual(row["glade_sha"], "c" * 40)
        self.assertEqual(row["started_at"], "2026-11-08T12:00:00Z")

    def test_bootstrap_all_unavailable_or_unknown_hosts_is_not_success(self):
        self.app.store.put("hosts", {"id": "runner", "ssh": "runner.local", "roots": [], "managed_root": "~/managed"})
        config_path = self.root / "history-sources.json"
        config_path.write_text(json.dumps({"sources": [{"host": "runner", "path": "~/missing/results.jsonl"}]}))
        with patch("corpus_manager.server.host_python", return_value=[{"index": 0, "error": True}]):
            with self.assertRaises(ValueError):
                self.app.import_bootstrap()
        config_path.write_text(json.dumps({"sources": [{"host": "unconfigured", "path": "~/results.jsonl"}]}))
        with patch("corpus_manager.server.host_python") as remote:
            with self.assertRaises(ValueError):
                self.app.import_bootstrap()
        remote.assert_not_called()
        self.assertEqual(self.app.store.all("runs"), [])


class HTTPTests(AppTestCase):
    def setUp(self):
        super().setUp()
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), handler(self.app))
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.addCleanup(self.stop_server)

    def stop_server(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=5)

    def request(self, method, path, data=None, headers=None):
        connection = http.client.HTTPConnection("127.0.0.1", self.server.server_port, timeout=3)
        self.addCleanup(connection.close)
        body = json.dumps(data) if data is not None else None
        connection.request(method, path, body=body, headers=headers or {})
        response = connection.getresponse()
        return response.status, json.loads(response.read())

    def test_state_exposes_token_only_to_loopback_host(self):
        status, state = self.request("GET", "/api/state")
        self.assertEqual(status, 200)
        self.assertEqual(state["csrf_token"], self.app.token)
        status, _ = self.request("GET", "/api/state", headers={"Host": "attacker.invalid"})
        self.assertEqual(status, 403)
        status, exported = self.request("GET", "/api/export")
        self.assertEqual(status, 200)
        self.assertNotIn("csrf_token", exported)

    def test_mutation_requires_token_and_matching_origin(self):
        base = {"Content-Type": "application/json"}
        status, _ = self.request("POST", "/api/repos", {"url": "synthetic/example"}, base)
        self.assertEqual(status, 403)
        status, _ = self.request("POST", "/api/repos", {"url": "synthetic/example"}, {**base, "X-Corpus-Token": "wrong"})
        self.assertEqual(status, 403)
        valid = {**base, "X-Corpus-Token": self.app.token}
        status, _ = self.request("POST", "/api/repos", {"url": "synthetic/example"}, {**valid, "Origin": "http://attacker.invalid"})
        self.assertEqual(status, 403)
        status, _ = self.request("POST", "/api/repos", {"url": "synthetic/example"}, {**valid, "Host": "attacker.invalid"})
        self.assertEqual(status, 403)
        self.assertEqual(self.app.store.all("repositories"), [])
        status, row = self.request("POST", "/api/repos", {"url": "synthetic/example"}, {**valid, "Origin": "http://127.0.0.1:%s" % self.server.server_port})
        self.assertEqual(status, 200)
        self.assertEqual(row["name"], "synthetic/example")

    def test_rejects_non_json_and_invalid_count_inputs(self):
        status, _ = self.request("POST", "/api/repos", {"url": "synthetic/example"}, {"X-Corpus-Token": self.app.token, "Content-Type": "text/plain"})
        self.assertEqual(status, 415)
        repo = self.repo()
        status, _ = self.request("POST", "/api/runs", {"repo_id": repo["id"], "passed": -1}, {"X-Corpus-Token": self.app.token, "Content-Type": "application/json"})
        self.assertEqual(status, 400)
        self.assertEqual(self.app.store.all("runs"), [])


if __name__ == "__main__":
    unittest.main()
