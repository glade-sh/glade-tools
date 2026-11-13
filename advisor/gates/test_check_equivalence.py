import json
import shutil
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

HERE = Path(__file__).resolve().parent

class CheckEquivalenceTest(unittest.TestCase):
    def run_gate(self, runner, mode, procs='3'):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        root = Path(tmp.name)
        (root / 'clone' / 'sample').mkdir(parents=True)
        binary = root / 'fake-glade'
        binary.write_text('''#!/usr/bin/env python3
import json, os, sys
args = sys.argv
out = args[args.index('--output') + 1]
mode = os.environ['FAKE_MODE']
serial = os.environ['GOMAXPROCS'] == '1'
if '--no-cache' not in args: sys.exit(4)
if mode == 'missing' and serial: sys.exit(3)
if mode == 'malformed' and serial:
    open(out, 'w').write('{')
    sys.exit(0)
value = {'diagnostics': [{'code': 'A'}, {'code': 'B'}], 'summary': {'types': 2}}
if mode == 'reorder' and serial: value['diagnostics'].reverse()
if mode == 'different' and serial: value['diagnostics'][0]['code'] = 'C'
open(out, 'w').write(json.dumps(value))
sys.exit(1 if mode == 'exit' and serial else 0)
''')
        binary.chmod(0o755)
        (root / 'config.json').write_text('{"repos": {}, "depSources": {}}')
        cmd = [sys.executable, str(HERE / runner), '--bin', str(binary), '--label', 'trial', '--clone', str(root / 'clone'), '--out', str(root / 'out'), '--repos', 'sample', '--procs', procs, '--skip-tests', '--no-overlap']
        if runner == 'run3.py':
            cmd += ['--corpus-config', str(root / 'config.json')]
        env = dict(os.environ, FAKE_MODE=mode)
        result = subprocess.run(cmd, env=env, capture_output=True, text=True, timeout=30)
        receipt = root / 'out' / 'trial' / 'sample.check-equivalence.json'
        return result, json.loads(receipt.read_text()) if receipt.exists() else None

    def test_both_runners_compare_and_fail_closed(self):
        for runner in ('run2.py', 'run3.py'):
            for mode, expected in [('same', 'match'), ('reorder', 'match'), ('different', 'mismatch'), ('missing', 'incomplete'), ('malformed', 'incomplete'), ('exit', 'mismatch')]:
                with self.subTest(runner=runner, mode=mode):
                    result, receipt = self.run_gate(runner, mode)
                    self.assertIsNotNone(receipt, result.stdout + result.stderr)
                    self.assertEqual(receipt['verdict'], expected)
                    self.assertEqual(result.returncode == 0, expected == 'match')

    def test_status_reports_receipts_and_missing_results(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / 'diff2.py').write_text('')
            output = root / 'out' / 'trial-public'
            output.mkdir(parents=True)
            (output / 'run.log').write_text('00:00:00Z label=trial repos=first second third\n00:00:01Z first check start\n00:00:02Z second check start\n00:00:03Z third check start\n00:00:04Z done\n')
            (output / 'first.check-equivalence.json').write_text('{"verdict":"match"}')
            (output / 'second.check-equivalence.json').write_text('{"verdict":"mismatch"}')
            result = subprocess.run([sys.executable, str(HERE / 'gstat.py'), str(root), 'pub:trial-public'], capture_output=True, text=True, check=True)
            self.assertIn('check equivalence: match=1 mismatch=1 incomplete=1', result.stdout)

    def test_exit_only_diff_is_readable(self):
        sys.path.insert(0, str(HERE))
        import check_equivalence
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for name in ('a', 'b'):
                (root / name).write_text('{"diagnostics": []}')
            receipt = check_equivalence.compare(root / 'a', root / 'b', 0, 1, 3, root / 'diff')
            self.assertEqual(receipt['verdict'], 'mismatch')
            self.assertIn('serial=1 parallel=0', (root / 'diff').read_text())

    def test_interrupted_plan_and_skipped_parts(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / 'diff2.py').write_text('')
            output = root / 'out' / 'trial-public'
            output.mkdir(parents=True)
            (output / 'check-plan.json').write_text('["first", "second"]')
            (output / 'run.log').write_text('00:00:00Z label=trial repos=first second\n00:00:01Z first check start\n')
            (output / 'first.check-equivalence.json').write_text('{"verdict":"match"}')
            result = subprocess.run([sys.executable, str(HERE / 'gstat.py'), str(root), 'pub:trial-public'], capture_output=True, text=True, check=True)
            self.assertIn('match=1 mismatch=0 incomplete=1', result.stdout)
            subprocess.run([sys.executable, str(HERE / 'check_plan.py'), str(output), '--shard', 'second=1/2', '--', 'first', 'second'], check=True)
            self.assertEqual(json.loads((output / 'check-plan.json').read_text()), ['first'])

    def test_runners_omit_include_only_and_nonzero_shard_from_plan(self):
        for runner in ('run2.py', 'run3.py'):
            with self.subTest(runner=runner), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                for name in ('included', 'sharded'):
                    (root / 'clone' / name).mkdir(parents=True)
                cmd = [sys.executable, str(HERE / runner), '--bin', '/unused', '--label', 'trial', '--clone', str(root / 'clone'), '--out', str(root / 'out'), '--repos', 'included', 'sharded', '--include-class', 'included=Example', '--shard', 'sharded=1/2', '--skip-tests', '--no-overlap']
                subprocess.run(cmd, check=True, capture_output=True)
                log = (root / 'out' / 'trial' / 'run.log').read_text()
                self.assertIn('check plan=[]', log)
                self.assertNotIn('check start', log)

    def test_public_wrapper_retains_failed_runner_status_after_cleanup(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for name in ('pubgate2.sh', 'check_plan.py'):
                shutil.copy(HERE / name, root / name)
            (root / 'run2.py').write_text('import sys; sys.exit(7)\n')
            (root / 'merge_shards.py').write_text('')
            (root / 'clone-curated' / 'sample').mkdir(parents=True)
            (root / 'out').mkdir()
            env = dict(os.environ, HIST='unused', BASELINES='unused')
            result = subprocess.run(['bash', str(root / 'pubgate2.sh'), 'binary', 'trial', 'sample'], env=env)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse((root / 'clone-trial').exists())
            self.assertTrue((root / 'out' / 'DIFF2-trial-public.txt').exists())

    def test_planned_lane_propagates_runner_failure_across_sets(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for name in ('one', 'two'):
                (root / name / name).mkdir(parents=True)
            (root / 'run2.py').write_text('import sys; sys.exit(7 if "one" in sys.argv else 0)\n')
            cmd = [sys.executable, str(HERE / 'plan_lanes.py'), '--lanes', '1', '--label', 'trial', '--bin', 'binary', '--hist', 'old', '--w', str(root), '--out', str(root / 'out'), '--outdir', str(root / 'lanes'), '--runroot', str(root / 'runs'), '--set', 'one=' + str(root / 'one'), '--set', 'two=' + str(root / 'two')]
            subprocess.run(cmd, check=True, capture_output=True)
            self.assertEqual(json.loads((root / 'out' / 'one-trial' / 'check-plan.json').read_text()), ['one'])
            result = subprocess.run(['bash', str(root / 'lanes' / 'trial-lane0.sh')], capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse((root / 'runs' / 'lane0' / 'run-two-trial').exists())

    def test_one_worker_is_not_a_comparison(self):
        result, receipt = self.run_gate('run2.py', 'same', '1')
        self.assertIsNotNone(receipt)
        self.assertEqual(receipt['verdict'], 'incomplete')
        self.assertNotEqual(result.returncode, 0)

if __name__ == '__main__':
    unittest.main()
