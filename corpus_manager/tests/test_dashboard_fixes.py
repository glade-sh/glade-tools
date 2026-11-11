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

class OperationalFixes(DashboardFixes):
    def test_private_checkout_never_contacts_github(self):
        repo = self.app.ops.add('example/alpha')
        repo['locations'] = [{'id':'x', 'host':'one', 'path':'/work/private/alpha', 'head':'a'}]
        self.app.store.put('repositories', repo)
        with self.assertRaises(ValueError):
            self.app.ops.check(repo['id'])
        self.app.ops.github.get.assert_not_called()

    def test_pin_is_comparison_base(self):
        repo = self.app.ops.add('example/alpha')
        repo['locations'] = [{'id':'x','host':'one','path':'/work/alpha','head':'checkout'}]
        repo['tracked_location_id'] = 'x'
        self.app.store.put('repositories', repo)
        path = self.root / 'tiers.json'
        path.write_text(json.dumps({'repos':[{'name':'example__alpha','tier':'A','pin':'pin'}]}))
        self.app.set_tiers_file(str(path))
        self.app.ops.github.get.side_effect = [
            {'default_branch':'main','stargazers_count':0,'pushed_at':'','archived':False,'size':1},
            {'sha':'new'}, {'status':'ahead','ahead_by':2,'html_url':'https://example.invalid/'}]
        self.app.ops.check(repo['id'])
        row = self.app.store.get('repositories', repo['id'])
        self.assertEqual(row['upstream']['base_sha'], 'pin')
        self.assertIn('pin...new', self.app.ops.github.get.call_args.args[0])

    def test_candidate_check_caches_positive_and_404(self):
        for key, result in [('one', 'confirmed'), ('two', 'absent')]:
            self.app.store.put('candidates', {'id':key, 'url':'https://github.com/example/alpha'})
            self.app.ops.github.get.reset_mock()
            self.app.ops.github.get.side_effect = ValueError('GitHub returned HTTP 404.') if key == 'two' else None
            self.app.ops.check_candidate(key)
            self.app.ops.check_candidate(key)
            self.assertEqual(self.app.store.get('candidates', key)['salesforce_project'], result)
            self.assertEqual(self.app.ops.github.get.call_count, 1)

    def test_owner_directory_url_is_only_saved_after_metadata_success(self):
        repo = self.app.ops.add('example/alpha')
        repo.update(name='example__alpha', url='', locations=[])
        self.app.store.put('repositories', repo)
        self.app.ops.github.get.side_effect = ValueError('GitHub returned HTTP 404.')
        with self.assertRaises(ValueError):
            self.app.ops.check(repo['id'])
        self.assertEqual(self.app.store.get('repositories', repo['id'])['url'], '')
        self.app.ops.github.get.side_effect = [
            {'default_branch':'main','stargazers_count':0,'pushed_at':'','archived':False,'size':1}, {'sha':'new'}]
        self.app.ops.check(repo['id'])
        self.assertEqual(self.app.store.get('repositories', repo['id'])['url'], 'https://github.com/example/alpha')

    def test_latest_result_prefers_test_when_phases_share_timestamp(self):
        import subprocess
        source = (Path(__file__).parents[1] / 'static' / 'app.js').read_text()
        function = next(line for line in source.splitlines() if line.startswith('function latestRun('))
        script = function + "\nconst r=latestRun({runs:[{phase:'check',status:'passed',started_at:'2026-01-01'},{phase:'test',status:'failed',started_at:'2026-01-01'}]});if(r.status!=='failed')process.exit(1);"
        subprocess.run(['node', '-e', script], check=True)
