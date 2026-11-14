#!/usr/bin/env python3
"""plan_lanes.py: LPT-balance the BMO repos into N lanes and emit one bash script per lane (d12 #3).

  plan_lanes.py --lanes 3 --label NEWLABEL --bin BIN --hist B70 [--exclude r1,r2] [--procs 3] [--memlimit 4GiB]
                [--outdir ~/advisor-broad/lanes] [--dry]

Sets (label prefix -> source clone) and history come from out/<SET>-<hist>/results.jsonl + <repo>.test.stdout.
Splits (all automatic from the history, nothing hard-coded to a repo name; SHARD3 comes from --shard3 or the
operational corpus-config.json key "shard3", empty by default):
  * SHARD3 repos (name fragments) run as 3 class shards (one per lane)
  * a repo with test wall >= 600 s whose top class alone takes >= 70% of that wall (the critical path) is split into
    --include-class <topclass> (own unit, gets full method parallelism) + --exclude-class <topclass> (the rest);
    Repos dominated by one long class fall out of this rule.
Every lane runs run2.py once per set with its own cloned repos; output lands in out/<SET>-<label>/ as before.
"""
import argparse, json, os, sys
from pathlib import Path

SETS = [('BUCKET2', '~/advisor-bucket2-src/clone'), ('BROAD', '~/advisor-broad-src/clone-public-new')]
# a set's label is <name>-<label> unless the name contains '{L}' (e.g. '{L}-public')
SHARD3 = ()


def lab(name, L):
    return name.replace('{L}', L) if '{L}' in name else '%s-%s' % (name, L)


def load_hist(out, hist):
    wall, check, tdur, classes = {}, {}, {}, {}
    for s, _ in SETS:
        d = out / lab(s, hist)
        rj = d / 'results.jsonl'
        if not rj.exists():
            continue
        # 10-25: a repo that ran as parts (inc/exc split or class shards) has only part rows in results.jsonl.
        # Skipping them (the old behaviour) left such repos at the median default, so every
        # FPGATE-history plan put ~3.5 h of real work into one lane (one lane ran 45-70 min past the others and an
        # unsplit long repo hit its 3600 s timeout). Now: whole-repo row if it finished; else the sum of its part walls
        # (serial-equivalent, the same convention merge_shards uses); else the timed-out whole-repo wall.
        whole, whole_to, parts = {}, {}, {}
        for line in rj.read_text().splitlines():
            try:
                r = json.loads(line)
            except ValueError:
                continue
            if r['phase'] == 'check':
                if not r.get('part'):
                    check[r['repo']] = r['wall_s']
                continue
            if r.get('part') or r.get('shard'):
                parts.setdefault(r['repo'], {})[r.get('part') or r.get('shard')] = r['wall_s']
            elif r.get('exit') == 'timeout':
                whole_to[r['repo']] = r['wall_s']
            else:
                whole[r['repo']] = r['wall_s']
        for repo in set(whole) | set(whole_to) | set(parts):
            if repo in whole:
                tdur[repo] = whole[repo]
            elif repo in parts:
                tdur[repo] = sum(parts[repo].values())
            else:
                tdur[repo] = whole_to[repo]
        for f in d.glob('*.test.stdout'):
            repo = f.name[:-len('.test.stdout')]
            try:
                t = json.loads(f.read_text())['tests']
            except Exception:
                continue
            c = {}
            for x in t:
                c[x['className']] = c.get(x['className'], 0) + x.get('durationMs', 0) / 1000.0
            classes[repo] = c
    return check, tdur, classes


