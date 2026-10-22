"""Synthetic CLI contracts for planning, worker execution, and aggregation."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('salesforce-product-tests.sh').resolve()
PREFIX = 'github.com/glade-sh/glade/'

FAKE_GO = r'''#!/usr/bin/env python3
import json, os, pathlib, shutil, sys
args = sys.argv[1:]
if args[:1] == ['-C']: args = args[2:]
prefix = 'github.com/glade-sh/glade/'
if args[0] == 'list':
    print('\n'.join(prefix + p for p in ['cmd/glade', 'internal/sema', 'internal/vm', 'internal/gladecli', 'internal/apextest', 'internal/lwc/compile', 'internal/visualforce']))
elif args[0] == 'test' and '-c' in args:
    output = pathlib.Path(args[args.index('-o') + 1])
    package = args[-1].replace('./', prefix)
    output.write_text('#!/usr/bin/env python3\nPACKAGE=' + repr(package) + '\n' + os.environ['FAKE_BINARY_SOURCE'])
    output.chmod(0o755)
elif args[0] == 'test':
    for p in args:
        if p.startswith(prefix):
            print(json.dumps({'Action':'pass', 'Package':p, 'Test':'TestProduct'}))
            print(json.dumps({'Action':'pass', 'Package':p}))
elif args[0] == 'run':
    package = args[args.index('--package') + 1]
    count = int(args[args.index('--shards') + 1])
    names = pathlib.Path(args[args.index('--tests') + 1]).read_text().splitlines()
    shards = []
    for i in range(count):
        selected = names[i::count]
        shards.append({'index':i, 'tests':selected, 'estimatedDurationMillis':0, 'regex':'^(?:'+'|'.join(selected)+')$'})
    print(json.dumps({'version':1,'package':package,'historyUsed':False,'shards':shards}))
elif args[:2] == ['tool','test2json']:
    sys.stdout.write(sys.stdin.read())
    if os.environ.get('FAIL_CONVERTER'): sys.exit(3)
else: sys.exit('unexpected go args: '+str(args))
'''
FAKE_BINARY = r'''import json, os, pathlib, re, sys
if os.environ.get('CHECK_PACKAGE_CWD'):
    assert pathlib.Path.cwd().as_posix().endswith(PACKAGE.split('/glade/', 1)[1]), 'wrong package working directory'
if '-test.list' in sys.argv:
    print('\n'.join('Test%02d'%i for i in range(16)))
else:
    regex = next(a.split('=',1)[1] for a in sys.argv if a.startswith('-test.run='))
    for i in range(16):
        name = 'Test%02d'%i
        if re.search(regex,name):
            if os.environ.get('SKIP_TEST') == name:
                print(json.dumps({'Action':'output','Package':PACKAGE,'Test':name,'Output':'    x_test.go:9: requires explicitly installed DataWeave toolchain\\n'}))
                print(json.dumps({'Action':'skip','Package':PACKAGE,'Test':name}))
                continue
            print(json.dumps({'Action':'pass','Package':PACKAGE,'Test':name}))
    print(json.dumps({'Action':'pass','Package':PACKAGE}))
'''


class ParallelProductTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.glade = self.root / 'glade'
        (self.glade / 'scripts/internal/cishard').mkdir(parents=True)
        (self.glade / 'scripts/internal/cishard/main.go').write_text('package main\n')
        for suffix in ['internal/apextest','internal/lwc/compile','internal/visualforce']:
            (self.glade / suffix).mkdir(parents=True)
        subprocess.run(['git','init','-q',str(self.glade)],check=True)
        subprocess.run(['git','-C',str(self.glade),'add','.'],check=True)
        subprocess.run(['git','-C',str(self.glade),'-c','user.name=Test','-c','user.email=test@example.com','commit','-qm','initial'],check=True)
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        (self.bin / 'go').write_text(FAKE_GO)
        (self.bin / 'go').chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.bin)+os.pathsep+os.environ['PATH'], LC_ALL='C', GLADE_ROOT=str(self.glade), GLADE_LWC_COMPILE='1', FAKE_BINARY_SOURCE=FAKE_BINARY, GITHUB_RUN_ID='123', GITHUB_RUN_ATTEMPT='1')
        self.plan = self.root / 'plan'
        self.results = self.root / 'results'
        self.results.mkdir()

    def run_script(self, *args, passes=True):
        result = subprocess.run(['bash',str(SCRIPT),*map(str,args)],env=self.env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
        if passes:
            self.assertEqual(result.returncode,0,result.stdout)
        else:
            self.assertNotEqual(result.returncode,0,result.stdout)
        return result

    def execute_workers(self):
        self.run_script('prepare',self.glade,self.plan)
        plan = json.loads((self.plan / 'parallel-plan.json').read_text())
        self.assertEqual(len([s for s in plan['shards'] if s['kind']=='apextest']),16)
        self.assertEqual(len([s for s in plan['shards'] if s['kind']=='lwc-compile']),8)
        self.assertEqual(len([s for s in plan['shards'] if s['kind']=='visualforce']),4)
        owned = [p for s in plan['shards'] if s['kind']=='packages' for p in s['packages']]
        self.assertEqual(len(owned),len(set(owned)))
        self.assertEqual(set(owned),{PREFIX+p for p in ['cmd/glade','internal/sema','internal/vm','internal/gladecli']})
        for shard in plan['shards']:
            self.run_script('shard',self.glade,self.plan,shard['id'],self.results / shard['id'])
        return plan

    def test_parallel_end_to_end_and_legacy_proof_shape(self):
        plan = self.execute_workers()
        out = self.root / 'out'
        self.run_script('aggregate',self.glade,self.plan,self.results,out)
        summary = json.loads((out / 'product-test-evidence/validation.json').read_text())
        self.assertEqual(summary['status'],'pass')
        self.assertEqual(summary['shardCount'],16)
        self.assertEqual(summary['nonApex']['packageCount'],6)
        self.assertEqual(summary['union']['count'],16)
        self.assertEqual(len(summary['artifacts']),54)
        self.assertFalse((out / 'product-test-evidence/apextest.test').exists())
        self.assertEqual(len(plan['shards']),32)


    def commit_deferred_script(self, name):
        (self.glade / 'scripts').mkdir(exist_ok=True)
        (self.glade / 'scripts/ci-go-test.sh').write_text(
            '    (module + "/internal/apextest", "%s"): (("GLADE_DATAWEAVE_APEX_TEST_HOME",), "requires explicitly installed DataWeave toolchain"),\n' % name)
        subprocess.run(['git','-C',str(self.glade),'add','.'],check=True)
        subprocess.run(['git','-C',str(self.glade),'-c','user.name=Test','-c','user.email=test@example.com','commit','-qm','deferred'],check=True)

    def test_end_to_end_accepts_a_deferred_dataweave_skip_through_the_final_validator(self):
        self.commit_deferred_script('Test03')
        self.env['SKIP_TEST'] = 'Test03'
        self.env.pop('GLADE_DATAWEAVE_APEX_TEST_HOME', None)
        self.execute_workers()
        out = self.root / 'out'
        self.run_script('aggregate',self.glade,self.plan,self.results,out)
        self.assertEqual(json.loads((out / 'product-test-evidence/validation.json').read_text())['status'],'pass')

    def test_end_to_end_rejects_an_undeclared_apex_skip(self):
        self.commit_deferred_script('Test04')
        self.env['SKIP_TEST'] = 'Test03'
        self.env.pop('GLADE_DATAWEAVE_APEX_TEST_HOME', None)
        self.run_script('prepare',self.glade,self.plan)
        plan = json.loads((self.plan / 'parallel-plan.json').read_text())
        apex = next(s for s in plan['shards'] if s['kind']=='apextest' and 'Test03' in s['selection']['tests'])
        self.run_script('shard',self.glade,self.plan,apex['id'],self.results / apex['id'],passes=False)

    def test_aggregate_missing_duplicate_failed_or_mismatched_shard_fails_closed(self):
        plan = self.execute_workers()
        first = plan['shards'][0]['id']
        for mutation in ['missing','duplicate','identity','plan','events','status','skip','union','extra-test','subtest-fail','binary']:
            with self.subTest(mutation=mutation):
                results = self.root / ('results-'+mutation)
                shutil.copytree(self.results,results)
                shard = results / first
                receipt_path = shard / 'result.json'
                receipt = json.loads(receipt_path.read_text())
                if mutation == 'missing': shutil.rmtree(shard)
                elif mutation == 'duplicate': shutil.copytree(shard,results / 'duplicate')
                elif mutation == 'events': (shard / 'events.jsonl').write_text('')
                elif mutation == 'binary': (self.plan / 'apextest.test').write_text('tampered')
                elif mutation in ['skip','union','extra-test','subtest-fail']:
                    apex = next(s for s in plan['shards'] if s['kind']=='apextest')
                    shard = results / apex['id']
                    path = shard / 'events.jsonl'
                    events = [json.loads(line) for line in path.read_text().splitlines()]
                    if mutation == 'skip': events[0]['Action']='skip'
                    elif mutation == 'union': events[0]['Test']='TestMissing'
                    elif mutation == 'extra-test': events.append({'Action':'run','Package':PREFIX+'internal/apextest','Test':'TestUnexpected'})
                    else: events.append({'Action':'fail','Package':PREFIX+'internal/apextest','Test':'Test00/child'})
                    path.write_text(''.join(json.dumps(e)+'\n' for e in events))
                    # Rebind transport checksum: semantic validation must still reject it.
                    import hashlib
                    rp = shard / 'result.json'
                    r = json.loads(rp.read_text())
                    r['eventsSHA256']=hashlib.sha256(path.read_bytes()).hexdigest()
                    rp.write_text(json.dumps(r))
                else:
                    receipt[{'identity':'gladeSHA','plan':'planSHA256','status':'status'}[mutation]]='wrong'
                    receipt_path.write_text(json.dumps(receipt))
                out = self.root / ('out-'+mutation)
                self.run_script('aggregate',self.glade,self.plan,results,out,passes=False)
                self.assertFalse((out / 'product-tests.jsonl').exists())

    def test_aggregate_route_twins_reject_incomplete_or_failed_events(self):
        plan = self.execute_workers()
        import hashlib
        for kind in ['packages','apextest','lwc-compile','visualforce']:
            for mutation in ['missing-terminal','duplicate-terminal','failed-subtest','wrong-package']:
                with self.subTest(kind=kind,mutation=mutation):
                    results = self.root / (kind+'-'+mutation)
                    shutil.copytree(self.results,results)
                    selected = next(s for s in plan['shards'] if s['kind']==kind)
                    shard = results / selected['id']
                    path = shard / 'events.jsonl'
                    events = [json.loads(line) for line in path.read_text().splitlines()]
                    if mutation=='missing-terminal': events.pop()
                    elif mutation=='duplicate-terminal': events.append(events[-1])
                    elif mutation=='wrong-package': events[0]['Package']='unexpected/package'
                    else: events.append({'Action':'fail','Package':selected['packages'][0],'Test':'Test00/child'})
                    path.write_text(''.join(json.dumps(e)+'\n' for e in events))
                    receipt = json.loads((shard / 'result.json').read_text())
                    receipt['eventsSHA256']=hashlib.sha256(path.read_bytes()).hexdigest()
                    (shard / 'result.json').write_text(json.dumps(receipt))
                    out = self.root / ('out-'+kind+'-'+mutation)
                    self.run_script('aggregate',self.glade,self.plan,results,out,passes=False)
                    self.assertFalse((out / 'product-tests.jsonl').exists())

    def test_package_cwd_discovery_and_worker_routes(self):
        self.env['CHECK_PACKAGE_CWD']='1'
        plan = self.execute_workers()
        out = self.root / 'downloaded'
        out.mkdir()
        shutil.copytree(self.plan,out / 'salesforce-product-plan')
        for shard in plan['shards']:
            shutil.copytree(self.results / shard['id'],out / ('salesforce-product-'+shard['id']))
        self.env['SALESFORCE_PRODUCT_SHARDS']=str(out)
        destination = self.root / 'gate'
        # The gate's original two-argument route must consume downloaded artifacts.
        self.run_script(self.glade,destination)
        self.assertEqual(json.loads((destination / 'product-test-evidence/validation.json').read_text())['status'],'pass')

    def test_worker_rejects_wrong_checkout_and_converter_failure(self):
        self.run_script('prepare',self.glade,self.plan)
        plan = json.loads((self.plan / 'parallel-plan.json').read_text())
        shard = next(s for s in plan['shards'] if s['kind']=='apextest')
        self.env['FAIL_CONVERTER']='1'
        self.run_script('shard',self.glade,self.plan,shard['id'],self.results / 'converter',passes=False)
        self.assertFalse((self.results / 'converter/result.json').exists())
        self.env.pop('FAIL_CONVERTER')
        self.env['GITHUB_RUN_ATTEMPT']='2'
        self.run_script('shard',self.glade,self.plan,shard['id'],self.results / 'attempt',passes=False)
        self.env['GITHUB_RUN_ATTEMPT']='1'
        (self.glade / 'other').write_text('different candidate')
        subprocess.run(['git','-C',str(self.glade),'add','.'],check=True)
        subprocess.run(['git','-C',str(self.glade),'-c','user.name=Test','-c','user.email=test@example.com','commit','-qm','next'],check=True)
        self.run_script('shard',self.glade,self.plan,shard['id'],self.results / 'identity',passes=False)


class DeferredDataWeaveSkipTests(unittest.TestCase):
    """Glade defers five optional DataWeave Apex tests when no toolchain is installed (D45).
    Only those exact tests, with their exact skip message, may be skipped; every other Apex test must pass."""

    def load(self):
        import importlib.util
        spec = importlib.util.spec_from_file_location('product_parallel', Path(__file__).with_name('salesforce-product-parallel.py'))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def events(self, directory, tests):
        package = PREFIX + 'internal/apextest'
        lines = []
        for name, action, message in tests:
            lines.append({'Action': 'run', 'Package': package, 'Test': name})
            if message:
                lines.append({'Action': 'output', 'Package': package, 'Test': name, 'Output': message})
            lines.append({'Action': action, 'Package': package, 'Test': name})
        lines.append({'Action': 'pass', 'Package': package})
        path = Path(directory) / 'events.jsonl'
        path.write_text(''.join(json.dumps(e) + '\n' for e in lines))
        return path, package

    FIVE = frozenset({'TestRunDataWeaveSourceSourceNames', 'TestRunDataWeaveSourceChangedSource', 'TestRunDataWeaveSourceBuiltinImports',
                      'TestRunDataWeaveSourceRestrictedWordsInLiterals', 'TestRunDataWeaveSourceLocalFunctionShadow'})

    def check(self, tests, selected, deferred=None):
        module = self.load()
        with tempfile.TemporaryDirectory() as directory:
            path, package = self.events(directory, tests)
            shard = {'kind': 'apextest', 'packages': [package], 'selection': {'tests': selected}}
            try:
                module.validate_events(path, shard, self.FIVE if deferred is None else deferred)
            except BaseException as error:
                return error
        return None

    def test_deferred_list_comes_from_the_candidate_ci_script(self):
        module = self.load()
        entry = '    (module + "/internal/apextest", "%s"): (("GLADE_DATAWEAVE_APEX_TEST_HOME",), "%s"),\n'
        script = (entry % ('TestRunC5BulkTypedProof', 'requires explicitly installed DataWeave toolchain')
                  + entry % ('TestRunDataWeaveTypedAdmitted', 'requires explicitly installed DataWeave toolchain')
                  + entry % ('TestOtherReason', 'something else')
                  + '    (module + "/internal/dataweave", "TestOfficialEngineSourceAndDenials"): (("X",), "requires explicitly installed DataWeave toolchain"),\n')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'scripts').mkdir()
            (root / 'scripts' / 'ci-go-test.sh').write_text(script)
            saved = os.environ.pop('GLADE_DATAWEAVE_APEX_TEST_HOME', None)
            try:
                self.assertEqual(module.load_deferred(root), frozenset({'TestRunC5BulkTypedProof', 'TestRunDataWeaveTypedAdmitted'}))
                os.environ['GLADE_DATAWEAVE_APEX_TEST_HOME'] = '/configured/toolchain'
                self.assertEqual(module.load_deferred(root), frozenset())
            finally:
                os.environ.pop('GLADE_DATAWEAVE_APEX_TEST_HOME', None)
                if saved is not None: os.environ['GLADE_DATAWEAVE_APEX_TEST_HOME'] = saved
            (root / 'scripts' / 'ci-go-test.sh').unlink()
            self.assertEqual(module.load_deferred(root), frozenset())

    def test_listed_dataweave_skip_with_its_message_is_accepted(self):
        message = '    dataweave_source_proof_test.go:95: requires explicitly installed DataWeave toolchain\n'
        names = ['TestRunDataWeaveSourceSourceNames', 'TestRunDataWeaveSourceChangedSource', 'TestRunDataWeaveSourceBuiltinImports',
                 'TestRunDataWeaveSourceRestrictedWordsInLiterals', 'TestRunDataWeaveSourceLocalFunctionShadow']
        tests = [(name, 'skip', message) for name in names] + [('TestOrdinary', 'pass', '')]
        self.assertIsNone(self.check(tests, names + ['TestOrdinary']))

    def test_listed_test_skipping_for_another_reason_is_rejected(self):
        tests = [('TestRunDataWeaveSourceLocalFunctionShadow', 'skip', '    x_test.go:1: some other reason\n')]
        self.assertIsNotNone(self.check(tests, ['TestRunDataWeaveSourceLocalFunctionShadow']))

    def test_listed_test_skipping_without_output_is_rejected(self):
        tests = [('TestRunDataWeaveSourceLocalFunctionShadow', 'skip', '')]
        self.assertIsNotNone(self.check(tests, ['TestRunDataWeaveSourceLocalFunctionShadow']))

    def test_unlisted_apex_test_cannot_use_the_dataweave_message(self):
        message = '    y_test.go:3: requires explicitly installed DataWeave toolchain\n'
        tests = [('TestSomethingElse', 'skip', message)]
        self.assertIsNotNone(self.check(tests, ['TestSomethingElse']))


if __name__ == '__main__':
    unittest.main()
