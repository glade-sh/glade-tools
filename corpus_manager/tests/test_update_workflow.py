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
