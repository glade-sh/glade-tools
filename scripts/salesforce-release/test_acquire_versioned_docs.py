import asyncio
import importlib.util
import json
from types import SimpleNamespace
import tempfile
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location('acquire', Path(__file__).with_name('acquire_versioned_docs.py'))
acquire = importlib.util.module_from_spec(spec)


class AcquisitionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec.loader.exec_module(acquire)

    def test_identity_comes_from_publication_not_stale_label(self):
        self.assertEqual(acquire.publication_label({'doc_version':'252.0', 'release_version':'62.0', 'version_text':"Winter '25 (API version 62.0)"}, '252.0'), "API v62.0  (Winter '25)")
        for value in ({'doc_version':'252.0','release_version':'63.0','version_text':'wrong'}, {'doc_version':'254.0','release_version':'63.0','version_text':"Spring '25 (API version 63.0)"}):
            with self.assertRaises(ValueError):
                acquire.publication_label(value, '252.0')

    def test_partial_family_is_never_marked_complete(self):
        with self.assertRaisesRegex(ValueError,'incomplete'):
            acquire.require_complete((99, 0, 1), 100)
        with self.assertRaisesRegex(ValueError,'incomplete'):
            acquire.require_complete((99, 1, 0), 100)
        acquire.require_complete((100, 0, 0), 100)

    def test_batches_cover_every_page_once(self):
        self.assertEqual(list(acquire.batches(list(range(5)), 2)), [[0,1],[2,3],[4]])
        with self.assertRaises(ValueError):
            list(acquire.batches([1], 0))

    def test_failed_batch_retains_receipt_without_snapshot_metadata(self):
        async def toc(*args):
            return {'version': {'doc_version': '252.0', 'release_version': '62.0', 'version_text': "Winter '25 (API version 62.0)"}, 'toc': []}
        async def fetch(*args, **kwargs):
            return (0, 0, 1)
        scraper = SimpleNamespace(
            utc_timestamp=lambda: '2026-10-19T00:00:00Z',
            DOCSETS={'apex': {'docset':'apexref', 'index':'index.htm', 'name':'Apex'}},
            KNOWN_VERSIONS={}, fetch_toc=toc,
            atlas_toc_api_url=lambda *args: 'https://developer.salesforce.com/metadata',
            extract_base_pages_from_toc=lambda _: ['example.htm'],
            atlas_doc_url=lambda *args: 'https://developer.salesforce.com/index',
            fetch_and_save_atlas_pages=fetch,
            atlas_content_api_url=lambda *args: 'https://developer.salesforce.com/content',
            atlas_markdown_path=lambda directory, page: directory / 'example.md',
        )
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / 'raw'
            with self.assertRaisesRegex(ValueError, 'incomplete'):
                asyncio.run(acquire.acquire(scraper, output, '252.0', ['apex'], 100, 1, 0, Path(__file__)))
            receipt = json.loads((output / '_acquisition-batched.json').read_text())
            self.assertFalse(receipt['complete'])
            self.assertFalse(receipt['families'][0]['complete'])
            self.assertEqual(receipt['families'][0]['missingURLs'], ['https://developer.salesforce.com/content'])
            self.assertFalse((output / '_scrape-meta.json').exists())
            self.assertFalse((output / 'apex' / '_version.json').exists())
