"""Synthetic checks for disk watchdogs and safe host/worker failures."""
import contextlib
import io
import signal
import subprocess
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock, call, patch

from corpus_manager import ops
from corpus_manager.server import App


class SnapshotSafetyTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "managed"
        self.prior = self.root / "existing-checkout"
        self.prior.mkdir(parents=True)
        self.marker = self.prior / "work.txt"
        self.marker.write_text("preserve unrelated work")
        self.data = {"root": str(self.root), "name": "synthetic/example",
                     "url": "https://github.com/synthetic/example", "sha": "b" * 40,
                     "size_kb": 100, "local_node": "different-synthetic-host"}

    def process(self, ignore_term=False):
        process = Mock()
        process.pid = 456789
        process.returncode = None
        process.poll.side_effect = lambda: process.returncode
        waits = 0

        def wait(timeout=None):
            nonlocal waits
            waits += 1
            if ignore_term and waits == 1:
                raise subprocess.TimeoutExpired("synthetic-git", timeout)
            process.returncode = -signal.SIGKILL if ignore_term else -signal.SIGTERM
            return process.returncode

        process.wait.side_effect = wait

        def start(command, **kwargs):
            self.assertIn("clone", command)
            self.assertTrue(kwargs["start_new_session"])
            target = Path(command[-1])
            target.mkdir()
            (target / "partial.txt").write_text("incomplete clone")
            process.target = target
            return process

        return process, start

    def test_falling_free_space_terminates_owned_git_and_preserves_partial_checkout(self):
        process, start = self.process()
        free = [SimpleNamespace(free=200 * 1024**3),
                SimpleNamespace(free=50 * 1024**3 + 512 * 1024**2)]
        with patch("subprocess.Popen", side_effect=start) as launch, \
                patch("shutil.disk_usage", side_effect=free), \
                patch("os.killpg") as kill, patch("time.sleep") as sleep, \
                patch("time.monotonic", return_value=0):
            with self.assertRaisesRegex(ValueError, "disk reserve") as error:
                exec(ops.SNAPSHOT, {"DATA": self.data})
        launch.assert_called_once()
        kill.assert_called_once_with(process.pid, signal.SIGTERM)
        process.wait.assert_called_once_with(timeout=3)
        sleep.assert_not_called()
        self.assertIn(str(process.target), str(error.exception))
        self.assertEqual(self.marker.read_text(), "preserve unrelated work")
        self.assertTrue((process.target / "partial.txt").is_file())

    def test_timeout_escalates_only_owned_process_group_without_waiting_in_test(self):
        process, start = self.process(ignore_term=True)
        with patch("subprocess.Popen", side_effect=start) as launch, \
                patch("shutil.disk_usage", return_value=SimpleNamespace(free=200 * 1024**3)), \
                patch("os.killpg") as kill, patch("time.sleep") as sleep, \
                patch("time.monotonic", side_effect=[0, 241]):
            with self.assertRaisesRegex(ValueError, "time limit"):
                exec(ops.SNAPSHOT, {"DATA": self.data})
        launch.assert_called_once()
        self.assertEqual(kill.call_args_list, [call(process.pid, signal.SIGTERM),
                                              call(process.pid, signal.SIGKILL)])
        self.assertEqual(process.wait.call_args_list, [call(timeout=3), call()])
        sleep.assert_not_called()
        self.assertEqual(self.marker.read_text(), "preserve unrelated work")
        self.assertTrue(process.target.is_dir())

    def test_owned_host_failure_reaches_caller_without_subprocess_stderr(self):
        def run(command, **kwargs):
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                exec(kwargs["input"], {})
            return SimpleNamespace(returncode=0, stdout=output.getvalue(),
                                   stderr="synthetic-secret-must-not-be-shown")

        with patch.object(ops.subprocess, "run", side_effect=run):
            with self.assertRaisesRegex(ValueError, "Insufficient disk reserve") as error:
                ops.host_python({"ssh": "synthetic.example"},
                                "raise ValueError('Insufficient disk reserve')", {})
            self.assertNotIn("synthetic-secret", str(error.exception))
            with self.assertRaises(ValueError) as unexpected:
                ops.host_python({"ssh": "synthetic.example"},
                                "raise RuntimeError('synthetic-private-detail')", {})
            self.assertNotIn("synthetic-private-detail", str(unexpected.exception))

    def test_worker_records_controlled_snapshot_failure(self):
        with patch("corpus_manager.ops.GitHub"):
            app = App(Path(self.temp.name) / "catalog")
        self.addCleanup(app.store.db.close)
        message = "Insufficient disk reserve; partial checkout preserved at /synthetic/partial"

        def fail():
            raise ValueError(message)

        result = app.job("Prepare checkout", fail)
        app.executor.shutdown(wait=True)
        job = app.store.get("jobs", result["job"])
        self.assertEqual(job["status"], "failed")
        self.assertEqual(job["message"], message)
        self.assertTrue(job["finished_at"])


if __name__ == "__main__":
    unittest.main()
