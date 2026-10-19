"""Bind a fresh current-only LWC view to an exact seasonal publication note."""
from pathlib import Path
import hashlib
import json

from assemble_versioned_docs import sha256, tree_sha256, unavailable_lwc_pages


def build(source, notes_inventory):
    source, notes_inventory = Path(source), Path(notes_inventory)
    identity = json.loads((source / '_version.json').read_text())
    files = sorted(source.glob('*.md'))
    count = len(files)
    if identity.get('version') != 'latest' or not count or identity.get('pages') != {'success': count, 'empty': 0, 'failed': 0, 'total': count}:
        raise ValueError('fresh current-only LWC acquisition is incomplete')
    invalid = [p.name for p in files if p.read_text().lstrip().startswith('# Oops') or '## Something went wrong' in p.read_text()]
    if invalid:
        raise ValueError('current-only LWC acquisition contains publisher error pages: ' + ', '.join(invalid))
    notes = json.loads(notes_inventory.read_text())
    publication = [doc for doc in notes['documents'] if doc['sourcePath'] == 'rn_lwc_versioning.md']
    if len(publication) != 1 or publication[0].get('title') != 'Get the Latest LWC Changes with LWC API Version 68.0':
        raise ValueError('exact API 68 LWC publication note is required')
    unavailable = unavailable_lwc_pages(source / 'reference-api-modules.md', 68)
    copied = [{'path': p.name, 'sha256': sha256(p)} for p in files if p.name not in unavailable]
    excluded = [{'file': p.name, 'sourceSHA256': sha256(p), 'module': unavailable[p.name][0], 'firstAvailableAPIVersion': unavailable[p.name][1]} for p in files if p.name in unavailable]
    filtering = {'targetAPIVersion': '68.0', 'copied': copied, 'excluded': excluded}
    return {
        'currentOnlyPublication': {'apiVersion': '68.0', 'inventory': {'path': notes_inventory.name, 'sha256': sha256(notes_inventory)}, 'sourcePath': 'rn_lwc_versioning.md', 'sourceURL': 'https://help.salesforce.com/s/articleView?id=release-notes.rn_lwc_versioning.htm&language=en_US&release=264&type=5'},
        'filterReceiptSHA256': hashlib.sha256((json.dumps(filtering, indent=2, sort_keys=True) + '\n').encode()).hexdigest(),
        'sourceVersion': 'latest', 'sourceVersionSHA256': tree_sha256(source),
        'sourceVersionMetadata': {'file': '_version.json', 'version': 'latest', 'sha256': sha256(source / '_version.json')},
        'availabilityTable': 'reference-api-modules.md', 'availabilityTableSHA256': sha256(source / 'reference-api-modules.md'),
        'copiedMarkdownFiles': len(copied), 'copied': copied, 'excluded': excluded,
        'limitation': 'Salesforce publishes the LWC reference as current-release-only; this is an availability-filtered view. The seasonal publication note binds API 68, not an invented versioned LWC guide.',
    }
