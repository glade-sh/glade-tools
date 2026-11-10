#!/usr/bin/env python3
"""Run an observational, private tier-B baseline from a fresh main checkout."""
import argparse
import contextlib
import datetime
import fcntl
import hashlib
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile
from zoneinfo import ZoneInfo


def preflight(config_dir):
    cfg = json.loads((config_dir / 'tiers.json').read_text())
    if cfg.get('schemaVersion') != 1 or not isinstance(cfg.get('repos'), list):
        raise ValueError('invalid config')
    names = set()
    selected = blocked = missing = 0
    for repo in cfg['repos']:
        name, tier = repo.get('name'), repo.get('tier')
        if not isinstance(name, str) or not re.fullmatch(r'[A-Za-z0-9_.-]+', name) or name in ('.', '..') or name in names or tier not in ('A', 'B', 'C'):
            raise ValueError('invalid repository entry')
        names.add(name)
        if tier not in ('A', 'B'):
            continue
        selected += 1
        if repo.get('blocked'):
            blocked += 1
        elif not (config_dir / 'goldens' / (name + '.json')).is_file():
            missing += 1
    if not selected:
        raise ValueError('empty tier B')
    return {'selected': selected, 'blocked': blocked, 'missing': missing}


def has_runnable_entries(check):
    return check['selected'] > check['blocked'] + check['missing']


def config_fingerprint(root):
    digest = hashlib.sha256()
    paths = [root / 'tiers.json', *sorted((root / 'goldens').glob('*.json'))]
    for path in paths:
        data = path.read_bytes()
        digest.update(path.relative_to(root).as_posix().encode() + b'\0')
        digest.update(hashlib.sha256(data).digest())
    return digest.hexdigest()


def run(log, state, config, runner, go, ref):
    check = preflight(config)
    if not has_runnable_entries(check):
        print('CORPUS-INFRA: tier B has no runnable entries', file=log, flush=True)
        return 2
    if check['blocked'] or check['missing']:
        print('tier B includes %d blocked and %d missing-golden entries' % (check['blocked'], check['missing']), file=log, flush=True)
    with tempfile.TemporaryDirectory(prefix='run-', dir=state / 'work') as tmp:
        root = pathlib.Path(tmp)
        clone = root / 'glade'
        frozen_config = root / 'config'
        before = config_fingerprint(config)
        shutil.copytree(config, frozen_config)
        frozen_hash = config_fingerprint(frozen_config)
        after = config_fingerprint(config)
        frozen_check = preflight(frozen_config)
        if before != frozen_hash or before != after or frozen_check != check or not has_runnable_entries(frozen_check):
            print('CORPUS-INFRA: configuration changed during snapshot', file=log, flush=True)
            return 2
        print('configuration sha256 ' + frozen_hash, file=log, flush=True)
        print('runner sha256 ' + hashlib.sha256(runner.read_bytes()).hexdigest(), file=log, flush=True)
        env = {**os.environ, 'CGO_ENABLED': '1', 'GOMAXPROCS': '2', 'GOMEMLIMIT': '4GiB',
               'GIT_TERMINAL_PROMPT': '0', 'GOTOOLCHAIN': 'local'}
        def command(args, cwd=None):
            subprocess.run(args, cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
        command(['git', 'clone', '--quiet', '--depth=1', '--single-branch', '--branch', ref,
                 'https://github.com/glade-sh/glade.git', str(clone)])
        commit = subprocess.check_output(['git', '-C', str(clone), 'rev-parse', 'HEAD'], env=env, text=True).strip()
        print('glade commit ' + commit, file=log, flush=True)
        binary = root / 'glade-bin'
        command([str(go), 'build', '-o', str(binary), './cmd/glade'], cwd=clone)
        report = state / 'results' / (log.name.removesuffix('.log') + '.json')
        result = subprocess.run([str(runner), str(binary), str(frozen_config), str(report)], env=env,
                                stdout=log, stderr=subprocess.STDOUT, check=False)
        return result.returncode


def write_summary(path, report_path, code):
    first = 'CORPUS-INFRA: nightly did not produce a valid report'
    effective = 2
    if report_path.is_file():
        try:
            report = json.loads(report_path.read_text())
            candidate = report.get('first')
            expected = {0: 'CORPUS-CLEAN', 1: 'CORPUS-NEW ', 2: 'CORPUS-INFRA '}.get(code)
            if (report.get('exitCode') == code and isinstance(candidate, str) and expected and
                    '\n' not in candidate and '\r' not in candidate and
                    (candidate == expected if code == 0 else candidate.startswith(expected))):
                first, effective = candidate, code
        except (OSError, ValueError, TypeError):
            pass
    with path.open('x', encoding='utf-8') as summary:
        summary.write(first + '\n')
    return effective


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--state', type=pathlib.Path, required=True)
    parser.add_argument('--config', type=pathlib.Path, required=True)
    parser.add_argument('--runner', type=pathlib.Path, required=True)
    parser.add_argument('--go', type=pathlib.Path, required=True)
    parser.add_argument('--ref', choices=('main', 'v0.3.5'), default='main')
    args = parser.parse_args()
    state = args.state.resolve()
    for directory in (state, state / 'work', state / 'results'):
        directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    with open(state / 'nightly.lock', 'a+') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            print('CORPUS-INFRA: another nightly is running', file=sys.stderr)
            return 2
        stamp = datetime.datetime.now(ZoneInfo('America/Los_Angeles')).strftime('%Y%m%d-%H%M%S-%f')
        with open(state / 'results' / (stamp + '.log'), 'x', encoding='utf-8') as log:
            try:
                code = run(log, state, args.config.resolve(), args.runner.resolve(strict=True),
                           args.go.resolve(strict=True), args.ref)
            except (OSError, ValueError, json.JSONDecodeError, subprocess.SubprocessError) as exc:
                print('CORPUS-INFRA: ' + str(exc), file=log, flush=True)
                code = 2
        return write_summary(state / 'results' / (stamp + '.txt'),
                             state / 'results' / (stamp + '.json'), code)


if __name__ == '__main__':
    sys.exit(main())
