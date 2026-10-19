import copy
import json
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
            p=self.atlas/family;p.mkdir();(p/'_version.json').write_text('{"version":"264.0"}');(p/'page.md').write_text('# Test\n')
        self.notes = self.root/'notes';self.notes.mkdir();(self.notes/'note.md').write_text('# Note\n')
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
