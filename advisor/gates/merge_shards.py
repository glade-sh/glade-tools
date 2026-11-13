#!/usr/bin/env python3
"""merge_shards.py: combine partial `glade test --json` outputs into one <repo>.test.stdout (same JSON shape).

  merge_shards.py --dir out/LABEL --repo REPO --expect s0of3,s1of3,s2of3      # reads REPO.test.<part>.part
  merge_shards.py --out merged.json part0.json part1.json ...                  # explicit files (cross-host)
  merge_shards.py --scan out/LABEL                                             # merge every complete part set
  merge_shards.py --history OUT.json test.stdout                               # per-class ms history from a result

The merged summary is recomputed from the de-duplicated tests list (verified against glade's own summaries:
total/passed/failed/skipped/unsupported/compileErrors/runtimeErrors/errors). durationMs is the SUM of part
wall times (serial-equivalent); the parts' own wall times are kept under data.merge.partDurationsMs.
Python 3.9 compatible.
"""
import argparse, collections, json, os, sys
from pathlib import Path


def recompute_summary(tests, duration_ms):
    c = collections.Counter(t.get('status') for t in tests)
    return {
        'total': len(tests), 'passed': c['pass'], 'failed': c['fail'],
        'skipped': c['skip'] + c['skipped'], 'compileErrors': c['compile_error'],
        'runtimeErrors': c['runtime_error'], 'unsupported': c['unsupported'],
        'errors': c['unsupported'] + c['compile_error'] + c['runtime_error'],
        'durationMs': duration_ms,
    }


def merge_docs(docs):
    first = docs[0]
    # same test name can legitimately repeat inside one part (duplicate class names in two dirs), so only
    # drop a test that an EARLIER part already reported with the same (name, sourceFile).
    seen, tests = set(), []
    for d in docs:
        keys = set()
        for t in d.get('tests', []):
            k = (t.get('name'), t.get('sourceFile'))
            if k in seen:
                continue
            keys.add(k); tests.append(t)
        seen |= keys
    durs = [int((d.get('summary') or {}).get('durationMs', 0)) for d in docs]
    summary = recompute_summary(tests, sum(durs))
    # keep key order of glade's own summary
    extras = {}
    for d in docs:
        for k, v in (d.get('summary') or {}).items():
            if k not in summary and isinstance(v, (int, float)):
                extras[k] = max(extras.get(k, v), v)   # e.g. excludedClasses is global, not per shard (verified vs unsharded run)
    summary.update(extras)
    order = list((first.get('summary') or {}).keys())
    summary = {k: summary[k] for k in order if k in summary} if set(order) == set(summary) else summary
    merged = dict(first)
    merged['tests'] = tests
    merged['summary'] = summary
    sts = [d.get('status') for d in docs]
    rank = {'failed': 3, 'error': 3, 'empty': 1, 'passed': 0}
    merged['status'] = max(sts, key=lambda x: rank.get(x, 2)) if len(set(sts)) > 1 else sts[0]
    merged['exitCode'] = max(int(d.get('exitCode', 0) or 0) for d in docs)
    sug, seen_s = [], set()
    for d in docs:
        for s in d.get('suggestions') or []:
            if s not in seen_s:
                seen_s.add(s); sug.append(s)
    merged['suggestions'] = sug
    data = dict(first.get('data') or {})
    suites, seen_suite = [], set()
    for d in docs:
        names = set()
        for s in (d.get('data') or {}).get('suites', []):
            if s.get('name') in seen_suite:
                continue
            names.add(s.get('name')); suites.append(s)
        seen_suite |= names
    data['suites'] = suites
    data['summary'] = summary
    data['durationMs'] = summary['durationMs']
    data['merge'] = {'parts': len(docs), 'partDurationsMs': durs}
    merged['data'] = data
    return merged


def write_atomic(path, doc):
    path = Path(path)
    tmp = path.with_name('.%s.%d.tmp' % (path.name, os.getpid()))
    tmp.write_text(json.dumps(doc, indent=2, sort_keys=False))
    os.replace(tmp, path)


def merge_files(files, dest):
    docs = [json.loads(Path(f).read_text()) for f in files]
    merged = merge_docs(docs)
    write_atomic(dest, merged)
    return merged


def merge_parts(outdir, repo, parts):
    outdir = Path(outdir)
    files = [outdir / ('%s.test.%s.part' % (repo, p)) for p in parts]
    return merge_files(files, outdir / (repo + '.test.stdout'))


def class_durations(path):
    d = json.loads(Path(path).read_text())
    hist = {}
    for t in d.get('tests', []):
        c = t.get('className')
        if c:
            hist[c] = hist.get(c, 0) + int(t.get('durationMs', 0) or 0)
    return dict(sorted(hist.items()))


def scan(outdir):
    """Merge every repo in OUTDIR whose parts are complete (s*ofN all present, or inc+exc). Returns merged repo names."""
    import re
    groups = collections.defaultdict(set)
    for f in Path(outdir).glob('*.test.*.part'):
        repo, part = f.name[:-len('.part')].split('.test.', 1)
        groups[repo].add(part)
    done = []
    for repo, parts in sorted(groups.items()):
        m = [re.fullmatch(r's(\d+)of(\d+)', p) for p in parts]
        if all(m):
            n = int(m[0].group(2)); exp = ['s%dof%d' % (i, n) for i in range(n)]
        else:
            exp = ['inc', 'exc'] if 'exc' in parts else sorted(parts)
        if all(e in parts for e in exp):
            merge_parts(outdir, repo, exp); done.append(repo)
        else:
            print('incomplete %s: have %s want %s' % (repo, sorted(parts), exp), file=sys.stderr)
    return done


def main():
    a = argparse.ArgumentParser()
    a.add_argument('--dir'); a.add_argument('--repo'); a.add_argument('--expect')
    a.add_argument('--out'); a.add_argument('--history'); a.add_argument('--scan', help='merge all complete part sets in DIR')
    a.add_argument('files', nargs='*')
    args = a.parse_args()
    if args.scan:
        print('merged', ' '.join(scan(args.scan))); return
    if args.history:
        write_atomic(args.history, class_durations(args.files[0])); return
    if args.dir:
        missing = [p for p in args.expect.split(',') if not (Path(args.dir) / ('%s.test.%s.part' % (args.repo, p))).exists()]
        if missing:
            sys.exit('missing parts for %s: %s' % (args.repo, ','.join(missing)))
        m = merge_parts(args.dir, args.repo, args.expect.split(','))
    else:
        m = merge_files(args.files, args.out)
    print(json.dumps(m['summary']))


if __name__ == '__main__':
    main()
