#!/usr/bin/env python3
"""run3.py: run2.py plus --corpus-config FILE (per-repo overlay, corpus_cfg.py); identical to run2.py when the flag is absent.
run2.py: corpus gate runner v2 (d12). Same output layout as run.py:
    <out>/<label>/<repo>.{check,test}.{stdout,stderr}, <repo>.check.json, results.jsonl, run.log

Differences from run.py (all opt-in except the speed items):
  * polls the child every 1 s (guard checks every 5 s)
  * overlaps the next repo's `check` with the current repo's `test` (--no-overlap to disable)
  * works on macOS (/usr/bin/time -l) and Linux (/usr/bin/time -v, byte-correct peak RSS)
  * --shard REPO=I[,J..]/N     run class shard(s) I of N for REPO (glade test --shard-count/--shard-index)
  * --include-class REPO=A,B   run only these classes of REPO (part 'inc')
  * --exclude-class REPO=A,B   run everything but these classes of REPO (part 'exc'; list via --write-class-shards)
  * --repo-procs REPO=N        worker count override for REPO
  * --history-from LABEL|DIR   seed --duration-history for each repo from <out>/<LABEL>/<repo>.test.stdout
  * --duration-history FILE|DIR  explicit {class: ms} json (a file for every repo, or DIR/<repo>.history.json)
Partial runs (shard/include/exclude) write <repo>.test.<part>.part (+ .stderr); when every expected part
of a repo is present in the label dir they are merged into <repo>.test.stdout (merge_shards.py).
Python 3.9 compatible.
"""
import argparse, json, os, platform, re, signal, subprocess, sys, threading, time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import merge_shards
import check_equivalence
import corpus_cfg
import incremental

IS_LINUX = platform.system() == 'Linux'
LOCK = threading.Lock()


def pressure():
    if IS_LINUX:
        return 1
    try:
        level = int(subprocess.run(['sysctl', '-n', 'kern.memorystatus_vm_pressure_level'],
                                   capture_output=True, text=True, timeout=5).stdout.strip())
    except Exception:
        return 1
    # macOS can stay at level 2 (warn) long after a swap burst even with plenty
    # free; treat warn as calm when at least 40% of memory is free, or a paused
    # job never resumes.
    if level == 2:
        try:
            out = subprocess.run(['memory_pressure'], capture_output=True, text=True, timeout=10).stdout
            m = re.search(r'free percentage:\s*(\d+)%', out)
            if m and int(m.group(1)) >= 40:
                return 1
        except Exception:
            pass
    return level


_YIELD = [0.0, False]


def yield_to_host_jobs():
    """Cached 3 s, one caller at a time. (Two guarded() threads used to see each other's `pgrep -f` command
    line and both yield forever: the patterns below use [x] brackets so a sibling pgrep never matches.)"""
    with LOCK:
        if time.monotonic() - _YIELD[0] > 3:
            _YIELD[1] = _yield_uncached(); _YIELD[0] = time.monotonic()
        return _YIELD[1]


def _yield_uncached():
    """True while a campaign job owns the host (same rules as run.py on each platform)."""
    if IS_LINUX:
        if (Path.home() / 'advisor-broad/PAUSE').exists():
            return True
        try:
            return subprocess.run(['pgrep', '-f', 'TRAIN-[l]ane|TRAIN-[s]moke|glade-roadmap-runner/[w]t/'],
                                  capture_output=True, timeout=5).returncode == 0
        except Exception:
            return False
    running = Path.home() / '.local/share/glade-family-cache/queue/running'
    try:
        if any('TRAIN-' in p.name for p in running.iterdir()):
            return True
    except OSError:
        pass
    try:
        return subprocess.run(['pgrep', '-f', 'test-slot/[g]uard.py entry'], capture_output=True, timeout=5).returncode == 0
    except Exception:
        return False


def safe_killpg(pid, sig):
    try:
        os.killpg(pid, sig)
        return True
    except (ProcessLookupError, PermissionError):
        return False


