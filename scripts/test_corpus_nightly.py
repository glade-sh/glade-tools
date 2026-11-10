import importlib.util
import json
import pathlib
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).with_name('corpus_nightly.py')
spec = importlib.util.spec_from_file_location('corpus_nightly', SCRIPT)
nightly = importlib.util.module_from_spec(spec)
spec.loader.exec_module(nightly)


class CorpusNightlyTest(unittest.TestCase):
    def test_tier_b_rejects_blocked_and_missing_goldens(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            (root / 'goldens').mkdir()
            config = {'schemaVersion': 1, 'repos': [
                {'name': 'synthetic-a', 'tier': 'A'},
                {'name': 'synthetic-b', 'tier': 'B', 'blocked': 'source unavailable'},
                {'name': 'synthetic-c', 'tier': 'C'},
            ]}
            (root / 'tiers.json').write_text(json.dumps(config))
            result = nightly.preflight(root)
            self.assertEqual(result, {'selected': 2, 'blocked': 1, 'missing': 1})
            (root / 'goldens' / 'synthetic-a.json').write_text('{}')
            self.assertEqual(nightly.preflight(root), {'selected': 2, 'blocked': 1, 'missing': 0})

    def test_config_fingerprint_covers_golden_contents(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            (root / 'goldens').mkdir()
            (root / 'tiers.json').write_text('{}')
            golden = root / 'goldens' / 'synthetic-a.json'
            golden.write_text('{}')
            before = nightly.config_fingerprint(root)
            golden.write_text('{"changed": true}')
            self.assertNotEqual(before, nightly.config_fingerprint(root))

    def test_rejects_empty_or_unsafe_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            for repos in ([], [{'name': '../escape', 'tier': 'A'}],
                          [{'name': 'same', 'tier': 'A'}, {'name': 'same', 'tier': 'B'}]):
                (root / 'tiers.json').write_text(json.dumps({'schemaVersion': 1, 'repos': repos}))
                with self.subTest(repos=repos), self.assertRaises(ValueError):
                    nightly.preflight(root)


if __name__ == '__main__':
    unittest.main()
