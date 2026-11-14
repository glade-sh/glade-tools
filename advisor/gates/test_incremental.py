import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import incremental

T = incremental.TIMEOUT
FIXTURE = HERE / 'testdata' / 'plan_lanes_main'


class UnitTimeoutTest(unittest.TestCase):
    def test_clean_history_clamps(self):
        self.assertEqual(incremental.unit_timeout(10, 3, 300, 3600), 300)
        self.assertEqual(incremental.unit_timeout(500, 3, 300, 3600), 1500)
        self.assertEqual(incremental.unit_timeout(5000, 3, 300, 3600), 3600)

    def test_timeout_history_gives_1200(self):
        self.assertEqual(incremental.unit_timeout(T, 3, 300, 3600), 1200)
        self.assertEqual(incremental.unit_timeout(T, 3, 300, 2400), 1200)
        self.assertEqual(incremental.unit_timeout(T, 3, 300, 900), 900)   # never above the default

    def test_unknown_history_gives_default(self):
        for hist in (None, 'junk'):
            self.assertEqual(incremental.unit_timeout(hist, 3, 300, 3600), 3600)

    def write(self, rows):
        d = tempfile.TemporaryDirectory()
        self.addCleanup(d.cleanup)
        (Path(d.name) / 'results.jsonl').write_text('\n'.join(json.dumps(r) for r in rows) + '\nnot json\n')
        return incremental.history_walls(d.name)

    def test_history_walls(self):
        w = self.write([
            {'repo': 'a', 'phase': 'test', 'exit': 0, 'wall_s': 200.0},
            {'repo': 'b', 'phase': 'test', 'exit': 'timeout', 'wall_s': 3600.0},
            {'repo': 'c', 'phase': 'test', 'exit': 0, 'wall_s': 50.0, 'part': 's0of2'},
            {'repo': 'c', 'phase': 'test', 'exit': 0, 'wall_s': 70.0, 'part': 's1of2'},
            {'repo': 'd', 'phase': 'test', 'exit': 1, 'wall_s': 5.0},
        ])
        self.assertEqual(w[('a', 'test')], 200.0)
        self.assertEqual(w[('b', 'test')], T)
        self.assertEqual(w[('c', 'test')], 120.0)
        self.assertIsNone(w[('d', 'test')])

    def test_incomplete_failed_and_mixed_are_unusable(self):
        w = self.write([
            {'repo': 'a', 'phase': 'test', 'exit': 'timeout', 'wall_s': 10, 'part': 's0of2'},
            {'repo': 'a', 'phase': 'test', 'exit': 0, 'wall_s': 10, 'part': 's1of2'},
            {'repo': 'b', 'phase': 'test', 'exit': 0, 'wall_s': 10, 'part': 's0of2'},
            {'repo': 'c', 'phase': 'test', 'exit': 0, 'wall_s': 10},
            {'repo': 'c', 'phase': 'test', 'exit': 0, 'wall_s': 10, 'part': 's0of1'},
            {'repo': 'e', 'phase': 'test', 'exit': 0, 'wall_s': 10, 'part': 'inc'},
        ])
        for repo in 'abce':
            self.assertIsNone(w[(repo, 'test')])

    def test_duplicate_whole_rows_are_unusable_in_either_order(self):
        for second in (0, 1, 'timeout'):
            rows = [{'repo': 'a', 'phase': 'test', 'exit': code, 'wall_s': 10} for code in (0, second)]
            for ordered in (rows, list(reversed(rows))):
                with self.subTest(second=second, rows=ordered):
                    self.assertIsNone(self.write(ordered)[('a', 'test')])

    def test_invalid_wall_cannot_hide_mixed_history(self):
        for bad_wall in ('bad', None, -1, float('nan'), float('inf')):
            rows = [{'repo': 'a', 'phase': 'test', 'exit': 0, 'wall_s': 10},
                    {'repo': 'a', 'phase': 'test', 'exit': 1, 'wall_s': bad_wall}]
            for ordered in (rows, list(reversed(rows))):
                with self.subTest(bad_wall=bad_wall, rows=ordered):
                    self.assertIsNone(self.write(ordered)[('a', 'test')])

    def test_missing_history(self):
        self.assertEqual(incremental.history_walls('/nonexistent-dir-xyz'), {})


