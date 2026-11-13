#!/usr/bin/env python3
"""gstat.py WORKDIR KIND:DIRNAME [KIND:DIRNAME ...]  (runs on a gate host, fed by gate-status.sh over ssh stdin)
KIND = priv | pub | b3 | bmo. Prints progress and the diff2 summary against the baselines.
Private (priv) repo names and test names are masked (priv-A, priv-B, ...), counts only. Python 3.9 compatible."""
import glob, json, os, re, subprocess, sys, time
W = sys.argv[1]; OUT = os.path.join(W, 'out')
sys.path.insert(0, W)
import diff2
try:
    import accepted
    LEDGER = accepted.load_ledger(W)
except Exception:
    accepted = None; LEDGER = None


def complete(d):
    try:
        return 'done' in open(os.path.join(d, 'run.log')).read().splitlines()[-1]
    except Exception:
        return False


def newest_other(pattern, me):
    c = [d for d in sorted(glob.glob(os.path.join(OUT, pattern)), key=os.path.getmtime, reverse=True)
         if os.path.basename(d) != me and glob.glob(d + '/*.test.stdout') and complete(d)]
    return c[:1]


def baselines(kind, name):
    if kind == 'pub':
        fixed = [os.path.join(OUT, n) for n in ('FB59FINAL-public', 'B60C88C5-public') if os.path.isdir(os.path.join(OUT, n))]
        return fixed or newest_other('*-public', name)
    if kind == 'priv':
        return newest_other('*-priv', name)
    if kind == 'b3':
        return newest_other('*-B3', name)
    pre = name.split('-')[0]
    return newest_other(pre + '-*', name)


def running(name):
    r = subprocess.run(['pgrep', '-f', 'run[23]?[.]py .*--label %s( |$)' % re.escape(name)], capture_output=True, text=True)
    return len(r.stdout.split())


for spec in sys.argv[2:]:
    kind, name = spec.split(':', 1)
    d = os.path.join(OUT, name)
    if not os.path.isdir(d):
        print('%-5s %-28s not started' % (kind, name)); continue
    log = open(os.path.join(d, 'run.log')).read().splitlines() if os.path.exists(os.path.join(d, 'run.log')) else []
    expected = set()
    for l in log:
        m = re.search(r'repos=(.*)$', l)
        if ' label=' in l and m:
            expected |= set(m.group(1).split())
    done = {os.path.basename(f)[:-len('.test.stdout')] for f in glob.glob(d + '/*.test.stdout')}
    parts = {os.path.basename(f).split('.test.')[0] for f in glob.glob(d + '/*.test.*.part')} - done
    ndone = sum(1 for l in log if l.endswith(' done') or l == 'done' or l.split(' ')[-1] == 'done')
    nlane = sum(1 for l in log if ' label=' in l)
    mask = {}
    if kind == 'priv':
        for i, r in enumerate(sorted(expected | done)):
            mask[r] = 'priv-%s' % chr(65 + i)
    mk = lambda r: mask.get(r, r)
    last = log[-1] if log else ''
    for r, m in mask.items():
        last = last.replace(r, m)
    state = 'FINISHED' if nlane and ndone >= nlane and not running(name) else ('running x%d' % running(name))
    print('%-5s %-28s %s  repos %d/%d%s  %s' % (kind, name, state, len(done), len(expected) or len(done),
          ('  (+%d partial)' % len(parts)) if parts else '', time.strftime('%H:%MZ', time.gmtime(os.path.getmtime(os.path.join(d, 'run.log') if log else d)))))
    print('      last: %s' % last[:150])
    # The planner records all lanes before launch; direct runners also log their plan.
    checks = set()
    try:
        with open(os.path.join(d, 'check-plan.json')) as f:
            checks.update(json.load(f))
    except (OSError, ValueError, TypeError):
        pass
    for line in log:
        if ' check plan=' in line:
            try:
                checks.update(json.loads(line.split(' check plan=', 1)[1]))
            except (ValueError, TypeError):
                pass
        m = re.search(r'^\S+ (.+) check start$', line)
        if m:
            checks.add(m.group(1))
    verdicts = {'match': 0, 'mismatch': 0, 'incomplete': 0}
    for repo in checks:
        try:
            with open(os.path.join(d, repo + '.check-equivalence.json')) as f:
                verdict = json.load(f).get('verdict')
            verdicts[verdict if verdict in verdicts else 'incomplete'] += 1
        except (OSError, ValueError):
            verdicts['incomplete'] += 1
    print('      check equivalence: match=%d mismatch=%d incomplete=%d%s' % (
        verdicts['match'], verdicts['mismatch'], verdicts['incomplete'],
        ' (no checks planned or observed)' if not checks else ''))
    for b in baselines(kind, name):
        bn = os.path.basename(b)
        tot_new = tot_fixed = tot_to = tot_acc = 0; bad = []; cmp_n = 0; longs = []
        for r in sorted(done):
            bf = os.path.join(b, r + '.test.stdout')
            if not os.path.exists(bf):
                continue
            try:
                _, fc, tc, _ = diff2.load(bf); _, fr, tr, _ = diff2.load(os.path.join(d, r + '.test.stdout'))
            except Exception as e:
                bad.append('%s(unreadable)' % mk(r)); continue
            tn = getattr(diff2, 'timeout_new', lambda a, b: {})(tc, tr)
            if LEDGER:      # ROAD-408: known-long class cap and accepted-new ledger (accepted-ledger.json)
                tn, _, linfo = accepted.long_filter(LEDGER, r, tn, tr)
                if linfo:
                    longs.append('%s %d/%d' % (mk(r), linfo[0], linfo[1]) + (' OVER CAP' if linfo[0] > linfo[1] else ''))
            new = (set(fr) | set(tn)) - set(fc) - set(tc); fixed = (set(fc) - set(fr)) - set(tr)
            if LEDGER and new:
                acc = accepted.accepted_new(LEDGER, r, new, os.path.join(d, r + '.test.stdout')); new -= acc; tot_acc += len(acc)
            cmp_n += 1; tot_new += len(new); tot_fixed += len(fixed); tot_to += len(tr)
            if new:
                bad.append('%s:%d' % (mk(r), len(new)))
        print('      vs %-26s compared %d repos: new=%d fixed=%d timeout=%d%s%s' % (bn, cmp_n, tot_new, tot_fixed, tot_to,
              ('   accepted=%d' % tot_acc) if tot_acc else '', ('   NEW IN: ' + ', '.join(bad[:30])) if bad else ''))
        if longs:
            print('      known-long canceled (accepted-ledger): %s' % ', '.join(longs))
