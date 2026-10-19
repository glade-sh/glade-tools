#!/usr/bin/env python3
"""Verify exact public TOC coverage by acquired docs and ConnectApi exclusions."""
import argparse
import json
from pathlib import Path

from acquire_versioned_docs import connectapi_policy
from assemble_versioned_docs import sha256


def validate(directory):
    directory = Path(directory)
    toc = json.loads((directory / '_toc.json').read_text())
    version = json.loads((directory / '_version.json').read_text())
    policy_path = directory / '_policy-excluded.json'
    policy = json.loads(policy_path.read_text())
    pages = toc['pages']
    if len(set(pages)) != len(pages) or any(Path(page).name != page or not page.endswith('.htm') for page in pages):
        raise ValueError('invalid or duplicate public TOC references')
    actual = {path.relative_to(directory).with_suffix('.htm').as_posix() for path in directory.rglob('*.md')}
    if not actual.issubset(pages):
        raise ValueError('acquired documents do not belong to the public TOC')
    missing = set(pages) - actual
    evidence = [{'atlas': policy['atlasVersion'], **row} for row in policy['httpEvidence']['responses']]
    expected = connectapi_policy(sorted(missing), policy['atlasVersion'], evidence)
    expected.pop('fetchPages')
    expected['tocSHA256'] = sha256(directory / '_toc.json')
    if expected != policy or {row['page'] for row in policy['references']} != missing:
        raise ValueError('policy does not exactly account for unavailable ConnectApi references')
    valid_urls = {'https://developer.salesforce.com/docs/get_document_content/apexref/' + page + '/en-us/' + policy['atlasVersion'] for page in pages}
    if any(row['url'] not in valid_urls for row in policy['httpEvidence']['responses']):
        raise ValueError('retained HTTP evidence does not belong to the public TOC')
    counts = version['pages']
    if version['version'] != policy['atlasVersion'] or version.get('tocReferences') != len(pages) or version.get('policyExcludedReferences') != len(missing) or version.get('policyReceiptSHA256') != sha256(policy_path) or counts != {'total': len(actual), 'success': len(actual), 'empty': 0, 'failed': 0}:
        raise ValueError('policy coverage differs from the family version receipt')
    return {'atlasVersion': policy['atlasVersion'], 'tocReferences': len(pages), 'acquired': len(actual), 'policyExcluded': len(missing), 'retained403': len(evidence)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    args = parser.parse_args()
    print(json.dumps(validate(args.directory), sort_keys=True))


if __name__ == '__main__':
    main()
