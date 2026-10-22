"""Benchmark-bearing Go packages must retain the test-only evidence contract."""
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

SCRIPT = Path(__file__).with_name('salesforce-product-parallel.py').resolve()
SPEC = importlib.util.spec_from_file_location('parallel_product', SCRIPT)
PARALLEL = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PARALLEL)


class BenchmarkDiscoveryRoutes(unittest.TestCase):
    def test_prepare_unit_apex_lwc_visualforce_benchmark_route_twins(self):
        for kind, (suffix, _) in PARALLEL.KINDS.items():
            with self.subTest(route=kind), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                package = PARALLEL.PREFIX + suffix
                selection = {'version': 1, 'package': package, 'historyUsed': False,
                             'shards': [{'index': 0, 'tests': ['TestOwned'],
                                         'estimatedDurationMillis': 0, 'regex': '^(?:TestOwned)$'}]}

                def capture(_root, *args):
                    return package + '\n' if args[0] == 'list' else json.dumps(selection)

                def compile_binary(args, **_kwargs):
                    Path(args[args.index('-o') + 1]).write_bytes(b'compiled binary')

                def list_binary(args, **_kwargs):
                    pattern = args[args.index('-test.list') + 1]
                    return ''.join(name + '\n' for name in ['TestOwned', 'BenchmarkOwned']
                                   if re.search(pattern, name))

                with patch.object(PARALLEL, 'KINDS', {kind: (suffix, 1)}), \
                     patch.object(PARALLEL, 'identity', return_value={}), \
                     patch.object(PARALLEL, 'capture', side_effect=capture), \
                     patch.object(PARALLEL.subprocess, 'run', side_effect=compile_binary), \
                     patch.object(PARALLEL.subprocess, 'check_output', side_effect=list_binary), \
                     patch('builtins.print'):
                    PARALLEL.prepare(root, root / 'plan')
                self.assertEqual((root / 'plan' / f'{kind}-discovery-raw.txt').read_text(), 'TestOwned\n')
                plan = json.loads((root / 'plan/parallel-plan.json').read_text())
                self.assertEqual(plan['shards'][0]['selection']['tests'], ['TestOwned'])

    def test_real_go_project_prepare_workers_aggregate_and_serial_benchmark_routes(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            glade = root / 'glade'
            glade.mkdir()
            (glade / 'go.mod').write_text('module github.com/glade-sh/glade\n\ngo 1.26.10\n')
            # Use the candidate's actual planner with owned, synthetic Go packages.
            module = subprocess.check_output(
                ['go', 'list', '-m', '-f', '{{.Dir}}', 'github.com/glade-sh/glade'],
                cwd=SCRIPT.parent.parent, text=True).strip()
            planner = glade / 'scripts/internal/cishard'
            planner.mkdir(parents=True)
            shutil.copyfile(Path(module) / 'scripts/internal/cishard/main.go', planner / 'main.go')
            for suffix, _ in PARALLEL.KINDS.values():
                package = glade / suffix
                package.mkdir(parents=True)
                source = 'package owned\nimport "testing"\n'
                source += ''.join(f'func TestOwned{i:02d}(t *testing.T) {{}}\n' for i in range(16))
                source += 'func BenchmarkOwned(b *testing.B) { panic("benchmark must not execute") }\n'
                (package / 'owned_test.go').write_text(source)
            subprocess.run(['git', 'init', '-q', str(glade)], check=True)
            subprocess.run(['git', '-C', str(glade), 'add', '.'], check=True)
            subprocess.run(['git', '-C', str(glade), '-c', 'user.name=Test', '-c',
                            'user.email=test@example.com', 'commit', '-qm', 'owned project'], check=True)
            env = dict(os.environ, LC_ALL='C', GLADE_ROOT=str(glade), GLADE_LWC_COMPILE='1',
                       GITHUB_RUN_ID='123', GITHUB_RUN_ATTEMPT='1')
            for key in ['GLADE_SHA', 'GITHUB_SHA', 'SALESFORCE_PRODUCT_SHARDS']:
                env.pop(key, None)
            wrapper = SCRIPT.with_name('salesforce-product-tests.sh')

            def run(*args, extra_env=None):
                result = subprocess.run(['bash', str(wrapper), *map(str, args)],
                                        env={**env, **(extra_env or {})}, text=True,
                                        stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                return result.stdout

            plan_dir = root / 'salesforce-product-plan'
            matrix = json.loads(run('prepare', glade, plan_dir))
            plan = json.loads((plan_dir / 'parallel-plan.json').read_text())
            self.assertEqual(len(matrix['include']), len(plan['shards']))
            for kind in PARALLEL.KINDS:
                with self.subTest(route=kind):
                    binary = plan_dir / f'{kind}.test'
                    listed = subprocess.check_output([str(binary), '-test.list', '.'], text=True)
                    self.assertIn('BenchmarkOwned\n', listed)
                    expected = [f'TestOwned{i:02d}' for i in range(16)]
                    self.assertEqual((plan_dir / f'{kind}-discovery.txt').read_text().splitlines(), expected)
                    self.assertEqual(sorted((plan_dir / f'{kind}-discovery-raw.txt').read_text().splitlines()), expected)
            results = root / 'results'
            results.mkdir()
            for shard in plan['shards']:
                run('shard', glade, plan_dir, shard['id'], results / shard['id'])
            run('aggregate', glade, plan_dir, results, root / 'aggregate')
            # Also consume the same evidence through the gate's downloaded-artifact route.
            downloaded = root / 'downloaded'
            downloaded.mkdir()
            shutil.copytree(plan_dir, downloaded / 'salesforce-product-plan')
            for shard in plan['shards']:
                shutil.copytree(results / shard['id'], downloaded / ('salesforce-product-' + shard['id']))
            run(glade, root / 'gate', extra_env={'SALESFORCE_PRODUCT_SHARDS': str(downloaded)})
            run(glade, root / 'serial')
            for route in ['aggregate', 'gate', 'serial']:
                with self.subTest(route=route):
                    evidence = root / route / 'product-test-evidence'
                    summary = json.loads((evidence / 'validation.json').read_text())
                    self.assertEqual(summary['status'], 'pass')
                    self.assertEqual(summary['union']['count'], 16)
                    events = (root / route / 'product-tests.jsonl').read_text()
                    self.assertNotIn('BenchmarkOwned', events)


if __name__ == '__main__':
    unittest.main()