def make_world(root, public_config=True):
    """A synthetic work dir with one set clone of repos a, b, c and BMO-style history."""
    root = Path(root)
    src = root / 'src'
    for r in 'abc':
        (src / r).mkdir(parents=True)
        (src / r / 'x.cls').write_text('1')
    w = root / 'w'
    (w / 'out' / 'SET-H1').mkdir(parents=True)
    rows = [{'repo': 'a', 'phase': 'check', 'exit': 0, 'wall_s': 5}, {'repo': 'a', 'phase': 'test', 'exit': 0, 'wall_s': 100},
            {'repo': 'b', 'phase': 'check', 'exit': 0, 'wall_s': 5}, {'repo': 'b', 'phase': 'test', 'exit': 0, 'wall_s': 40},
            {'repo': 'c', 'phase': 'check', 'exit': 0, 'wall_s': 5}, {'repo': 'c', 'phase': 'test', 'exit': 'timeout', 'wall_s': 3600}]
    (w / 'out' / 'SET-H1' / 'results.jsonl').write_text('\n'.join(json.dumps(r) for r in rows) + '\n')
    cfg = w / 'corpus-config.json'
    cfg.write_text('{"public": true}\n')
    if public_config:
        (w / 'corpus-config.json.sha256').write_text(hashlib.sha256(cfg.read_bytes()).hexdigest() + '\n')
    return src, w, cfg


def plan(script, root, src, w, *extra, outdir=None):
    outdir = outdir or Path(root) / 'lanes'
    cmd = [sys.executable, str(script), '--lanes', '2', '--label', 'NEW', '--bin', '/B/glade', '--hist', 'H1',
           '--set', 'SET=%s' % src, '--w', str(w), '--out', str(Path(w) / 'out'), '--outdir', str(outdir),
           '--runroot', '/RR', '--shard3', '', *extra]
    return subprocess.run(cmd, capture_output=True, text=True)


def snapshot(root, w, outdir):
    """Generated lane scripts and check-plan.json with the temp path replaced."""
    files = {}
    for p in sorted(Path(outdir).glob('*.sh')):
        files[p.name] = p.read_text().replace(str(root), '<T>')
    files['check-plan.json'] = (Path(w) / 'out' / 'SET-NEW' / 'check-plan.json').read_text()
    return files


class TrainPlanTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(os.path.realpath(self.tmp.name))

    def lanes(self):
        return list((self.root / 'lanes').glob('*.sh')) if (self.root / 'lanes').exists() else []

    def train(self, *extra, public_config=True, **kw):
        src, w, cfg = make_world(self.root, public_config)
        return plan(HERE / 'plan_lanes.py', self.root, src, w, *extra, **kw), src, w, cfg

    def test_requires_only(self):
        r, src, w, cfg = self.train('--train', '--cfg', str(self.root / 'w' / 'corpus-config.json'))
        self.assertEqual(r.returncode, 2, r.stderr)
        self.assertEqual(self.lanes(), [])

    def test_unknown_name(self):
        r, src, w, cfg = self.train('--train', '--only', 'a,zzz', '--cfg', str(self.root / 'w' / 'corpus-config.json'))
        self.assertEqual(r.returncode, 2, r.stderr)
        self.assertIn('zzz', r.stderr)
        self.assertEqual(self.lanes(), [])

    def test_only_file(self):
        src, w, cfg = make_world(self.root)
        lst = self.root / 'only.txt'
        lst.write_text('# tier 1\na\nb  # changed\n')
        r = plan(HERE / 'plan_lanes.py', self.root, src, w, '--train', '--only', '@%s' % lst, '--cfg', str(cfg))
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(json.loads((w / 'out/SET-NEW/check-plan.json').read_text()), ['a', 'b'])

    def test_config_rules_stop_the_plan(self):
        src, w, cfg = make_world(self.root)
        only = ['--train', '--only', 'a']
        other = self.root / 'other.json'
        other.write_text(cfg.read_text())
        cases = {'no cfg': only, 'other path': only + ['--cfg', str(other)]}
        for name, args in cases.items():
            with self.subTest(name):
                r = plan(HERE / 'plan_lanes.py', self.root, src, w, *args)
                self.assertEqual(r.returncode, 2, r.stderr)
                self.assertEqual(self.lanes(), [])
        sha = w / 'corpus-config.json.sha256'
        with self.subTest('mismatch'):
            sha.write_text('0' * 64 + '\n')
            r = plan(HERE / 'plan_lanes.py', self.root, src, w, *only, '--cfg', str(cfg))
            self.assertEqual(r.returncode, 2, r.stderr)
            self.assertEqual(self.lanes(), [])
        with self.subTest('missing'):
            sha.unlink()
            r = plan(HERE / 'plan_lanes.py', self.root, src, w, *only, '--cfg', str(cfg))
            self.assertEqual(r.returncode, 2, r.stderr)
            self.assertEqual(self.lanes(), [])
        self.assertFalse((w / 'out/SET-NEW').exists())

    def test_scope_file_and_lane_scripts(self):
        r, src, w, cfg = self.train('--train', '--only', 'a,b', '--cfg', str(self.root / 'w' / 'corpus-config.json'))
        self.assertEqual(r.returncode, 0, r.stderr)
        target = w / 'out' / 'SET-NEW'
        scope = json.loads((target / 'train-scope.json').read_text())
        self.assertEqual(scope['planned'], ['a', 'b'])
        self.assertEqual(scope['not_planned'], ['c'])
        self.assertNotIn('ran', scope)
        self.assertNotIn('not_run', scope)
        self.assertEqual(scope['config_sha256'], hashlib.sha256(cfg.read_bytes()).hexdigest())
        self.assertEqual(sorted(p.name for p in target.iterdir()), ['check-plan.json', 'train-scope.json'])
        text = ''.join(p.read_text() for p in self.lanes())
        self.assertIn('--unit-timeout-history SET-H1', text)
        self.assertIn('--corpus-config %s' % cfg, text)
        self.assertNotIn("'c'", text.split('--repos', 1)[1])
        self.assertNotIn("--repos 'c'", text)

    def test_exclusions_and_reused_label_fail_closed(self):
        src, w, cfg = make_world(self.root)
        target = w / 'out/SET-NEW'
        target.mkdir()
        stale = target / 'c.test.stdout'
        stale.write_text('stale')
        args = ('--train', '--only', 'a,b', '--exclude', 'b', '--cfg', str(cfg))
        r = plan(HERE / 'plan_lanes.py', self.root, src, w, *args)
        self.assertNotEqual(r.returncode, 0)
        self.assertEqual(stale.read_text(), 'stale')
        self.assertEqual(self.lanes(), [])
        stale.unlink()
        r = plan(HERE / 'plan_lanes.py', self.root, src, w, *args)
        self.assertEqual(r.returncode, 0, r.stderr)
        scope = json.loads((target / 'train-scope.json').read_text())
        self.assertEqual(scope['planned'], ['a'])
        self.assertEqual(scope['not_planned'], ['b', 'c'])
        self.assertEqual(json.loads((target / 'check-plan.json').read_text()), scope['planned'])

    def test_empty_effective_scope_fails_and_writes_nothing(self):
        src, w, cfg = make_world(self.root)
        r = plan(HERE / 'plan_lanes.py', self.root, src, w, '--train', '--only', 'a', '--exclude', 'a', '--cfg', str(cfg))
        self.assertEqual(r.returncode, 2, r.stderr)
        self.assertEqual(self.lanes(), [])
        self.assertFalse((w / 'out/SET-NEW').exists())

    def test_empty_scope_through_val2_reports_plan_failed(self):
        src, w, cfg = make_world(self.root)
        for f in ('val2.sh', 'plan_lanes.py'):
            (w / f).write_text((HERE / f).read_text())
        env = dict(os.environ, W=str(w), CFG='1', PLAN_ARGS='--train --only a --set SET=%s --shard3 ""' % src,
                   RUNROOT=str(self.root / 'rr'))
        r = subprocess.run(['bash', str(w / 'val2.sh'), '/B/glade', 'NEW', '--lanes', '2', '--hist', 'H1', '--exclude', 'a'],
                           env=env, capture_output=True, text=True, timeout=120)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        log = (w / 'val2-NEW.log').read_text()
        self.assertIn('plan failed', log)
        self.assertTrue(log.rstrip().endswith('done'))
        self.assertNotIn('new total', log)
        self.assertEqual(list((w / 'lanes').glob('*.sh')) if (w / 'lanes').exists() else [], [])

    def test_train_rejects_colliding_set_outputs(self):
        src, w, cfg = make_world(self.root)
        r = plan(HERE / 'plan_lanes.py', self.root, src, w, '--train', '--only', 'a',
                 '--cfg', str(cfg), '--set', 'SET=%s' % src)
        self.assertEqual(r.returncode, 2, r.stderr)
        self.assertFalse((w / 'out/SET-NEW').exists())
        self.assertEqual(self.lanes(), [])

    def test_train_rejects_output_directory_that_lanes_would_not_use(self):
        src, w, cfg = make_world(self.root)
        other = self.root / 'other-out'
        r = plan(HERE / 'plan_lanes.py', self.root, src, w, '--train', '--only', 'a',
                 '--cfg', str(cfg), '--out', str(other))
        self.assertEqual(r.returncode, 2, r.stderr)
        self.assertFalse((w / 'out/SET-NEW').exists())
        self.assertEqual(self.lanes(), [])

    def test_non_train_only_matches_main_for_existing_arguments(self):
        src, w, cfg = make_world(self.root)
        main = self.root / 'main.py'
        main.write_bytes(subprocess.check_output(['git', 'show', 'origin/main:advisor/gates/plan_lanes.py'], cwd=HERE))
        for arg in ('a, b', 'a,,b', 'a,', '@missing'):
            with self.subTest(arg=arg):
                expected = plan(main, self.root, src, w, '--only', arg, outdir=self.root / 'expected')
                expected_files = snapshot(self.root, w, self.root / 'expected')
                actual = plan(HERE / 'plan_lanes.py', self.root, src, w, '--only', arg, outdir=self.root / 'actual')
                self.assertEqual(actual.returncode, expected.returncode, actual.stderr)
                self.assertEqual(snapshot(self.root, w, self.root / 'actual'), expected_files)

    def test_default_output_matches_main(self):
        src, w, cfg = make_world(self.root)
        r = plan(HERE / 'plan_lanes.py', self.root, src, w)
        self.assertEqual(r.returncode, 0, r.stderr)
        got = snapshot(self.root, w, self.root / 'lanes')
        want = json.loads((FIXTURE / 'plain.json').read_text())
        self.assertEqual(got, want)
        self.assertFalse((w / 'out/SET-NEW/train-scope.json').exists())

    def test_default_cfg_output_matches_main(self):
        src, w, cfg = make_world(self.root)
        r = plan(HERE / 'plan_lanes.py', self.root, src, w, '--cfg', str(cfg), '--only', 'a,b')
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(snapshot(self.root, w, self.root / 'lanes'), json.loads((FIXTURE / 'cfg_only.json').read_text()))


