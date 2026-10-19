#!/usr/bin/env python3
"""Run the existing public Atlas scraper in bounded anonymous browser sessions.

The scraper is supplied explicitly. No Salesforce CLI, org or auth state is used.
Raw output and acquisition receipts belong outside the public repository.
"""
import argparse
import asyncio
import hashlib
import importlib.util
import json
import re
import shutil
from pathlib import Path

from assemble_versioned_docs import sha256, tree_sha256


def publication_label(identity, atlas):
    api = str((int(atlas.split('.')[0]) - 128) // 2) + '.0'
    if identity.get('doc_version') != atlas or identity.get('release_version') != api:
        raise ValueError(f'publication identity does not match Atlas {atlas}: {identity}')
    match = re.fullmatch(r"(.+) \(API version " + re.escape(api) + r"\)", identity.get('version_text', ''))
    if not match:
        raise ValueError('publication season label is missing or ambiguous')
    return f'API v{api}  ({match[1]})'


def batches(pages, size):
    if size < 1:
        raise ValueError('batch size must be positive')
    for start in range(0, len(pages), size):
        yield pages[start:start + size]


def require_complete(counts, expected):
    success, empty, failed = counts
    if success != expected or empty or failed:
        raise ValueError(f'incomplete public documentation batch: success={success}, empty={empty}, failed={failed}, expected={expected}')


def write(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + '\n', encoding='utf-8')


def is_connectapi(page):
    return bool(re.fullmatch(r'(?:apex_connectapi_|connectapi_)[A-Za-z0-9_]+\.htm', page, re.IGNORECASE))


def connectapi_policy(missing_pages, atlas, evidence):
    """Account for unavailable references without inventing their declarations.

    This policy applies only to ConnectApi. Already acquired documents remain
    in the inventory; their declarations still require explicit classification.
    """
    prefix = 'https://developer.salesforce.com/docs/get_document_content/apexref/'
    suffix = '/en-us/' + atlas
    failures = []
    for row in evidence:
        if row.get('atlas') != atlas:
            continue
        url = row.get('url', '')
        page = url.removeprefix(prefix).removesuffix(suffix)
        if row.get('status') != 403 or not url.startswith(prefix) or not url.endswith(suffix) or not is_connectapi(page):
            raise ValueError('retained HTTP evidence must identify only ConnectApi 403 responses')
        failures.append({'url': url, 'status': 403})
    if len({row['url'] for row in failures}) != len(failures):
        raise ValueError('duplicate retained HTTP evidence')
    references = [{'page': page, 'sourceURL': prefix + page + suffix,
                   'namespace': 'ConnectApi', 'acquisition': 'policy-excluded',
                   'disposition': 'explicitly-unsupported',
                   'reason': 'excluded and unsupported; documentation not acquired'}
                  for page in sorted(missing_pages) if is_connectapi(page)]
    return {'schemaVersion': 1, 'atlasVersion': atlas,
            'policy': 'ConnectApi is excluded and unsupported',
            'references': references,
            'httpEvidence': {'statusCounts': {'403': len(failures)},
                             'responses': sorted(failures, key=lambda row: row['url'])},
            'fetchPages': [page for page in missing_pages if not is_connectapi(page)]}


async def acquire(scraper, output, atlas, families, batch_size, concurrency, delay, scraper_path,
                  retained_input=None, failure_evidence=None, exclude_connectapi=False):
    output.mkdir(parents=True, exist_ok=True)
    receipt_path = output / '_acquisition-batched.json'
    if receipt_path.exists():
        raise FileExistsError(f'acquisition receipt already exists: {receipt_path}')
    started = scraper.utc_timestamp()
    receipt = {'schemaVersion': 1, 'atlasVersion': atlas, 'startedAt': started,
               'scraperSHA256': hashlib.sha256(scraper_path.read_bytes()).hexdigest(),
               'wrapperSHA256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
               'batchSize': batch_size, 'concurrency': concurrency, 'requestDelaySeconds': delay,
               'complete': False, 'families': []}
    evidence = json.loads(failure_evidence.read_text()) if failure_evidence else []
    if retained_input:
        receipt['retainedInputSHA256'] = tree_sha256(retained_input)
    if failure_evidence:
        receipt['retainedHTTPEvidenceSHA256'] = sha256(failure_evidence)
    write(receipt_path, receipt)
    for family in families:
        pages = []
        cfg = scraper.DOCSETS[family]
        record = {'name': family, 'metadataURL': scraper.atlas_toc_api_url(cfg['docset'], atlas), 'attemptedAt': scraper.utc_timestamp(), 'complete': False, 'batches': []}
        receipt['families'].append(record)
        try:
            if retained_input:
                publication = retained_input / ('_publication-batched-' + family + '.json')
                toc = json.loads(publication.read_text())
                record['retainedPublicationSHA256'] = sha256(publication)
            else:
                toc = await scraper.fetch_toc(cfg['docset'], atlas, cfg['index'])
            scraper.KNOWN_VERSIONS[atlas] = publication_label(toc['version'], atlas)
            record['publicationIdentity'] = toc['version']
            write(output / ('_publication-batched-' + family + '.json'), toc)
            pages = scraper.extract_base_pages_from_toc(toc['toc'])
            if cfg.get('page_filter'):
                pages = [page for page in pages if cfg['page_filter'](page)]
            if not pages:
                raise ValueError(f'empty public TOC for {family}')
            directory = output / family
            directory.mkdir(exist_ok=True)
            if retained_input:
                for page in pages:
                    source = scraper.atlas_markdown_path(retained_input / family, page)
                    if source.is_file():
                        target = scraper.atlas_markdown_path(directory, page)
                        target.parent.mkdir(parents=True, exist_ok=True)
                        shutil.copy2(source, target)
            write(directory / '_toc.json', {'title': cfg['name'], 'pages': pages, 'toc': toc['toc']})
            missing = [page for page in pages if not scraper.atlas_markdown_path(directory, page).is_file()]
            policy = connectapi_policy(missing, atlas, evidence) if exclude_connectapi and family == 'apex' else None
            if policy:
                valid_urls = {scraper.atlas_content_api_url(cfg['docset'], page, atlas) for page in pages}
                if any(row['url'] not in valid_urls for row in policy['httpEvidence']['responses']):
                    raise ValueError('retained ConnectApi HTTP evidence does not belong to the public TOC')
            pending = policy['fetchPages'] if policy else missing
            if policy:
                ledger = {key: value for key, value in policy.items() if key != 'fetchPages'}
                ledger['tocSHA256'] = sha256(directory / '_toc.json')
                write(directory / '_policy-excluded.json', ledger)
                record['policyExcluded'] = len(policy['references'])
                record['policyReceiptSHA256'] = sha256(directory / '_policy-excluded.json')
                record['httpEvidence'] = policy['httpEvidence']
            total = len(pages) - len(missing)
            for batch in batches(pending, batch_size):
                counts = await scraper.fetch_and_save_atlas_pages(cfg['docset'], atlas, batch,
                    scraper.atlas_doc_url(cfg['docset'], atlas, cfg['index']), directory,
                    concurrency, request_delay_seconds=delay, resume=True)
                record['batches'].append({'completedAt': scraper.utc_timestamp(), 'pages': len(batch), 'success': counts[0], 'empty': counts[1], 'failed': counts[2]})
                write(receipt_path, receipt)
                require_complete(counts, len(batch))
                total += counts[0]
                print(f'{atlas} {family}: {total}/{len(pages)} successful pages', flush=True)
            remaining = {page for page in pages if not scraper.atlas_markdown_path(directory, page).is_file()}
            excluded = {row['page'] for row in policy['references']} if policy else set()
            if remaining != excluded or total + len(excluded) != len(pages):
                raise ValueError('acquired documents and policy-excluded references do not cover the public TOC exactly')
            scraper.write_docset_version(directory, cfg['name'], atlas, total, 0, 0)
            if policy:
                version_path = directory / '_version.json'
                version = json.loads(version_path.read_text())
                version['policyExcludedReferences'] = len(policy['references'])
                version['tocReferences'] = len(pages)
                version['policyReceiptSHA256'] = sha256(directory / '_policy-excluded.json')
                write(version_path, version)
            record['complete'] = True
        except Exception as error:
            record['error'] = type(error).__name__ + ': ' + str(error)
            record['missingURLs'] = [scraper.atlas_content_api_url(cfg['docset'], page, atlas) for page in pages if not scraper.atlas_markdown_path(output / family, page).is_file()]
            write(receipt_path, receipt)
            raise
        write(receipt_path, receipt)
    scraper.build_manifest_and_index(output, atlas, started)
    receipt['complete'] = True
    receipt['completedAt'] = scraper.utc_timestamp()
    write(receipt_path, receipt)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--scraper', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--atlas', required=True)
    parser.add_argument('--families', nargs='+', default=['apex', 'lightning', 'rest-api', 'tooling-api', 'visualforce'])
    parser.add_argument('--batch-size', type=int, default=100)
    parser.add_argument('--concurrency', type=int, default=1)
    parser.add_argument('--request-delay', type=float, default=0.5)
    parser.add_argument('--retained-input', type=Path, help='Reuse retained public TOC and Markdown without refetching them')
    parser.add_argument('--failure-evidence', type=Path, help='Retained HTTP status records; local log names are not exported')
    parser.add_argument('--exclude-connectapi', action='store_true', help='Record missing ConnectApi references as explicitly unsupported; never request them')
    args = parser.parse_args()
    if not re.fullmatch(r'[1-9][0-9]*\.0', args.atlas) or int(args.atlas.split('.')[0]) % 2 or int(args.atlas.split('.')[0]) <= 128:
        parser.error('Atlas must be a canonical even version greater than 128')
    if args.concurrency < 1 or args.batch_size < 1 or args.request_delay < 0:
        parser.error('batch size/concurrency must be positive; delay must be nonnegative')
    spec = importlib.util.spec_from_file_location('atlas_scraper', args.scraper)
    scraper = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(scraper)
    asyncio.run(acquire(scraper, args.output, args.atlas, args.families, args.batch_size, args.concurrency, args.request_delay, args.scraper,
                        args.retained_input, args.failure_evidence, args.exclude_connectapi))


if __name__ == '__main__':
    main()