def guarded(cmd, env, timeout, stdout, stderr, log):
    time_flag = '-v' if IS_LINUX else '-l'
    proc = subprocess.Popen(['/usr/bin/time', time_flag, 'nice', '-n', '15', *cmd], env=env, stdout=stdout,
                            stderr=stderr, start_new_session=True)
    start = time.monotonic(); paused = False; paused_s = 0.0; calm_since = None; t_pause = 0.0; last_guard = 0.0
    while True:
        try:
            proc.wait(timeout=1)
            break
        except subprocess.TimeoutExpired:
            pass
        now = time.monotonic()
        if now - last_guard >= 5:
            last_guard = now
            level = pressure()
            if yield_to_host_jobs():
                level = max(level, 9)
            if os.environ.get('GATE_DEBUG'):
                log('  guard level=%s paused=%s calm_since=%s' % (level, paused, calm_since))
            if level != 1 and not paused:
                safe_killpg(proc.pid, signal.SIGSTOP); paused = True; t_pause = now; calm_since = None
                log('  pause: %s' % ('host job running' if level == 9 else 'memory pressure level %d' % level))
            elif paused and level == 1:
                calm_since = calm_since or now
                if now - calm_since >= 30:
                    safe_killpg(proc.pid, signal.SIGCONT); paused = False; paused_s += now - t_pause
                    log('  resume after %.0fs' % (now - t_pause))
            elif paused:
                calm_since = None
        now = time.monotonic()
        if now - start - paused_s - ((now - t_pause) if paused else 0) > timeout:
            if paused:
                safe_killpg(proc.pid, signal.SIGCONT)
            safe_killpg(proc.pid, signal.SIGTERM)
            time.sleep(10)
            if proc.poll() is None:
                safe_killpg(proc.pid, signal.SIGKILL)
            proc.wait()
            return 'timeout', time.monotonic() - start - paused_s, paused_s
    return proc.returncode, time.monotonic() - start - paused_s, paused_s


def peak_rss(stderr_path):
    """Peak RSS in bytes from /usr/bin/time output (macOS -l: bytes; GNU -v: kbytes)."""
    try:
        lines = Path(stderr_path).read_text(errors='replace').splitlines()
    except OSError:
        return None
    for line in lines:
        low = line.lower()
        if 'maximum resident set size' in low:
            try:
                if 'kbytes' in low:
                    return int(low.split(':')[-1].split()[0]) * 1024
                return int(line.split()[0])
            except (ValueError, IndexError):
                return None
    return None


def kv(items, what):
    d = {}
    for item in items:
        if '=' not in item:
            sys.exit('%s: expected REPO=VALUE, got %r' % (what, item))
        k, v = item.split('=', 1)
        d[k] = v
    return d


def parse_shards(items):
    """REPO=I[,J]/N or REPO=N (== 0/N) -> {repo: ([idx...], N)}"""
    out = {}
    for repo, spec in kv(items, '--shard').items():
        if '/' in spec:
            idx, n = spec.split('/', 1)
            idxs = [int(x) for x in idx.split(',')]
        else:
            idxs, n = [0], spec
        n = int(n)
        if any(i < 0 or i >= n for i in idxs):
            sys.exit('--shard %s: index out of range for %d shards' % (repo, n))
        out[repo] = (idxs, n)
    return out


