import importlib.util
import json
import pathlib
import tempfile
import unittest
from unittest import mock

SCRIPT = pathlib.Path(__file__).with_name('corpus_nightly.py')
spec = importlib.util.spec_from_file_location('corpus_nightly', SCRIPT)
nightly = importlib.util.module_from_spec(spec)
spec.loader.exec_module(nightly)


class CorpusNightlyTest(unittest.TestCase):
    def test_tier_b_rejects_blocked_and_missing_goldens(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            (root / 'goldens').mkdir()
            config = {'schemaVersion': 1, 'repos': [
                {'name': 'synthetic-a', 'tier': 'A'},
                {'name': 'synthetic-b', 'tier': 'B', 'blocked': 'source unavailable'},
                {'name': 'synthetic-c', 'tier': 'C'},
            ]}
            (root / 'tiers.json').write_text(json.dumps(config))
            result = nightly.preflight(root)
            self.assertEqual(result, {'selected': 2, 'blocked': 1, 'missing': 1})
            (root / 'goldens' / 'synthetic-a.json').write_text('{}')
            self.assertEqual(nightly.preflight(root), {'selected': 2, 'blocked': 1, 'missing': 0})

    def test_partial_tier_runs_while_all_blocked_tier_does_not(self):
        self.assertTrue(nightly.has_runnable_entries({'selected': 59, 'blocked': 4, 'missing': 0}))
        self.assertFalse(nightly.has_runnable_entries({'selected': 4, 'blocked': 4, 'missing': 0}))
        self.assertFalse(nightly.has_runnable_entries({'selected': 1, 'blocked': 0, 'missing': 1}))

    def test_partial_preflight_runs_and_preserves_infra_exit(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            config = root / 'config'
            (config / 'goldens').mkdir(parents=True)
            (root / 'work').mkdir()
            (root / 'results').mkdir()
            (config / 'tiers.json').write_text(json.dumps({'schemaVersion': 1, 'repos': [
                {'name': 'synthetic-a', 'tier': 'A'},
                {'name': 'synthetic-b', 'tier': 'B', 'blocked': 'source unavailable'}]}))
            (config / 'goldens' / 'synthetic-a.json').write_text('{}')
            with (root / 'results' / 'run.log').open('w+') as log, \
                 mock.patch.object(nightly.subprocess, 'run') as run, \
                 mock.patch.object(nightly.subprocess, 'check_output', return_value='a' * 40):
                runner = root / 'runner'
                runner.write_text('synthetic runner')
                run.return_value.returncode = 2
                self.assertEqual(nightly.run(log, root, config, runner, root / 'go', 'main'), 2)
                self.assertGreaterEqual(run.call_count, 3)
                log.seek(0)
                self.assertIn('1 blocked', log.read())

    def test_blocked_preflight_does_not_start_external_commands(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            config = root / 'config'
            config.mkdir()
            (config / 'tiers.json').write_text(json.dumps({'schemaVersion': 1, 'repos': [
                {'name': 'synthetic-b', 'tier': 'B', 'blocked': 'source unavailable'}]}))
            with (root / 'run.log').open('w+') as log, mock.patch.object(nightly.subprocess, 'run') as run:
                self.assertEqual(nightly.run(log, root, config, root / 'runner', root / 'go', 'main'), 2)
                run.assert_not_called()
                log.seek(0)
                self.assertIn('CORPUS-INFRA', log.read())

    def test_overlap_records_infra_without_starting_external_commands(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            args = ['nightly', '--state', str(root), '--config', str(root / 'config'),
                    '--runner', str(root / 'runner'), '--go', str(root / 'go')]
            with mock.patch('sys.argv', args), \
                 mock.patch.object(nightly.fcntl, 'flock', side_effect=BlockingIOError), \
                 mock.patch.object(nightly.subprocess, 'run') as run:
                self.assertEqual(nightly.main(), 2)
                run.assert_not_called()
            summaries = list((root / 'results').glob('*.txt'))
            self.assertEqual(len(summaries), 1)
            self.assertTrue(summaries[0].read_text().startswith('CORPUS-INFRA'))

    def test_summary_starts_with_verdict_and_rejects_mismatch(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            report, summary = root / 'run.json', root / 'run.txt'
            report.write_text(json.dumps({'first': 'CORPUS-NEW 1 in 1 repos', 'exitCode': 1}))
            self.assertEqual(nightly.write_summary(summary, report, 1), 1)
            self.assertEqual(summary.read_text().splitlines()[0], 'CORPUS-NEW 1 in 1 repos')
            summary.unlink()
            self.assertEqual(nightly.write_summary(summary, report, 0), 2)
            self.assertTrue(summary.read_text().startswith('CORPUS-INFRA'))

    def test_config_fingerprint_covers_golden_contents(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            (root / 'goldens').mkdir()
            (root / 'tiers.json').write_text('{}')
            golden = root / 'goldens' / 'synthetic-a.json'
            golden.write_text('{}')
            before = nightly.config_fingerprint(root)
            golden.write_text('{"changed": true}')
            self.assertNotEqual(before, nightly.config_fingerprint(root))

    def test_rejects_empty_or_unsafe_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            for repos in ([], [{'name': '../escape', 'tier': 'A'}],
                          [{'name': 'same', 'tier': 'A'}, {'name': 'same', 'tier': 'B'}]):
                (root / 'tiers.json').write_text(json.dumps({'schemaVersion': 1, 'repos': repos}))
                with self.subTest(repos=repos), self.assertRaises(ValueError):
                    nightly.preflight(root)


if __name__ == '__main__':
    unittest.main()