STUB = r'''#!/usr/bin/env python3
import json, sys
a = sys.argv[1:]
import os
if '--project' in a and not os.path.isdir(a[a.index('--project') + 1]):
    sys.exit(2)
if a[0] == 'check':
    open(a[a.index('--output') + 1], 'w').write(json.dumps({'status': 'ok', 'summary': {}, 'diagnostics': []}))
elif a[0] == 'test':
    print(json.dumps({'status': 'ok', 'summary': {}, 'tests': []}))
'''


class Run3Test(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.bin = self.root / 'glade'
        self.bin.write_text(STUB)
        self.bin.chmod(0o755)
        self.clone = self.root / 'clone'
        for r in 'abc':
            (self.clone / r).mkdir(parents=True)
            (self.clone / r / 'x.cls').write_text('1')

    def run3(self, *extra):
        return subprocess.run([sys.executable, str(HERE / 'run3.py'), '--bin', str(self.bin), '--label', 'L',
                               '--clone', str(self.clone), '--out', str(self.root / 'out'), '--no-overlap',
                               '--procs', '2', *extra], capture_output=True, text=True, timeout=300)

    def test_no_new_flag_runs_everything(self):
        r = self.run3()
        self.assertEqual(r.returncode, 0, r.stderr + r.stdout)
        rows = [json.loads(x) for x in (self.root / 'out/L/results.jsonl').read_text().splitlines()]
        self.assertEqual(sorted((x['repo'], x['phase']) for x in rows),
                         [(r_, p) for r_ in 'abc' for p in ('check', 'test')])
        self.assertNotIn('unit timeouts', (self.root / 'out/L/run.log').read_text())

    def test_unit_timeout_history_is_logged(self):
        hist = self.root / 'hist'
        hist.mkdir()
        (hist / 'results.jsonl').write_text('\n'.join(json.dumps(x) for x in [
            {'repo': 'a', 'phase': 'test', 'exit': 0, 'wall_s': 10},
            {'repo': 'b', 'phase': 'test', 'exit': 'timeout', 'wall_s': 3600}]) + '\n')
        self.run3('--unit-timeout-history', str(hist))
        log = (self.root / 'out/L/run.log').read_text()
        self.assertIn('a unit timeouts check=900s test=300s', log)
        self.assertIn('b unit timeouts check=900s test=1200s', log)
        self.assertIn('c unit timeouts check=900s test=2400s', log)
    def test_history_snapshot_tracks_directory_contents_repo_and_phase(self):
        dirs = [self.root / 'hist1', self.root / 'hist2']
        for d in dirs:
            d.mkdir()
        def write(d, rows):
            (d / 'results.jsonl').write_text('\n'.join(json.dumps(row) for row in rows) + '\n')
        def row(repo, phase, wall):
            return {'repo': repo, 'phase': phase, 'exit': 0, 'wall_s': wall}
        write(dirs[0], [row('a', 'check', 200), row('b', 'test', 300)])
        write(dirs[1], [row('a', 'test', 400)])
        def run(d):
            result = subprocess.run([sys.executable, str(HERE / 'run3.py'), '--bin', str(self.bin), '--label', 'L',
                                     '--clone', str(self.clone), '--out', str(self.root / 'out'), '--procs', '2',
                                     '--unit-timeout-history', str(d)], capture_output=True, text=True, timeout=60)
            self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
            return result.stdout
        first = run(dirs[0])
        self.assertIn('a unit timeouts check=600s test=2400s', first)
        self.assertIn('b unit timeouts check=900s test=900s', first)
        second = run(dirs[1])
        self.assertIn('a unit timeouts check=900s test=1200s', second)
        write(dirs[1], [row('a', 'test', 500)])
        third = run(dirs[1])
        self.assertIn('a unit timeouts check=900s test=1500s', third)

    def test_non_train_timeout_matches_main_byte_for_byte(self):
        import shutil
        main = self.root / 'main.py'
        main.write_bytes(subprocess.check_output(['git', 'show', 'origin/main:advisor/gates/run3.py'], cwd=HERE))
        harness = self.root / 'harness.py'
        harness.write_text("""import importlib.util, subprocess, sys
spec = importlib.util.spec_from_file_location('runner', sys.argv[1])
m = importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
def guarded(cmd, env, timeout, stdout, stderr, log):
    subprocess.run(cmd, env=env, stdout=stdout, stderr=stderr, check=True)
    return ('timeout' if cmd[1] == 'test' else 0), 1.0, 0.0
m.guarded = guarded
m.peak_rss = lambda path: None
m.time.strftime = lambda *args: 'fixed'
sys.argv = [sys.argv[1]] + sys.argv[2:]
m.main()
""")
        env = dict(os.environ, PYTHONPATH=str(HERE))
        cmd = [sys.executable, str(harness), '', '--bin', str(self.bin), '--label', 'L',
               '--clone', str(self.clone), '--out', str(self.root / 'out'), '--no-overlap', '--procs', '2']
        def run(script):
            cmd[2] = str(script)
            result = subprocess.run(cmd, env=env, capture_output=True)
            files = {str(p.relative_to(self.root / 'out')): p.read_bytes()
                     for p in (self.root / 'out').rglob('*') if p.is_file()}
            shutil.rmtree(self.root / 'out')
            return result.returncode, result.stdout, result.stderr, files
        self.assertEqual(run(main), run(HERE / 'run3.py'))

    def test_timeout_propagates_through_lane_and_report_including_shards(self):
        import shutil
        w = self.root / 'w'
        w.mkdir()
        for name in ('plan_lanes.py', 'run3.py', 'incremental.py', 'corpus_cfg.py',
                     'check_equivalence.py', 'merge_shards.py', 'val2.sh'):
            shutil.copy(HERE / name, w / name)
        (w / 'lanes').mkdir()
        cfg = w / 'corpus-config.json'
        cfg.write_text('{}')
        (w / 'corpus-config.json.sha256').write_text(hashlib.sha256(cfg.read_bytes()).hexdigest())
        self.bin.write_text(STUB.replace("elif a[0] == 'test':", "elif a[0] == 'test':\n    import time; time.sleep(3)"))
        # Existing history provides a one-second test timeout through the train flags.
        hist = w / 'out/BUCKET2-H1'
        hist.mkdir(parents=True)
        (hist / 'results.jsonl').write_text('\n'.join(json.dumps({'repo': r, 'phase': 'test', 'exit': 0, 'wall_s': 1}) for r in 'ab') + '\n')
        (hist / 'b.test.stdout').write_text('{}')
        (w / 'diff2.py').write_text('print("new 0")\n')
        env = dict(os.environ, W=str(w), RUNROOT=str(self.root / 'run'), CFG='1',
                   PLAN_ARGS='--train --only a,b --set BUCKET2=%s --shard3 a' % self.clone)
        # The lane script uses the default floor; override the history-derived timeout via a runner wrapper.
        runner = w / 'run3-real.py'
        (w / 'run3.py').rename(runner)
        (w / 'run3.py').write_text("import sys, runpy\nsys.argv += ['--unit-timeout-min', '1', '--unit-timeout-factor', '1']\nrunpy.run_path(%r, run_name='__main__')\n" % str(runner))
        result = subprocess.run(['bash', str(w / 'val2.sh'), str(self.bin), 'L', '--lanes', '4', '--hist', 'H1'],
                                env=env, capture_output=True, text=True, timeout=90)
        self.assertNotEqual(result.returncode, 0, (w / 'val2-L.log').read_text())
        rows = [json.loads(x) for x in (w / 'out/BUCKET2-L/results.jsonl').read_text().splitlines()]
        self.assertEqual(sorted(x['part'] for x in rows if x['repo'] == 'a' and x['phase'] == 'test' and x['exit'] == 'timeout'),
                         ['s0of3', 's1of3', 's2of3'])
        self.assertTrue(any(x['repo'] == 'b' and x['phase'] == 'test' and x['exit'] == 'timeout' for x in rows))
        self.assertIn('BUCKET2 b new 0', (w / 'out/DIFF2-L.txt').read_text())
        for lane in range(4):
            self.assertIn('rc=1', (w / ('lanes/L-lane%d.log' % lane)).read_text())



class ScopeAccountingTest(unittest.TestCase):
    """train-ran.json is derived from finished result rows, never from the plan."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.d = Path(self.tmp.name)

    def account(self, planned, rows, check_plan=None, not_planned=()):
        (self.d / 'train-scope.json').write_text(json.dumps({'planned': planned, 'not_planned': list(not_planned)}))
        (self.d / 'check-plan.json').write_text(json.dumps(planned if check_plan is None else check_plan))
        if rows is not None:
            (self.d / 'results.jsonl').write_text('\n'.join(json.dumps(x) for x in rows) + '\n')
        return incremental.scope_report(self.d)

    def row(self, repo, phase, part=None, exit=0):
        return {'repo': repo, 'phase': phase, 'exit': exit, 'wall_s': 1, 'part': part, 'summary': {}}

    def test_failed_startup_ran_nothing(self):
        rep = self.account(['a', 'b'], None)
        self.assertEqual(rep['ran'], [])
        self.assertEqual(rep['not_run'], ['a', 'b'])

    def test_interruption_counts_only_finished_repos(self):
        rep = self.account(['a', 'b', 'c'], [self.row('a', 'check'), self.row('a', 'test'), self.row('b', 'check')])
        self.assertEqual(rep['ran'], ['a'])
        self.assertEqual(rep['not_run'], ['b', 'c'])

    def test_timeout_is_not_ran(self):
        rep = self.account(['a'], [self.row('a', 'check'), self.row('a', 'test', exit='timeout')])
        self.assertEqual(rep['ran'], [])
        rep = self.account(['a'], [self.row('a', 'check', exit='timeout'), self.row('a', 'test')])
        self.assertEqual(rep['ran'], [])

    def test_crash_without_parsed_result_is_not_ran(self):
        crash = dict(self.row('a', 'test', exit=2), summary=None)
        self.assertEqual(self.account(['a'], [self.row('a', 'check'), crash])['ran'], [])
        crash = dict(self.row('a', 'check', exit=2), summary=None)
        self.assertEqual(self.account(['a'], [crash, self.row('a', 'test')])['ran'], [])

    def test_failed_test_exit_still_ran(self):
        rep = self.account(['a'], [self.row('a', 'check'), self.row('a', 'test', exit=1)])
        self.assertEqual(rep['ran'], ['a'])

    def test_shards_need_every_part(self):
        rows = [self.row('k', 'check', 's0of3'), self.row('k', 'test', 's0of3'), self.row('k', 'test', 's1of3')]
        rep = self.account(['k'], [dict(r, part=r['part'] if r['phase'] == 'test' else None) for r in rows])
        self.assertEqual(rep['ran'], [])
        rows.append(self.row('k', 'test', 's2of3'))
        rep = self.account(['k'], [dict(r, part=r['part'] if r['phase'] == 'test' else None) for r in rows])
        self.assertEqual(rep['ran'], ['k'])

    def test_include_exclude_parts_need_both(self):
        base = [self.row('p', 'check'), self.row('p', 'test', 'exc')]
        self.assertEqual(self.account(['p'], base)['ran'], [])
        self.assertEqual(self.account(['p'], base + [self.row('p', 'test', 'inc')])['ran'], ['p'])

    def test_cli_writes_receipt_and_keeps_plan(self):
        self.account(['a'], [self.row('a', 'check'), self.row('a', 'test')], not_planned=['z'])
        r = subprocess.run([sys.executable, str(HERE / 'incremental.py'), 'scope', str(self.d)], capture_output=True, text=True)
        self.assertEqual(r.returncode, 0, r.stderr)
        rep = json.loads((self.d / 'train-ran.json').read_text())
        self.assertEqual((rep['ran'], rep['not_run'], rep['not_planned']), (['a'], ['z'], ['z']))
        self.assertIn('ran: a', r.stdout)
        self.assertIn('not_run: z', r.stdout)
        self.assertEqual(json.loads((self.d / 'train-scope.json').read_text())['planned'], ['a'])


class Val2ReportTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(os.path.realpath(self.tmp.name))

    def val2(self, w, script, plan_args='', runroot=None, label='NEW'):
        env = dict(os.environ, W=str(w), CFG='1', PLAN_ARGS=plan_args, RUNROOT=str(runroot or self.root / 'rr'))
        env.pop('GATE_DEBUG', None)
        r = subprocess.run(['bash', str(script), '%s' % self.bin, label, '--lanes', '2', '--hist', 'H1'],
                           env=env, capture_output=True, text=True, timeout=300)
        log = (Path(w) / ('val2-%s.log' % label)).read_text()
        return r, re.sub(r'\d{4}-\d\d-\d\dT[\d:]+Z', '<TS>', log).replace(str(w), '<W>').replace(str(self.root), '<T>')

    def stub_world(self, name):
        """A work dir with stub planner, merge and diff helpers: lanes only print and exit 0."""
        w = self.root / name
        (w / 'out').mkdir(parents=True)
        (w / 'plan_lanes.py').write_text(
            "import sys, pathlib\n"
            "a = sys.argv; L = a[a.index('--label') + 1]; n = int(a[a.index('--lanes') + 1]); d = pathlib.Path(a[a.index('--outdir') + 1])\n"
            "d.mkdir(parents=True, exist_ok=True)\n"
            "[(d / ('%s-lane%d.sh' % (L, k))).write_text('echo lane%d\\nexit 0\\n' % k) for k in range(n)]\n"
            "print('planned', ' '.join(a[1:]))\n")
        (w / 'merge_shards.py').write_text('print("merge", __import__("sys").argv[1:])\n')
        (w / 'diff2.py').write_text('')
        return w

    bin = '/B/glade'

    def test_non_train_output_matches_main(self):
        for plan_args in ('', '--only a,b --exclude x'):
            with self.subTest(plan_args=plan_args):
                wm, wn = self.stub_world('main' + str(len(plan_args))), self.stub_world('new' + str(len(plan_args)))
                rm, lm = self.val2(wm, HERE / 'testdata' / 'val2_main.sh', plan_args)
                rn, ln = self.val2(wn, HERE / 'val2.sh', plan_args)
                self.assertEqual((rn.returncode, rn.stdout, rn.stderr), (rm.returncode, rm.stdout, rm.stderr))
                self.assertEqual(ln.replace('new' + str(len(plan_args)), 'main' + str(len(plan_args))), lm)
                self.assertNotIn('train scope', ln)
                self.assertTrue(ln.rstrip().endswith('done <TS>'))

    def real_world(self, runroot=None):
        src, w, cfg = make_world(self.root)
        (w / 'out' / 'SET-H1').rename(w / 'out' / 'BROAD-H1')
        for f in HERE.glob('*.py'):
            (w / f.name).write_text(f.read_text())
        (w / 'val2.sh').write_text((HERE / 'val2.sh').read_text())
        (w / 'diff2.py').write_text('')
        b = self.root / 'glade'
        b.write_text(STUB)
        b.chmod(0o755)
        self.bin = str(b)
        return src, w

    def test_train_gate_reports_actual_coverage(self):
        src, w = self.real_world()
        r, log = self.val2(w, w / 'val2.sh', '--train --only a,b --set BROAD=%s --shard3 ""' % src)
        self.assertEqual(r.returncode, 0, log)
        lines = log.rstrip().splitlines()
        self.assertTrue(lines[-1].startswith('done'))
        tail = lines[-5:-1]
        self.assertEqual(tail[0], 'train scope BROAD-NEW:')
        self.assertTrue(tail[1].startswith('ran=2 not_run=1'), tail)
        self.assertEqual(tail[2], 'ran: a b')
        self.assertEqual(tail[3], 'not_run: c')
        self.assertEqual(json.loads((w / 'out/BROAD-NEW/train-ran.json').read_text())['not_run'], ['c'])

    def test_train_gate_with_failed_lane_start_reports_nothing_ran_and_fails(self):
        src, w = self.real_world()
        blocker = self.root / 'blocker'
        blocker.write_text('not a directory')
        r, log = self.val2(w, w / 'val2.sh', '--train --only a,b --set BROAD=%s --shard3 ""' % src, runroot=blocker / 'rr')
        self.assertNotEqual(r.returncode, 0, log)
        self.assertIn('ran=0 not_run=3', log)
        self.assertIn('ran: \n', log)
        self.assertIn('not_run: a b c', log)
        self.assertTrue(log.rstrip().splitlines()[-1].startswith('done'))


    def test_train_flag_detected_through_any_whitespace(self):
        for sep in ('\t', '   ', ' \t '):
            with self.subTest(sep=repr(sep)):
                self.tmp2 = tempfile.TemporaryDirectory()
                self.addCleanup(self.tmp2.cleanup)
                self.root = Path(os.path.realpath(self.tmp2.name))
                src, w = self.real_world()
                r, log = self.val2(w, w / 'val2.sh', '--train%s--only a,b --set BROAD=%s --shard3 ""' % (sep, src))
                self.assertEqual(r.returncode, 0, log)
                self.assertIn('train scope BROAD-NEW:', log)
                self.assertTrue((w / 'out/BROAD-NEW/train-ran.json').exists())

    def test_interrupted_train_gate_reports_partial_work(self):
        import signal
        import time
        src, w = self.real_world()
        hang = self.root / 'glade-hang'
        hang.write_text(STUB.replace("a = sys.argv[1:]\n", "a = sys.argv[1:]\nimport time\nif a[0] == 'test' and a[a.index('--project') + 1].endswith('/b'):\n    time.sleep(120)\n", 1))
        hang.chmod(0o755)
        self.bin = str(hang)
        env = dict(os.environ, W=str(w), CFG='1', RUNROOT=str(self.root / 'rr'),
                   PLAN_ARGS='--train --only a,b --set BROAD=%s --shard3 ""' % src)
        proc = subprocess.Popen(['bash', str(w / 'val2.sh'), self.bin, 'NEW', '--lanes', '2', '--hist', 'H1'], env=env,
                                start_new_session=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        results = w / 'out/BROAD-NEW/results.jsonl'
        deadline = time.time() + 60
        while time.time() < deadline:
            rows = results.read_text() if results.exists() else ''
            if '"repo": "a", "phase": "test"' in rows and '"repo": "b", "phase": "check"' in rows:
                break
            time.sleep(0.2)
        else:
            proc.kill()
            self.fail('lanes never reached partial state')
        time.sleep(1)
        proc.send_signal(signal.SIGTERM)
        self.assertEqual(proc.wait(timeout=60), 143)
        log = (w / 'val2-NEW.log').read_text()
        self.assertIn('interrupted by TERM', log)
        lines = log.rstrip().splitlines()
        self.assertTrue(lines[-1].startswith('done'))
        self.assertEqual(lines[-5], 'train scope BROAD-NEW:')
        self.assertTrue(lines[-4].startswith('ran=1 not_run=2'), lines[-5:])
        self.assertIn('ran: a', log)
        self.assertIn('not_run: b c', log)
        self.assertEqual(json.loads((w / 'out/BROAD-NEW/train-ran.json').read_text())['ran'], ['a'])
        left = subprocess.run(['pgrep', '-f', str(hang)], capture_output=True, text=True).stdout.split()
        self.assertEqual(left, [], 'lane processes survived the interrupt')


class InstallTest(unittest.TestCase):
    def test_installs_incremental_beside_run3_and_pins_config(self):
        text = (HERE / 'gate-install.sh').read_text()
        lines = [x for x in text.splitlines() if 'inst ' in x and 'run3.py' in x and not x.lstrip().startswith('#')]
        self.assertEqual(len(lines), 4)
        for line in lines:
            self.assertIn('incremental.py', line)
        self.assertIn('corpus-config.json.sha256', text)
        self.assertTrue((HERE / 'incremental.py').exists())
    def test_clean_checkout_fails_clearly_naming_every_missing_input(self):
        import shutil
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            checkout = root / 'checkout'
            shutil.copytree(HERE, checkout)
            (root / 'empty').mkdir()
            log = root / 'calls'
            transport = root / 'bin'
            transport.mkdir()
            for cmd in ('scp', 'ssh'):
                (transport / cmd).write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$CALLS"\n')
                (transport / cmd).chmod(0o755)
            env = dict(os.environ, PATH=str(transport) + os.pathsep + os.environ['PATH'],
                       GLADE_GATE_INPUTS=str(root / 'empty'), CALLS=str(log))
            r = subprocess.run(['bash', str(checkout / 'gate-install.sh'), 'casper', 'razor', 'bmo'], env=env,
                               capture_output=True, text=True)
            self.assertEqual(r.returncode, 1)
            for name in ('corpus-config.json', 'mkhist.py', 'diff2.py', 'order_repos.py', 'accepted.py', 'accepted-ledger.json'):
                self.assertIn(name, r.stderr)
            self.assertIn('nothing was installed', r.stderr)
            self.assertFalse(log.exists())

    def test_installer_preflights_and_installs_all_inputs(self):
        import shutil
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            checkout = root / 'checkout'
            shutil.copytree(HERE, checkout)
            inputs = root / 'inputs'
            inputs.mkdir()
            missing = ('corpus-config.json', 'mkhist.py', 'diff2.py', 'order_repos.py', 'accepted.py', 'accepted-ledger.json')
            for name in missing:
                (inputs / name).write_text('{}' if name.endswith('.json') else '# synthetic\n')
            transport = root / 'bin'
            transport.mkdir()
            log = root / 'calls'
            for cmd in ('scp', 'ssh'):
                p = transport / cmd
                p.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$CALLS"\n')
                p.chmod(0o755)
            env = dict(os.environ, PATH=str(transport) + os.pathsep + os.environ['PATH'],
                       GLADE_GATE_INPUTS=str(inputs), CALLS=str(log))
            (inputs / 'diff2.py').unlink()
            failed = subprocess.run(['bash', str(checkout / 'gate-install.sh'), 'casper'], env=env, capture_output=True, text=True)
            self.assertNotEqual(failed.returncode, 0)
            self.assertFalse(log.exists())
            (inputs / 'diff2.py').write_text('# synthetic\n')
            ok = subprocess.run(['bash', str(checkout / 'gate-install.sh'), 'casper', 'razor', 'bmo'], env=env, capture_output=True, text=True)
            self.assertEqual(ok.returncode, 0, ok.stderr + ok.stdout)
            calls = log.read_text()
            for name in missing:
                self.assertIn(name, calls)
            self.assertIn('corpus-config.json.sha256', calls)
            self.assertEqual(calls.count(hashlib.sha256((inputs / 'corpus-config.json').read_bytes()).hexdigest()), 3)
            # Failure of the final remote command must be visible to the caller.
            (transport / 'ssh').write_text('#!/bin/sh\nexit 1\n')
            failed = subprocess.run(['bash', str(checkout / 'gate-install.sh'), 'razor'], env=env, capture_output=True, text=True)
            self.assertNotEqual(failed.returncode, 0)



if __name__ == '__main__':
    unittest.main()
