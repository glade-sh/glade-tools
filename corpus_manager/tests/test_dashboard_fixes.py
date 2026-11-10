import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from corpus_manager.server import App


class DashboardFixes(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        with patch('corpus_manager.ops.GitHub'):
            self.app = App(self.root)
        self.addCleanup(self.app.store.db.close)
        self.addCleanup(self.app.executor.shutdown, wait=True)

    def test_host_disambiguates_and_normalizes_history_name(self):
        first = self.app.ops.add('example/alpha')
        second = self.app.ops.add('other/alpha')
        for repo, host in ((first, 'one'), (second, 'two')):
            repo['locations'] = [{'host': host, 'path': '/work/alpha'}]
            self.app.store.put('repositories', repo)
        self.assertEqual(self.app.match_run({'repo_name': 'alpha', 'host': 'one'})['repo_id'], first['id'])
        self.assertEqual(self.app.match_run({'repo_name': 'example__alpha', 'host': 'one'})['repo_id'], first['id'])

    def test_sources_backfill_label_and_sha(self):
        (self.root / 'history-sources.json').write_text(json.dumps({'sources': [{'host':'one','path':'/results.jsonl','label':'gate A','glade_sha':'abc'}]}))
        self.app.store.put('runs', {'id':'run', 'host':'one', 'source':'/results.jsonl:1'})
        self.app.migrate_history()
        row = self.app.store.get('runs', 'run')
        self.assertEqual((row['gate_label'], row['glade_sha']), ('gate A', 'abc'))

    def test_tier_write_preserves_fields_and_detects_stale_file(self):
        repo = self.app.ops.add('example/alpha')
        path = self.root / 'tiers.json'
        path.write_text(json.dumps({'schemaVersion':1,'depSources':{},'repos':[{'name':'example__alpha','tier':'A','pin':'abc','overlay':{'x':1},'blocked':''}]}))
        self.app.set_tiers_file(str(path))
        token = self.app.tiers_snapshot()['revision']
        self.app.move_tier(repo, 'B', token)
        entry = json.loads(path.read_text())['repos'][0]
        self.assertEqual((entry['tier'], entry['pin'], entry['overlay']), ('B','abc',{'x':1}))
        with self.assertRaises(ValueError):
            self.app.move_tier(repo, 'C', token)
        self.assertEqual(json.loads(path.read_text())['repos'][0]['tier'], 'B')
