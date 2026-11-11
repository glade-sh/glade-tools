import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from corpus_manager.server import App
from corpus_manager import ops


class UpdateWorkflow(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        with patch('corpus_manager.ops.GitHub'):
            self.app = App(self.temp.name)
        self.addCleanup(self.app.store.db.close)
        self.addCleanup(self.app.executor.shutdown, wait=True)
        self.app.store.put('hosts', {'id':'local', 'ssh':'', 'roots':[], 'managed_root':str(Path(self.temp.name)/'managed')})
        self.repo = self.app.ops.add('example/alpha')
        self.repo.update(locations=[{'id':'old', 'path':'/existing/alpha', 'host':'local', 'head':'a'*40}], tracked_location_id='old', upstream={'status':'updates', 'sha':'b'*40, 'base_sha':'a'*40})
        self.app.store.put('repositories', self.repo)

    def test_pull_preserves_tracked_checkout_and_upstream_comparison(self):
        prepared={'path':str(Path(self.temp.name)/'managed'/'new'), 'head':'b'*40, 'dirty':False, 'branch':''}
        with patch.object(self.app.ops, 'check'), patch.object(ops,'host_python',return_value=prepared):
            self.app.ops.snapshot(self.repo['id'],'local')
        saved=self.app.store.get('repositories',self.repo['id'])
        self.assertEqual(saved['tracked_location_id'],'old')
        self.assertEqual(saved['locations'][0],self.repo['locations'][0])
        self.assertTrue(saved['locations'][1]['pulled'])
        self.assertEqual(saved['upstream']['status'],'updates')
        self.assertEqual(saved['upstream']['base_sha'],'a'*40)

    def test_accept_pin_changes_only_pin_and_requires_golden(self):
        import json
        p=Path(self.temp.name)/'tiers.json'
        p.write_text(json.dumps({'schemaVersion':1,'repos':[{'name':'example__alpha','tier':'A','pin':'a'*40,'overlay':{},'blocked':''}]}))
        self.app.set_tiers_file(str(p))
        self.app.accept_pin(self.repo,'b'*40,self.app.tiers_snapshot()['revision'])
        entry=json.loads(p.read_text())['repos'][0]
        self.assertEqual((entry['pin'],entry['tier']),('b'*40,'A'))
        self.assertIn('golden',entry['blocked'])

    def test_compare_only_names_when_both_results_have_names(self):
        result=ops.compare_results({'failed_tests':['A','B'], 'failed':2}, {'failed_tests':['B','C'], 'failed':2})
        self.assertEqual(result['fixed'],['A'])
        self.assertEqual(result['new'],['C'])
        self.assertEqual(ops.compare_results({'failed':2},{'failed_tests':['C'],'failed':1})['mode'],'counts')

    def test_compare_details_classifies_changed_files(self):
        data = {'commits':[{'sha':'c'*40,'commit':{'message':'Change','author':{'date':'2026-01-01'}},'html_url':'https://example.invalid/commit'}],
                'files':[{'filename':'src/Thing.cls'}, {'filename':'lwc/widget/main.js','status':'modified'}, {'filename':'pages/home.page'}, {'filename':'meta/object.object-meta.xml'}, {'filename':'README.md'}]}
        out=ops.compare_details(data)
        self.assertEqual({k:len(v) for k,v in out['files'].items()}, {'Apex':1,'LWC':1,'Visualforce':1,'Metadata':1,'Other':1})
        self.assertTrue(out['touches_code'])
        self.assertFalse(ops.compare_details({'files':[{'filename':'README.md'}]})['touches_code'])

    def test_ambiguous_tier_does_not_create_or_modify_entry(self):
        import json
        other=self.app.ops.add('other/beta')
        self.repo['locations']=[{'id':'x','host':'local','path':'/one/shared'}]
        other['locations']=[{'id':'y','host':'local','path':'/two/shared'}]
        self.app.store.put('repositories',self.repo)
        self.app.store.put('repositories',other)
        p=Path(self.temp.name)/'tiers.json'
        original={'repos':[{'name':'shared','tier':'A','pin':'a'*40}]}
        p.write_text(json.dumps(original))
        self.app.set_tiers_file(str(p))
        with self.assertRaises(ValueError):
            self.app.move_tier(self.repo,'B',self.app.tiers_snapshot()['revision'])
        self.assertEqual(json.loads(p.read_text()),original)

    def test_busy_host_refuses_before_runner(self):
        self.repo['locations'].append({'id':'pulled','host':'local','path':'/scratch/update','head':'b'*40,'pulled':True})
        self.app.store.put('repositories',self.repo)
        with patch.object(self.app.ops, 'host_busy', return_value=True), patch.object(ops, 'host_python') as invoke:
            with self.assertRaisesRegex(ValueError, 'busy'):
                self.app.test_update(self.repo['id'],'pulled')
        invoke.assert_not_called()

    def test_update_runner_records_result_and_preserves_pin(self):
        import json
        p=Path(self.temp.name)/'tiers.json'
        p.write_text(json.dumps({'schemaVersion':1,'depSources':{},'repos':[{'name':'example__alpha','tier':'A','clone':'/work/alpha','pin':'a'*40,'overlay':{},'blocked':''}]}))
        self.app.set_tiers_file(str(p))
        self.app.save_settings({'glade_binary':'/bin/glade','runner_binary':'/bin/glade-tools','glade_sha':'c'*40})
        self.repo['locations'].append({'id':'pulled','host':'local','path':'/scratch/update','head':'b'*40,'pulled':True})
        self.app.store.put('repositories',self.repo)
        result={'observed':{'counts':{'tests':4,'passed':3},'failing':[{'test':'Example.test'}], 'timeouts':[], 'diagnostics':[], 'wallSeconds':2.5}, 'complete':True}
        with patch.object(self.app.ops,'host_busy',return_value=False), patch.object(ops,'host_python',return_value=result) as invoke:
            self.app.test_update(self.repo['id'],'pulled')
        rows=self.app.store.all('runs')
        self.assertEqual(len(rows),1)
        self.assertEqual((rows[0]['repo_sha'],rows[0]['glade_sha'],rows[0]['failed_tests']),('b'*40,'c'*40,['Example.test']))
        self.assertEqual(invoke.call_args.args[2]['repo']['pin'],'b'*40)
        self.assertEqual(json.loads(p.read_text())['repos'][0]['pin'],'a'*40)

    def test_delete_refuses_existing_checkout_and_unowned_path(self):
        with patch.object(ops, 'host_python') as invoke:
            with self.assertRaises(ValueError): self.app.delete_pull(self.repo['id'], 'old')
        invoke.assert_not_called()

    def test_keep_pin_records_dismissal_without_modifying_pin(self):
        self.repo['locations'].append({'id':'pulled','host':'local','path':'/scratch/update','head':'b'*40,'pulled':True})
        self.app.store.put('repositories',self.repo)
        self.app.mutate('/api/repos/'+self.repo['id']+'/keep-pin',{'location_id':'pulled'})
        repo=self.app.store.get('repositories',self.repo['id'])
        self.assertTrue(repo['locations'][1]['dismissed'])
        self.assertEqual(repo['tracked_location_id'],'old')

    def test_cancel_queued_job_does_not_execute(self):
        import threading
        gate=threading.Event()
        first=self.app.job('wait',lambda:gate.wait(3))
        ran=[]
        second=self.app.job('pull',lambda:ran.append(True))
        self.app.cancel_job(second['job'])
        gate.set()
        self.app.executor.shutdown(wait=True)
        self.assertEqual(ran,[])
        self.assertEqual(self.app.store.get('jobs',second['job'])['status'],'cancelled')

    def test_trend_prefers_complete_observation_over_newer_shard(self):
        import subprocess
        source=(Path(__file__).parents[1]/'static'/'app.js').read_text()
        fn=next(x for x in source.splitlines() if x.startswith('function trendRows('))
        js=fn+"\nconst r=trendRows({runs:[{phase:'test',gate_label:'g',status:'partial',started_at:'2026-02'},{phase:'test',gate_label:'g',status:'failed',started_at:'2026-01'}]});if(r[0].status!=='failed')process.exit(1);"
        subprocess.run(['node','-e',js],check=True)

    def test_runner_wrapper_uses_temporary_config_and_preserves_checkout(self):
        import contextlib, io, json
        root=Path(self.temp.name)/'managed';root.mkdir()
        checkout=root/'checkout';checkout.mkdir();marker=checkout/'keep.txt';marker.write_text('untouched')
        runner=root/'runner'
        runner.write_text("#!/usr/bin/env python3\nimport json, pathlib, sys\na=sys.argv; config=pathlib.Path(a[a.index('--config')+1]); out=pathlib.Path(a[a.index('--json')+1]); d=json.loads((config/'tiers.json').read_text()); assert d['repos'][0]['pin']=='b'*40; assert d['repos'][0]['overlay']=={'deps':[]}; out.write_text(json.dumps({'results':[{'complete':True,'observed':{'counts':{'tests':1,'passed':1}}}]}))\n")
        runner.chmod(0o755)
        data={'root':str(root),'repo':{'name':'example__alpha','tier':'A','pin':'a'*40,'overlay':{'deps':[]}},'depSources':{},'checkout':str(checkout),'sha':'b'*40,'glade':'/bin/true','runner':str(runner)}
        output=io.StringIO()
        with contextlib.redirect_stdout(output): exec(ops.UPDATE_RUNNER,{'DATA':data})
        self.assertTrue(json.loads(output.getvalue())['complete'])
        self.assertEqual(marker.read_text(),'untouched')
        self.assertEqual(sorted(x.name for x in root.iterdir()),['checkout','runner'])

    def test_delete_script_checks_ownership_and_containment(self):
        import contextlib, io
        root=Path(self.temp.name)/'managed';root.mkdir()
        target=root/'owned';target.mkdir();(target/'keep').write_text('owned')
        marker=root/'.owned.dashboard-owner';marker.write_text('token')
        with self.assertRaises(ValueError):
            exec(ops.DELETE_PULL,{'DATA':{'root':str(root),'path':str(target),'token':'wrong'}})
        self.assertTrue(target.exists())
        with contextlib.redirect_stdout(io.StringIO()):
            exec(ops.DELETE_PULL,{'DATA':{'root':str(root),'path':str(target),'token':'token'}})
        self.assertFalse(target.exists());self.assertFalse(marker.exists())

    def test_runner_cancel_signal_prevents_execution(self):
        root=Path(self.temp.name)/'managed';root.mkdir()
        (root/('.dashboard-cancel-'+'a'*32)).write_text('cancel')
        data={'root':str(root),'repo':{'name':'example__alpha','tier':'A','pin':'a'*40},'depSources':{},'checkout':'/unused','sha':'b'*40,'glade':'/unused','runner':'/unused','cancel_token':'a'*32}
        with patch('subprocess.Popen') as process:
            with self.assertRaisesRegex(ValueError,'Cancelled'):
                exec(ops.UPDATE_RUNNER,{'DATA':data})
        process.assert_not_called()

    def test_existing_lane_prevents_host_probe(self):
        slots=Path(self.temp.name)/'slots';slots.mkdir();(slots/'local.0').mkdir()
        cfg=Path(self.temp.name)/'routes.json'
        cfg.write_text(__import__('json').dumps({'lanes':{'slots_dir':str(slots),'hosts':{'local':{'flag':'LOCAL_OFF','prefix':'local'}}}}))
        with patch.dict('os.environ',{'GLADE_CORPUS_MANAGER_ROUTES':str(cfg)}),patch.object(ops,'host_python') as probe:
            self.assertTrue(self.app.ops.host_busy({'ssh':''}))
            probe.assert_not_called()

    def test_existing_upstream_result_is_stale_when_pin_differs(self):
        import json
        p=Path(self.temp.name)/'tiers.json'
        p.write_text(json.dumps({'repos':[{'name':'example__alpha','tier':'A','pin':'c'*40}]}))
        self.app.set_tiers_file(str(p))
        repo=next(x for x in self.app.state()['repositories'] if x['id']==self.repo['id'])
        self.assertTrue(repo['upstream']['stale_pin'])
        self.assertEqual(repo['upstream']['status'],'unknown')

    def test_update_comparison_displays_names_only_on_same_glade(self):
        import subprocess
        source=(Path(__file__).parents[1]/'static'/'app.js').read_text()
        fn=next(x for x in source.splitlines() if x.startswith('function updateComparison('))
        js="const esc=x=>String(x),num=x=>String(x);\n"+fn+"\nconst repo={pin:'a',runs:[{repo_sha:'a',glade_sha:'g',phase:'test',failed_tests:['old']},{repo_sha:'b',glade_sha:'g',phase:'test',gate_label:'update-b',failed_tests:['new']}]};const html=updateComparison(repo,{head:'b'});if(!html.includes('old')||!html.includes('new')||!html.includes('fixes 1, breaks 1'))process.exit(1);repo.runs[0].glade_sha='other';if(!updateComparison(repo,{head:'b'}).includes('No pin baseline'))process.exit(2);"
        subprocess.run(['node','-e',js],check=True)
