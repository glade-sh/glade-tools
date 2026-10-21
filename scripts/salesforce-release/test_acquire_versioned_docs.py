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

    def test_connectapi_policy_preserves_reference_denominator_and_403_evidence(self):
        pages = ['apex_connectapi_example.htm', 'connectAPI_enums.htm', 'apex_System_Crypto.htm']
        url = 'https://developer.salesforce.com/docs/get_document_content/apexref/apex_connectapi_example.htm/en-us/264.0'
        retained = [{'atlas': '264.0', 'url': url, 'status': 403, 'log': 'local-only.log'}]
        plan = acquire.connectapi_policy(pages, '264.0', retained)
        self.assertEqual(plan['fetchPages'], ['apex_System_Crypto.htm'])
        self.assertEqual(len(plan['references']), 2)
        self.assertEqual(plan['httpEvidence']['statusCounts'], {'403': 1})
        self.assertNotIn('local-only.log', json.dumps(plan))
        for row in plan['references']:
            self.assertEqual(row['namespace'], 'ConnectApi')
            self.assertEqual(row['disposition'], 'explicitly-unsupported')
            self.assertEqual(row['reason'], 'excluded and unsupported; documentation not acquired')

    def test_policy_does_not_exclude_other_namespaces_or_accept_unrelated_failures(self):
        plan = acquire.connectapi_policy(['apex_System_Crypto.htm', 'apex_Auth_JWTUtil.htm'], '264.0', [])
        self.assertEqual(len(plan['fetchPages']), 2)
        with self.assertRaisesRegex(ValueError, 'ConnectApi'):
            acquire.connectapi_policy(['apex_System_Crypto.htm'], '264.0', [{'atlas': '264.0', 'status': 403, 'url': 'https://developer.salesforce.com/docs/get_document_content/apexref/apex_System_Crypto.htm/en-us/264.0'}])

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

    def test_retained_resume_never_requests_excluded_or_acquired_pages(self):
        calls = []
        async def forbidden_toc(*args):
            raise AssertionError('retained TOC must not be fetched')
        async def fetch(docset, atlas, pages, session, directory, *args, **kwargs):
            calls.extend(pages)
            for page in pages:
                (directory / Path(page).with_suffix('.md')).write_text('# Public declaration\n')
            return len(pages), 0, 0
        def version(directory, name, atlas, success, empty, failed):
            acquire.write(directory / '_version.json', {'version': atlas, 'pages': {'total': success, 'success': success, 'empty': empty, 'failed': failed}})
        scraper = SimpleNamespace(
            utc_timestamp=lambda: '2026-10-19T00:00:00Z',
            DOCSETS={'apex': {'docset': 'apexref', 'index': 'index.htm', 'name': 'Apex'}},
            KNOWN_VERSIONS={}, fetch_toc=forbidden_toc,
            atlas_toc_api_url=lambda *args: 'https://developer.salesforce.com/metadata',
            extract_base_pages_from_toc=lambda toc: toc,
            atlas_doc_url=lambda *args: 'https://developer.salesforce.com/index',
            fetch_and_save_atlas_pages=fetch,
            atlas_content_api_url=lambda docset, page, atlas: f'https://developer.salesforce.com/docs/get_document_content/{docset}/{page}/en-us/{atlas}',
            atlas_markdown_path=lambda directory, page: directory / Path(page).with_suffix('.md'),
            write_docset_version=version, build_manifest_and_index=lambda *args: None,
        )
        with tempfile.TemporaryDirectory() as tmp:
            retained, output = Path(tmp) / 'retained', Path(tmp) / 'output'
            (retained / 'apex').mkdir(parents=True)
            (retained / 'apex/apex_connectapi_retained.md').write_text('# Retained public declaration\n')
            pages = ['apex_connectapi_missing.htm', 'apex_connectapi_retained.htm', 'apex_System_Crypto.htm']
            acquire.write(retained / '_publication-batched-apex.json', {'version': {'doc_version': '264.0', 'release_version': '68.0', 'version_text': "Winter '27 (API version 68.0)"}, 'toc': pages})
            asyncio.run(acquire.acquire(scraper, output, '264.0', ['apex'], 100, 1, 0, Path(__file__), retained, None, True))
            self.assertEqual(calls, ['apex_System_Crypto.htm'])
            identity = json.loads((output / 'apex/_version.json').read_text())
            self.assertEqual(identity['tocReferences'], 3)
            self.assertEqual(identity['pages']['success'], 2)
            self.assertEqual(identity['policyExcludedReferences'], 1)
            self.assertTrue(json.loads((output / '_acquisition-batched.json').read_text())['complete'])
            from validate_policy_exclusions import validate
            self.assertEqual(validate(output / 'apex')['policyExcluded'], 1)
            (output / 'apex/apex_System_Crypto.md').unlink()
            with self.assertRaisesRegex(ValueError, 'policy'):
                validate(output / 'apex')
