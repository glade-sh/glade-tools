import copy
import json
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from compose_winter27 import compose, sha256


class WinterCompositionTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        fixtures = Path(__file__).resolve().parents[2] / 'docs/fixtures'
        self.previous = fixtures / 'salesforce-release-source-summer-26.json'
        self.previous_inventory = fixtures / 'salesforce-docs-inventory-summer-26.json'
        self.atlas = self.root / 'atlas'
        self.atlas.mkdir()
        (self.atlas/'_scrape-meta.json').write_text('{"version":"264.0"}')
        for family in ('apex','tooling-api','apex-guide','metadata-api','object-reference'):
            p=self.atlas/family;p.mkdir();(p/'_version.json').write_text(json.dumps({'version':'264.0','pages':{'success':1,'empty':0,'failed':0,'total':1}}));(p/'page.md').write_text('# Test\n')
        self.notes = self.root/'notes';self.notes.mkdir();(self.notes/'note.md').write_text('# Note\n')
        (self.notes/'_export-meta.json').write_text(json.dumps({'complete':True,'release':'264.0.0','tocArticleCount':1,'exportedArticleCount':1,'selectedArticleCount':1,'failures':[],'files':[{'file':'note.md','version':'264.0.0','sha256':sha256(self.notes/'note.md')}]}))
        previous = json.loads(self.previous_inventory.read_text())
        previous['documents']=[d for d in previous['documents'] if d['sourcePath'].split('/')[0] not in ('apex','tooling-api')]
        previous['totalFiles']=len(previous['documents'])
        self.inventory=self.root/'inventory.json';self.inventory.write_text(json.dumps(previous))
        self.manifest=self.root/"manifest.json";self.manifest.write_text(json.dumps({"release":"Winter '27","apiVersion":"68.0","digest":"f"*64}))

    def build(self):
        return compose(self.atlas,self.notes,self.previous,self.previous_inventory,self.inventory,self.manifest)

    def test_deterministic_explicit_bound_previous_ga_carries(self):
        a,receipt=self.build();self.assertEqual((a,receipt),self.build())
        for f in receipt['snapshot']['families']:
            if f['name'] in ('lightning-aura','rest-api','visualforce'):
                self.assertEqual('262.0',f['carriedFrom']);self.assertEqual(sha256(self.previous),f['carriedReceiptSHA256'])
        self.assertEqual('262.0',receipt['snapshot']['lwc']['carriedFrom'])
        self.assertNotIn(str(self.root),json.dumps(receipt))

    def test_modified_carried_document_is_rejected(self):
        d=json.loads(self.inventory.read_text());d['documents'][0]['title']='changed';self.inventory.write_text(json.dumps(d))
        with self.assertRaisesRegex(ValueError,'carried inventory documents differ'):self.build()

    def test_wrong_previous_inventory_hash_is_rejected(self):
        p=self.root/'previous-inventory.json';p.write_text(self.previous_inventory.read_text()+' ');self.previous_inventory=p
        with self.assertRaisesRegex(ValueError,'previous inventory differs'):self.build()

    def test_non_264_source_is_rejected(self):
        (self.atlas/'_scrape-meta.json').write_text('{"version":"262.0"}')
        with self.assertRaisesRegex(ValueError,'explicit 264.0'):self.build()

    def test_failed_physical_export_is_rejected(self):
        (self.atlas/'apex'/'_version.json').write_text(json.dumps({'version':'264.0','pages':{'success':1,'empty':0,'failed':1,'total':2}}))
        with self.assertRaisesRegex(ValueError, 'incomplete'):
            self.build()

    def test_supplementary_context_is_optional(self):
        for family in ('apex-guide','metadata-api','object-reference'):
            shutil.rmtree(self.atlas/family)
        composition, receipt = self.build()
        self.assertEqual([f['name'] for f in composition['additionalFamilies']], ['release-notes'])
        self.assertEqual(composition['additionalFamilies'], receipt['snapshot']['additionalFamilies'])

    def test_present_but_partial_supplementary_export_is_rejected(self):
        (self.atlas/'metadata-api'/'_version.json').write_text(json.dumps({'version':'264.0','pages':{'success':1,'empty':1,'failed':0,'total':2}}))
        with self.assertRaisesRegex(ValueError, 'incomplete'):
            self.build()

    def test_incomplete_release_notes_are_rejected(self):
        path=self.notes/'_export-meta.json'
        metadata=json.loads(path.read_text());metadata['complete']=False
        path.write_text(json.dumps(metadata))
        with self.assertRaisesRegex(ValueError, 'release-note export is incomplete'):
            self.build()

    def test_modified_release_note_is_rejected(self):
        (self.notes/'note.md').write_text('# Changed after export\n')
        with self.assertRaisesRegex(ValueError, 'release-note file hash'):
            self.build()