def main():
    a = argparse.ArgumentParser()
    a.add_argument('--lanes', type=int, default=3); a.add_argument('--label', required=True)
    a.add_argument('--bin', required=True); a.add_argument('--hist', required=True)
    a.add_argument('--exclude', default=''); a.add_argument('--procs', default='3'); a.add_argument('--memlimit', default='4GiB')
    a.add_argument('--out', default=os.path.expanduser('~/advisor-broad/out'))
    a.add_argument('--outdir', default=os.path.expanduser('~/advisor-broad/lanes'))
    a.add_argument('--runroot', default=os.path.expanduser('~/advisor-val-run'))
    a.add_argument('--w', default=os.path.expanduser('~/advisor-broad'))
    a.add_argument('--dry', action='store_true')
    a.add_argument('--only', default='', help='comma list: plan only these repos (testing)')
    a.add_argument('--set', action='append', default=[], help="NAME=SRC_CLONE (repeatable; NAME may contain {L}); default BMO sets")
    a.add_argument('--shard3', default=None, help="comma list of repo-name fragments to run as 3 shards ('' for none); default: \"shard3\" list in corpus-config.json (--cfg, else $W/corpus-config.json), else none")
    a.add_argument('--runner', default='run2.py', help='path of run2.py relative to --w')
    a.add_argument('--cfg', default='', help='corpus-config.json: use run3.py and pass --corpus-config (per-repo overlay on the run-clone copies)')
    a.add_argument('--min-split', type=float, default=600.0, help='split a repo whose test wall >= this and one class is >=70%% of it')
    args = a.parse_args()
    global SETS, SHARD3
    if args.cfg:
        args.runner = 'run3.py'
    if args.set:
        SETS = [tuple(x.split('=', 1)) for x in args.set]
    if args.shard3 is None:
        cp = Path(os.path.expanduser(args.cfg or os.path.join(args.w, 'corpus-config.json')))
        args.shard3 = ''
        if cp.exists():
            try:
                lst = json.loads(cp.read_text())['shard3']
            except KeyError:
                sys.exit('plan_lanes.py: %s has no "shard3" key (use [] to opt out)' % cp)
            except ValueError as e:
                sys.exit('plan_lanes.py: %s is not valid JSON: %s' % (cp, e))
            args.shard3 = ','.join(lst)
    SHARD3 = tuple(x for x in args.shard3.split(',') if x)
    out = Path(args.out); N = args.lanes; P = float(args.procs)
    check, tdur, classes = load_hist(out, args.hist)
    excl = [x for x in args.exclude.split(',') if x]
    default = sorted(tdur.values())[len(tdur) // 2] if tdur else 30.0

    units = []   # dict(set, repo, w, kind, arg)
    for s, src in SETS:
        d = Path(os.path.expanduser(src))
        for repo in sorted(p.name for p in d.iterdir() if p.is_dir()):
            if any(x and x in repo for x in excl) or (args.only and repo not in args.only.split(',')):
                continue
            ck, tw = check.get(repo, 5.0), tdur.get(repo, default)
            cl = classes.get(repo, {}); tot = sum(cl.values())
            top = max(cl, key=cl.get) if cl else None
            if any(k in repo for k in SHARD3) and N >= 3:
                for i in range(3):
                    units.append(dict(set=s, repo=repo, w=tw / 3.0 + (ck if i == 0 else 0), kind='shard', arg='%d/3' % i))
            elif tw >= args.min_split and top and tot > 0 and cl[top] >= 0.7 * tw:   # one class is the critical path
                inc = cl[top] / P; rest = max(tot - cl[top], 0) / P
                units.append(dict(set=s, repo=repo, w=inc, kind='inc', arg=top))
                units.append(dict(set=s, repo=repo, w=rest + ck, kind='exc', arg=top))
            else:
                units.append(dict(set=s, repo=repo, w=tw + ck, kind='plain', arg=None))
    units.sort(key=lambda u: -u['w'])
    lanes = [dict(load=0.0, units=[]) for _ in range(N)]
    for u in units:
        # keep parts of one repo on different lanes (they exist to run concurrently)
        cand = [l for l in lanes if not any(x['repo'] == u['repo'] for x in l['units'])] or lanes
        l = min(cand, key=lambda l: l['load'])
        l['load'] += u['w']; l['units'].append(u)

    # Write the complete plan before any lane starts, including lanes that never launch.
    if not args.dry:
        for s, _ in SETS:
            planned = sorted({u['repo'] for u in units if u['set'] == s and
                              (u['kind'] in ('plain', 'exc') or
                               (u['kind'] == 'shard' and u['arg'].startswith('0/')))})
            target = out / lab(s, args.label)
            target.mkdir(parents=True, exist_ok=True)
            for repo in planned:
                (target / (repo + '.check-equivalence.json')).unlink(missing_ok=True)
            (target / 'check-plan.json').write_text(json.dumps(planned) + '\n')

    outdir = Path(args.outdir); outdir.mkdir(parents=True, exist_ok=True)
    print('units=%d total_est=%.0fs lanes=%d' % (len(units), sum(u['w'] for u in units), N))
    for k, l in enumerate(lanes):
        print('lane%d est=%.0fs units=%d top=%s' % (k, l['load'], len(l['units']),
              ', '.join('%s%s:%.0f' % (u['repo'][:22], '' if u['kind'] == 'plain' else '[%s %s]' % (u['kind'], u['arg'][:12]), u['w']) for u in l['units'][:4])))
        lines = ['#!/bin/bash', '# generated by plan_lanes.py; lane %d of %d, est %.0fs' % (k, N, l['load']),
                 'W=%s; BIN=%s; L=%s' % (args.w, args.bin, args.label), 'RUN=%s' % args.runroot, 'mkdir -p $RUN', 'status=0']
        for s, src in SETS:
            us = [u for u in l['units'] if u['set'] == s]
            if not us:
                continue
            repos = []
            for u in us:
                if u['repo'] not in repos:
                    repos.append(u['repo'])
            extra = []
            for u in us:
                if u['kind'] == 'shard':
                    extra += ['--shard', "'%s=%s'" % (u['repo'], u['arg'].replace('/', '/'))]
                elif u['kind'] == 'inc':
                    extra += ['--include-class', "'%s=%s'" % (u['repo'], u['arg'])]
                elif u['kind'] == 'exc':
                    extra += ['--exclude-class', "'%s=%s'" % (u['repo'], u['arg'])]
            slab = lab(s, '$L')
            lines += ['SRC=%s; SL=%s; PD=$RUN/lane%d/run-$SL' % (src, slab, k),
                      'rm -rf $PD; mkdir -p $PD; for r in %s; do cp -R $SRC/$r $PD/; done; for d in $SRC/*/; do b=$(basename $d); [ -e $PD/$b ] || cp -R $SRC/$b $PD/$b; done' % ' '.join("'%s'" % r for r in repos),
                      'python3 $W/%s --bin $BIN --label $SL --clone $PD --out $W/out --procs %s --memlimit %s --no-cache-check '
                      '--test-timeout 3600 --history-from %s %s %s --repos %s 2>$W/$SL-lane%d.err' % (
                          args.runner, args.procs, args.memlimit, lab(s, args.hist), ' '.join(extra) + (' --corpus-config ' + args.cfg if args.cfg else ''), '', ' '.join("'%s'" % r for r in repos), k),
                      'rc=$?; echo "$SL lane%d rc=$rc $(date -u +%%FT%%TZ)"; [ $rc -eq 0 ] || status=1; rm -rf $PD' % k]
        lines.append('echo "lane%d done $(date -u +%%FT%%TZ)"; exit $status' % k)
        if not args.dry:
            p = outdir / ('%s-lane%d.sh' % (args.label, k)); p.write_text('\n'.join(lines) + '\n'); p.chmod(0o755)


if __name__ == '__main__':
    main()