def main():
    a = argparse.ArgumentParser()
    a.add_argument('--bin', required=True); a.add_argument('--label', required=True)
    a.add_argument('--clone', required=True); a.add_argument('--out', required=True)
    a.add_argument('--repos', nargs='*'); a.add_argument('--check-timeout', type=int, default=900)
    a.add_argument('--test-timeout', type=int, default=2400); a.add_argument('--skip-tests', action='store_true')
    a.add_argument('--shard', action='append', default=[])
    a.add_argument('--include-class', action='append', default=[])
    a.add_argument('--exclude-class', action='append', default=[])
    a.add_argument('--repo-procs', action='append', default=[])
    a.add_argument('--history-from'); a.add_argument('--duration-history')
    a.add_argument('--no-cache-check', action='store_true', help='pass --no-cache to glade check')
    a.add_argument('--no-overlap', action='store_true', help='do not overlap next check with current test')
    a.add_argument('--corpus-config', help='JSON overlay (corpus_cfg.py) applied to each repo COPY in --clone before check/test')
    a.add_argument('--unit-timeout-history', help='LABEL|DIR whose results.jsonl sets each repo timeout (train mode)')
    a.add_argument('--unit-timeout-factor', type=float, default=3.0)
    a.add_argument('--unit-timeout-min', type=int, default=300)
    a.add_argument('--unit-timeout-known-timeout', type=int, default=1200)
    a.add_argument('--procs', default='3'); a.add_argument('--memlimit', default='4GiB')
    args = a.parse_args()
    clone, out = Path(args.clone), Path(args.out) / args.label
    out.mkdir(parents=True, exist_ok=True)
    repos = args.repos or sorted((p.name for p in clone.iterdir() if p.is_dir()),
                                 key=lambda n: sum(1 for _ in (clone / n).rglob('*.cls')))
    env = dict(os.environ, GOMAXPROCS=args.procs, GOMEMLIMIT=args.memlimit)
    results = out / 'results.jsonl'
    unit_hist = {}
    if args.unit_timeout_history:
        src = Path(args.unit_timeout_history)
        unit_hist = incremental.history_walls(src if src.is_dir() else Path(args.out) / args.unit_timeout_history)

    def timeout_for(repo, phase, default):
        if not args.unit_timeout_history:
            return default
        return incremental.unit_timeout(unit_hist.get((repo, phase)), args.unit_timeout_factor,
                                        args.unit_timeout_min, default, args.unit_timeout_known_timeout)

    def log(msg):
        line = time.strftime('%H:%M:%SZ', time.gmtime()) + ' ' + msg
        with LOCK:
            print(line, flush=True)
            with open(out / 'run.log', 'a') as fh:
                fh.write(line + '\n')

    shards = parse_shards(args.shard)
    include = {k: v.split(',') for k, v in kv(args.include_class, '--include-class').items()}
    exclude = {k: v.split(',') for k, v in kv(args.exclude_class, '--exclude-class').items()}
    rprocs = kv(args.repo_procs, '--repo-procs')
    log('label=%s bin=%s procs=%s repos=%s' % (args.label, args.bin, args.procs, ' '.join(repos)))
    planned = [repo for repo in repos if (repo not in shards or 0 in shards[repo][0])
               and (repo not in include or repo in exclude or repo in shards)]
    log('check plan=' + json.dumps(planned))
    for repo in planned:
        (out / (repo + '.check-equivalence.json')).unlink(missing_ok=True)
    if args.unit_timeout_history:
        for repo in repos:
            log('%s unit timeouts check=%ds test=%ds' % (repo, timeout_for(repo, 'check', args.check_timeout),
                                                          timeout_for(repo, 'test', args.test_timeout)))

    def history_for(repo):
        """Path of a {class: ms} file for REPO, or None. Deterministic content so lanes agree."""
        if args.duration_history:
            p = Path(args.duration_history)
            if p.is_dir():
                p = p / (repo + '.history.json')
            return str(p) if p.exists() else None
        if args.history_from:
            src = Path(args.history_from)
            if not src.is_dir():
                src = Path(args.out) / args.history_from
            prev = src / (repo + '.test.stdout')
            if not prev.exists():
                return None
            hp = out / (repo + '.history.json')
            try:
                hist = merge_shards.class_durations(prev)
                tmp = out / ('.%s.history.%d.tmp' % (repo, os.getpid()))
                tmp.write_text(json.dumps(hist, sort_keys=True))
                os.replace(tmp, hp)
                return str(hp)
            except Exception as error:
                log('%s history unusable: %s' % (repo, type(error).__name__))
        return None

    def all_classes(repo, root):
        d = out / ('.classes-%s-%d' % (repo, os.getpid()))
        d.mkdir(exist_ok=True)
        subprocess.run([args.bin, 'test', '--project', str(root), '--shard-count', '1', '--write-class-shards', str(d)],
                       env=env, capture_output=True, timeout=600)
        names = []
        for f in sorted(d.glob('*.txt')):
            names += [x.strip() for x in f.read_text().splitlines() if x.strip()]
        for f in d.glob('*'):
            f.unlink()
        d.rmdir()
        return names

    # per-repo overlay on the copy (never the source clone); dep test classes are subtracted from the selection
    cfg_exclude = {}
    if args.corpus_config:
        ccfg = corpus_cfg.load_config(args.corpus_config)
        for repo in repos:
            if not (clone / repo).is_dir():
                continue
            try:
                rec, ex = corpus_cfg.apply_overlay(repo, clone / repo, ccfg)
            except Exception as error:
                rec, ex = {'repo': repo, 'applied': False, 'reason': 'overlay error: %s: %s' % (type(error).__name__, error)}, set()
            if repo in ccfg['repos']:
                (out / (repo + '.config-applied.json')).write_text(json.dumps(rec, indent=1, sort_keys=True))
                log('%s overlay applied=%s deps=%s features=%s excl_classes=%d%s' % (repo, rec.get('applied'),
                    ','.join('%s:%s' % (d['name'], d['status']) for d in rec.get('deps', [])), ','.join(rec.get('features', [])),
                    len(ex), (' reason=' + rec['reason']) if rec.get('reason') else ''))
            if ex:
                cfg_exclude[repo] = ex

    # job: repo, part (None | 's0of3' | 'inc' | 'exc'), extra test args, check?
    jobs = []
    for repo in repos:
        root = clone / repo
        hist = history_for(repo)
        base = ['--duration-history', hist] if hist else []
        if repo in shards:
            if repo in cfg_exclude:
                log('%s WARNING: overlay deps are not excluded from shard runs' % repo)
            idxs, n = shards[repo]
            for i in idxs:
                jobs.append(dict(repo=repo, part='s%dof%d' % (i, n), shard=(i, n), check=(i == 0),
                                 targs=base + ['--shard-count', str(n), '--shard-index', str(i)]))
        elif repo in include or repo in exclude:
            if repo in include:
                cf = out / (repo + '.inc.classes'); cf.write_text('\n'.join(include[repo]) + '\n')
                jobs.append(dict(repo=repo, part='inc', check=False, targs=base + ['--class-file', str(cf)]))
            if repo in exclude:
                names = [c for c in all_classes(repo, root) if c not in set(exclude[repo]) | cfg_exclude.get(repo, set())]
                cf = out / (repo + '.exc.classes'); cf.write_text('\n'.join(names) + '\n')
                jobs.append(dict(repo=repo, part='exc', check=True, targs=base + ['--class-file', str(cf)]))
        elif repo in cfg_exclude:     # overlay added dependency sources: run only the repo's own test classes
            names = [c for c in all_classes(repo, root) if c not in cfg_exclude[repo]]
            cf = out / (repo + '.cfg.classes'); cf.write_text('\n'.join(names) + '\n')
            log('%s overlay test selection: %d classes (dependency classes excluded)' % (repo, len(names)))
            jobs.append(dict(repo=repo, part=None, check=True, targs=base + ['--class-file', str(cf)]))
        else:
            jobs.append(dict(repo=repo, part=None, check=True, targs=base))

    def expected_parts(job):
        if job['part'] is None:
            return None
        if job.get('shard'):
            n = job['shard'][1]
            return ['s%dof%d' % (i, n) for i in range(n)]
        return ['inc', 'exc']

    def record(job, phase, code, wall, paused, se, so_path):
        repo, part = job['repo'], job['part']
        row = {'repo': repo, 'phase': phase, 'label': args.label, 'exit': code, 'wall_s': round(wall, 1),
               'paused_s': round(paused, 1), 'peak_rss': peak_rss(se), 'part': part if phase == 'test' else None,
               'shard': ('%d/%d' % job['shard']) if (phase == 'test' and job.get('shard')) else None}
        try:
            src = out / (repo + '.check.json') if phase == 'check' else so_path
            data = json.loads(src.read_text())
            row['summary'] = data.get('summary'); row['status'] = data.get('status')
        except Exception as error:
            row['summary'] = None; row['parse_error'] = type(error).__name__
        with LOCK:
            with open(results, 'a') as fh:
                fh.write(json.dumps(row) + '\n')
        log('%s %s%s exit=%s wall=%.0fs rss=%s summary=%s' % (repo, phase, ('[%s]' % part) if (part and phase == 'test') else '',
            code, wall, (row['peak_rss'] or 0) // 2**20, json.dumps(row['summary'])[:200]))
        return row

    check_failed = []
    test_timed_out = []

    def run_check(job):
        repo = job['repo']; root = clone / repo
        cmd = [args.bin, 'check', '--project', str(root), '--format', 'json', '--output', str(out / (repo + '.check.json')),
               '--no-progress', *(['--no-cache'] if args.no_cache_check else [])]
        so = out / (repo + '.check.stdout'); se = out / (repo + '.check.stderr')
        for suffix in ('.check.json', '.check-equivalence.json', '.check-equivalence.diff'):
            (out / (repo + suffix)).unlink(missing_ok=True)
        # Identical uncached inputs are essential to compare worker counts.
        if '--no-cache' not in cmd:
            cmd.append('--no-cache')
        log('%s check start' % repo)
        with open(so, 'w') as fo, open(se, 'w') as fe:
            code, wall, paused = guarded(cmd, env, timeout_for(repo, 'check', args.check_timeout), fo, fe, log)
        record(job, 'check', code, wall, paused, se, so)
        serial = out / (repo + '.check.serial.json')
        parallel = out / (repo + '.check.json')
        receipt_path = out / (repo + '.check-equivalence.json')
        diff_path = out / (repo + '.check-equivalence.diff')
        # Remove stale artifacts before either result can be interpreted.
        # The parallel output was removed before the first invocation below.
        serial.unlink(missing_ok=True)
        serial_cmd = list(cmd)
        serial_cmd[serial_cmd.index('--output') + 1] = str(serial)
        with open(out / (repo + '.check.serial.stdout'), 'w') as fo, open(out / (repo + '.check.serial.stderr'), 'w') as fe:
            serial_code, _, _ = guarded(serial_cmd, dict(env, GOMAXPROCS='1'), timeout_for(repo, 'check', args.check_timeout), fo, fe, log)
        receipt = check_equivalence.compare(parallel, serial, code, serial_code, args.procs, diff_path)
        receipt_path.write_text(json.dumps(receipt, indent=2) + '\n')
        log('%s check equivalence=%s' % (repo, receipt['verdict']))
        if receipt['verdict'] != 'match':
            check_failed.append(repo)

    def run_test(job):
        repo, part = job['repo'], job['part']; root = clone / repo
        procs = rprocs.get(repo, args.procs)
        cmd = [args.bin, 'test', '--project', str(root), '--json', '--no-serve', '--no-progress',
               '--parallelism', procs, '--gc-aggressive', '--test-timeout', '0', *job['targs']]
        if part is None:
            so = out / (repo + '.test.stdout'); se = out / (repo + '.test.stderr')
        else:
            so = out / ('%s.test.%s.part' % (repo, part)); se = out / ('%s.test.%s.stderr' % (repo, part))
        log('%s test%s start' % (repo, ('[%s]' % part) if part else ''))
        tenv = dict(env, GOMAXPROCS=procs) if procs != args.procs else env
        with open(so, 'w') as fo, open(se, 'w') as fe:
            code, wall, paused = guarded(cmd, tenv, timeout_for(repo, 'test', args.test_timeout), fo, fe, log)
        record(job, 'test', code, wall, paused, se, so)
        if args.unit_timeout_history and code == 'timeout':
            test_timed_out.append((repo, part))
        exp = expected_parts(job)
        if exp and all((out / ('%s.test.%s.part' % (repo, p))).exists() for p in exp):
            try:
                merge_shards.merge_parts(out, repo, exp)
                log('%s merged %s' % (repo, ','.join(exp)))
            except Exception as error:
                log('%s merge deferred: %s' % (repo, error))

    # pipeline: check(next needing check) overlaps test(current)
    pool = ThreadPoolExecutor(max_workers=1)
    futures = {}

    def submit_next_check(start):
        for j in range(start, len(jobs)):
            if jobs[j]['check']:
                if j not in futures:
                    futures[j] = pool.submit(run_check, jobs[j])
                return

    if args.no_overlap:
        for job in jobs:
            if job['check']:
                run_check(job)
            if not args.skip_tests:
                run_test(job)
    else:
        submit_next_check(0)
        for i, job in enumerate(jobs):
            if job['check']:
                if i not in futures:
                    submit_next_check(i)
                futures[i].result()
            if not args.skip_tests:
                submit_next_check(i + 1)
                run_test(job)
            else:
                submit_next_check(i + 1)
    pool.shutdown(wait=True)
    log('done')
    if check_failed or test_timed_out:
        sys.exit(1)


if __name__ == '__main__':
    main()
