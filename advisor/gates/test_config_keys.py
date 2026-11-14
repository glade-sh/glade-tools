#!/usr/bin/env python3
"""A present corpus-config.json must carry shard3 / b3Repos / splitRepos; a missing key fails loudly, [] opts out."""
import json, os, shutil, subprocess, sys, tempfile, unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent


def run(script, cfg, *args, env=None):
    w = Path(tempfile.mkdtemp())
    try:
        for f in ('plan_lanes.py', 'b3-2.sh', 'pubgate2.sh', 'check_plan.py', 'merge_shards.py'):
            shutil.copy(HERE / f, w / f)
        if cfg is not None:
            (w / 'corpus-config.json').write_text(json.dumps(cfg))
        cmd = [sys.executable, str(w / script)] if script.endswith('.py') else ['bash', str(w / script)]
        return subprocess.run(cmd + list(args) + ([] if script.endswith('.py') else []), capture_output=True, text=True, cwd=w, timeout=60, env=dict(os.environ, **(env or {}))), w
    finally:
        pass


def plan(cfg, *extra, env=None):
    w = Path(tempfile.mkdtemp()); (w / 'src' / 'r1').mkdir(parents=True); (w / 'out').mkdir()
    shutil.copy(HERE / 'plan_lanes.py', w / 'plan_lanes.py')
    if cfg is not None:
        (w / 'corpus-config.json').write_text(json.dumps(cfg))
    r = subprocess.run([sys.executable, str(w / 'plan_lanes.py'), '--lanes', '3', '--label', 'X', '--bin', 'b', '--hist', 'H',
                        '--w', str(w), '--out', str(w / 'out'), '--outdir', str(w / 'l'), '--set', 'S=%s' % (w / 'src'), '--dry'] + list(extra),
                       capture_output=True, text=True, timeout=60, env=dict(os.environ, **(env or {})))
    shutil.rmtree(w); return r


class ConfigKeys(unittest.TestCase):
    def test_plan_lanes_missing_key_fails(self):
        r = plan({})
        self.assertNotEqual(r.returncode, 0); self.assertIn('"shard3"', r.stderr)

    def test_plan_lanes_empty_list_opts_out(self):
        r = plan({'shard3': []})
        self.assertEqual(r.returncode, 0); self.assertNotIn('shard', r.stdout)

    def test_plan_lanes_list_shards(self):
        r = plan({'shard3': ['r1']})
        self.assertEqual(r.returncode, 0); self.assertIn('[shard 0/3]', r.stdout)

    def test_b3_missing_key_fails(self):
        r, w = run('b3-2.sh', {}, 'bin', 'lbl')
        self.assertEqual(r.returncode, 2); self.assertIn('"b3Repos"', r.stderr); shutil.rmtree(w)

    def test_b3_wrong_count_fails(self):
        r, w = run('b3-2.sh', {'b3Repos': ['a']}, 'bin', 'lbl')
        self.assertEqual(r.returncode, 2); self.assertIn('exactly 3', r.stderr); shutil.rmtree(w)

    def test_pubgate_missing_key_fails(self):
        r, w = run('pubgate2.sh', {}, 'bin', 'lbl')
        self.assertEqual(r.returncode, 2); self.assertIn('"splitRepos"', r.stderr); shutil.rmtree(w)

    def test_pubgate_empty_list_passes_key_check(self):
        r, w = run('pubgate2.sh', {'splitRepos': []}, 'bin', 'lbl', env={'HIST': 'h'})
        self.assertNotIn('has no', r.stderr); shutil.rmtree(w)

    def test_pubgate_configured_without_history_fails(self):
        for cfg in ({'splitRepos': ['r1']}, {'splitRepos': []}):
            r, w = run('pubgate2.sh', cfg, 'bin', 'lbl')
            self.assertEqual(r.returncode, 2); self.assertIn('no history found', r.stderr); shutil.rmtree(w)

    def test_plan_lanes_no_file_fails(self):
        r = plan(None)
        self.assertNotEqual(r.returncode, 0); self.assertIn('corpus-config.json', r.stderr)

    def test_plan_lanes_no_file_optout(self):
        self.assertEqual(plan(None, '--no-config').returncode, 0)
        self.assertEqual(plan(None, env={'GATE_NO_CONFIG': '1'}).returncode, 0)

    def test_b3_no_file_fails_and_optout(self):
        r, w = run('b3-2.sh', None, 'bin', 'lbl')
        self.assertEqual(r.returncode, 2); self.assertIn('corpus-config.json', r.stderr); shutil.rmtree(w)
        r, w = run('b3-2.sh', None, 'bin', 'lbl', env={'GATE_NO_CONFIG': '1', 'B3_REPOS': 'a b'})
        self.assertEqual(r.returncode, 2); self.assertIn('exactly 3', r.stderr); shutil.rmtree(w)

    def test_pubgate_no_file_fails_and_optout(self):
        r, w = run('pubgate2.sh', None, 'bin', 'lbl')
        self.assertEqual(r.returncode, 2); self.assertIn('corpus-config.json', r.stderr); shutil.rmtree(w)
        r, w = run('pubgate2.sh', None, 'bin', 'lbl', env={'GATE_NO_CONFIG': '1'})
        self.assertNotIn('not found', r.stderr); shutil.rmtree(w)

    def test_pubgate_planned_path_without_history(self):
        # GATE_NO_CONFIG=1, no HIST: pubgate2.sh -> plan_lanes (no --hist) -> lane script -> runner -> merge.
        w = Path(tempfile.mkdtemp())
        try:
            for f in ('plan_lanes.py', 'pubgate2.sh', 'merge_shards.py', 'check_plan.py'):
                shutil.copy(HERE / f, w / f)
            (w / 'clone-curated' / 'r1').mkdir(parents=True)
            (w / 'run2.py').write_text('import sys\nopen(%r, "w").write(" ".join(sys.argv))\n' % str(w / 'runner.args'))
            env = dict(os.environ, GATE_NO_CONFIG='1', LANES='1')
            env.pop('HIST', None)
            r = subprocess.run(['bash', str(w / 'pubgate2.sh'), 'bin', 'lbl'], capture_output=True, text=True, cwd=w, timeout=60, env=env)
            self.assertEqual(r.returncode, 0, (r.stdout, r.stderr, (w / 'pub2-lbl.log').read_text() if (w / 'pub2-lbl.log').exists() else ''))
            args = (w / 'runner.args').read_text()
            self.assertIn('--repos r1', args); self.assertNotIn('--history-from', args)
        finally:
            shutil.rmtree(w)


if __name__ == '__main__':
    unittest.main()
