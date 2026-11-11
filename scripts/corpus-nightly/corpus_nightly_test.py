import json
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import corpus_nightly as n


def write_config(root, repos, goldens):
    (root / 'goldens').mkdir()
    (root / 'tiers.json').write_text(json.dumps({'schemaVersion': 1, 'repos': repos}))
    for name in goldens:
        (root / 'goldens' / (name + '.json')).write_text('{}')


class PreflightTest(unittest.TestCase):
    def test_counts_nested_tiers_blocked_and_missing(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            write_config(root, [
                {'name': 'a', 'tier': 'A'}, {'name': 'b', 'tier': 'B', 'blocked': 'why'},
                {'name': 'c', 'tier': 'B'}, {'name': 'd', 'tier': 'C'}, {'name': 'e', 'tier': 'B'}], ['a', 'e'])
            self.assertEqual(n.preflight(root, 'B'), {'selected': 4, 'blocked': 1, 'missing': 1})
            self.assertEqual(n.preflight(root, 'A'), {'selected': 1, 'blocked': 0, 'missing': 0})
            self.assertEqual(n.preflight(root, 'C')['selected'], 5)
            self.assertEqual(n.runnable(n.preflight(root, 'B')), 2)

    def test_rejects_bad_names_and_schema(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            write_config(root, [{'name': '../x', 'tier': 'B'}], [])
            with self.assertRaises(ValueError):
                n.preflight(root, 'B')
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            write_config(root, [{'name': 'a', 'tier': 'B'}, {'name': 'a', 'tier': 'B'}], [])
            with self.assertRaises(ValueError):
                n.preflight(root, 'B')


class FirstLineTest(unittest.TestCase):
    def report(self, payload):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        path = pathlib.Path(tmp.name) / 'r.json'
        path.write_text(payload if isinstance(payload, str) else json.dumps(payload))
        return path

    def test_accepts_matching_first_lines(self):
        self.assertEqual(n.first_line_for(self.report({'first': 'CORPUS-CLEAN', 'exitCode': 0}), 0), ('CORPUS-CLEAN', 0))
        self.assertEqual(n.first_line_for(self.report({'first': 'CORPUS-NEW 3 in 2 repos', 'exitCode': 1}), 1),
                         ('CORPUS-NEW 3 in 2 repos', 1))
        self.assertEqual(n.first_line_for(self.report({'first': 'CORPUS-INFRA 1 repos', 'exitCode': 2}), 2),
                         ('CORPUS-INFRA 1 repos', 2))

    def test_disagreement_is_infra(self):
        bad = ('CORPUS-INFRA: nightly did not produce a valid report', 2)
        self.assertEqual(n.first_line_for(self.report({'first': 'CORPUS-CLEAN', 'exitCode': 0}), 1), bad)
        self.assertEqual(n.first_line_for(self.report({'first': 'CORPUS-CLEAN\nextra', 'exitCode': 0}), 0), bad)
        self.assertEqual(n.first_line_for(self.report({'first': 'CORPUS-CLEAN x', 'exitCode': 0}), 0), bad)
        self.assertEqual(n.first_line_for(self.report('not json'), 0), bad)
        self.assertEqual(n.first_line_for(pathlib.Path('/nonexistent/report.json'), 0), bad)


if __name__ == '__main__':
    unittest.main()
