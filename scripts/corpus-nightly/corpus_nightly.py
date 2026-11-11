#!/usr/bin/env python3
"""Run one corpus tier against its golden files from a fresh checkout of the Glade repo.

The script names no corpus repo. The tier list, overlays and goldens live in a
private config directory that --config points at. It builds Glade from --ref,
builds `glade-tools` from the committed HEAD of the checkout this script lives in (next to the Glade clone, which its go.mod needs), runs
`glade-tools corpus run --tier <tier>`, and writes a summary whose first line is
CORPUS-CLEAN, CORPUS-NEW n in k repos, or CORPUS-INFRA ... . The exit code is
0, 1 or 2 like the runner's.

Example user crontab entry (nightly tier B at 02:00):
  0 2 * * * /usr/bin/python3 $HOME/glade-tools/scripts/corpus-nightly/corpus_nightly.py --state $HOME/corpus-nightly --config $HOME/corpus-config --go $HOME/go/bin/go
"""
import argparse
import datetime
import fcntl
import json
import os
import pathlib
import re
import subprocess
import sys
import tempfile

TIERS = ('A', 'B', 'C')
EXPECTED_FIRST = {0: 'CORPUS-CLEAN', 1: 'CORPUS-NEW ', 2: 'CORPUS-INFRA'}


def preflight(config_dir, tier):
    """Count the entries of the tier (tiers are nested) that can run."""
    cfg = json.loads((config_dir / 'tiers.json').read_text())
    if cfg.get('schemaVersion') != 1 or not isinstance(cfg.get('repos'), list):
        raise ValueError('invalid config')
    rank = TIERS.index(tier)
    names = set()
    selected = blocked = missing = 0
    for repo in cfg['repos']:
        name, repo_tier = repo.get('name'), repo.get('tier')
        if (not isinstance(name, str) or not re.fullmatch(r'[A-Za-z0-9_.-]+', name) or name in ('.', '..')
                or name in names or repo_tier not in TIERS):
            raise ValueError('invalid repository entry')
        names.add(name)
        if TIERS.index(repo_tier) > rank:
            continue
        selected += 1
        if repo.get('blocked'):
            blocked += 1
        elif not (config_dir / 'goldens' / (name + '.json')).is_file():
            missing += 1
    return {'selected': selected, 'blocked': blocked, 'missing': missing}


def runnable(check):
    return check['selected'] - check['blocked'] - check['missing']


def first_line_for(report_path, code):
    """The report's own first line, only when it agrees with the exit code."""
    default = 'CORPUS-INFRA: nightly did not produce a valid report'
    try:
        report = json.loads(report_path.read_text())
    except (OSError, ValueError):
        return default, 2
    first = report.get('first') if isinstance(report, dict) else None
    expected = EXPECTED_FIRST.get(code)
    if (report.get('exitCode') != code or not isinstance(first, str) or not expected
            or '\n' in first or '\r' in first):
        return default, 2
    ok = first == expected if code == 0 else first.startswith(expected)
    return (first, code) if ok else (default, 2)


def run(log, args, stamp):
    """Returns (report path or a ready first line, exit code)."""
    check = preflight(args.config, args.tier)
    if runnable(check) <= 0:
        message = 'CORPUS-INFRA: tier %s has no runnable entries' % args.tier
        print(message, file=log, flush=True)
        return message, 2
    print('tier %s: %d selected, %d blocked, %d without a golden' % (
        args.tier, check['selected'], check['blocked'], check['missing']), file=log, flush=True)
    env = {**os.environ, 'CGO_ENABLED': '1', 'GOMAXPROCS': '2', 'GOMEMLIMIT': '4GiB',
           'GIT_TERMINAL_PROMPT': '0', 'GOTOOLCHAIN': 'local'}

    def command(argv, cwd=None):
        subprocess.run(argv, cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT, check=True)

    tools_root = pathlib.Path(__file__).resolve().parents[2]
    with tempfile.TemporaryDirectory(prefix='run-', dir=args.state / 'work') as tmp:
        root = pathlib.Path(tmp)
        clone = root / 'glade'
        command(['git', 'clone', '--quiet', '--depth=1', '--single-branch', '--branch', args.ref, args.glade_url, str(clone)])
        commit = subprocess.check_output(['git', '-C', str(clone), 'rev-parse', 'HEAD'], env=env, text=True).strip()
        print('glade commit ' + commit, file=log, flush=True)
        glade = root / 'glade-bin'
        tools = root / 'glade-tools-bin'
        command([str(args.go), 'build', '-o', str(glade), './cmd/glade'], cwd=clone)
        # glade-tools replaces the glade module with ../glade, so build it next to the clone.
        tools_src = root / 'glade-tools'
        tools_src.mkdir()
        archive = subprocess.run(['git', '-C', str(tools_root), 'archive', 'HEAD'], env=env, stdout=subprocess.PIPE, check=True)
        subprocess.run(['tar', '-x', '-C', str(tools_src)], input=archive.stdout, check=True)
        command([str(args.go), 'build', '-o', str(tools), './cmd/glade-tools'], cwd=tools_src)
        report = args.state / 'results' / (stamp + '.json')
        result = subprocess.run(
            [str(tools), 'corpus', 'run', '--tier', args.tier, '--glade', str(glade), '--config', str(args.config),
             '--scratch', str(root), '--json', str(report)],
            env=env, stdout=log, stderr=subprocess.STDOUT, check=False)
        return report, result.returncode


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--state', type=pathlib.Path, required=True, help='directory for results, work and the lock')
    parser.add_argument('--config', type=pathlib.Path, required=True, help='private corpus config directory')
    parser.add_argument('--go', type=pathlib.Path, required=True, help='go binary')
    parser.add_argument('--tier', choices=TIERS, default='B')
    parser.add_argument('--ref', default='main', help='branch or tag of the Glade repo to test')
    parser.add_argument('--glade-url', default='https://github.com/glade-sh/glade.git')
    args = parser.parse_args(argv)
    args.state, args.config = args.state.resolve(), args.config.resolve()
    for directory in (args.state, args.state / 'work', args.state / 'results'):
        directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    stamp = datetime.datetime.now().strftime('%Y%m%d-%H%M%S-%f')
    summary = args.state / 'results' / (stamp + '.txt')
    with open(args.state / 'nightly.lock', 'a+') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            summary.write_text('CORPUS-INFRA: another nightly is running\n')
            return 2
        report, code = None, 2
        with open(args.state / 'results' / (stamp + '.log'), 'x', encoding='utf-8') as log:
            try:
                report, code = run(log, args, stamp)
            except (OSError, ValueError, subprocess.SubprocessError) as exc:
                print('CORPUS-INFRA: ' + str(exc), file=log, flush=True)
        if isinstance(report, str):
            first = report
        elif report is not None:
            first, code = first_line_for(report, code)
        else:
            first, code = 'CORPUS-INFRA: nightly did not produce a valid report', 2
        summary.write_text(first + '\n')
        return code


if __name__ == '__main__':
    sys.exit(main())
