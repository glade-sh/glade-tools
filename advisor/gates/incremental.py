#!/usr/bin/env python3
"""incremental.py: per-unit timeout helpers for the per-train corpus gate (ROAD-420), used by run3.py.

Train mode narrows scope and shortens timeouts. It never skips a repo and never records an accepted state.

  history_walls(dir)   {(repo, phase): value} from a finished run's results.jsonl. value is a float wall (clean
                       whole-repo or complete-shard history), TIMEOUT (the unit timed out), or None (unusable)
  unit_timeout(...)    seconds for one unit: clamp(factor * wall, floor, default) for clean history,
                       min(known_timeout, default) for timeout history, default otherwise

CLI: incremental.py scope OUTDIR   writes train-ran.json: what a train gate actually ran, from results.jsonl
"""
import argparse
import json
import math
import os
import re
import sys
from pathlib import Path

TIMEOUT = 'timeout'


def history_walls(directory):
    """Whole-repo rows win; complete clean shard rows are summed (serial-equivalent); a timed-out whole-repo
    row gives TIMEOUT; mixed, failed or incomplete rows give None."""
    out, parts, invalid = {}, {}, set()
    try:
        lines = (Path(directory) / 'results.jsonl').read_text().splitlines()
    except OSError:
        return out
    for line in lines:
        try:
            r = json.loads(line)
            key = (r['repo'], r['phase'])
            if not all(isinstance(x, str) for x in key):
                continue
        except (ValueError, KeyError, TypeError, AttributeError):
            continue
        try:
            wall = float(r['wall_s'])
            if not math.isfinite(wall) or wall < 0:
                raise ValueError('invalid wall')
        except (ValueError, KeyError, TypeError):
            invalid.add(key)
            continue
        part = r.get('part')
        if part:
            parts.setdefault(key, []).append((part, wall, r.get('exit')))
        elif key in out:
            out[key] = None  # duplicate whole rows do not identify one complete run
        elif r.get('exit') == 'timeout':
            out[key] = TIMEOUT
        elif r.get('exit') != 0:
            out[key] = None
        else:
            out[key] = wall
    for key, rows in parts.items():
        if key in out:
            out[key] = None  # mixed whole and partial rows cannot establish a complete run
            continue
        matches = [re.fullmatch(r's(\d+)of(\d+)', str(part)) for part, _, _ in rows]
        if (all(matches) and all(code == 0 for _, _, code in rows)
                and len({int(m.group(2)) for m in matches}) == 1
                and sorted(int(m.group(1)) for m in matches) == list(range(int(matches[0].group(2))))):
            out[key] = sum(wall for _, wall, _ in rows)
        else:
            out[key] = None
    for key in invalid:
        out[key] = None
    return out


def unit_timeout(hist, factor, floor, default, known_timeout=1200):
    """hist is a float wall, TIMEOUT or None. The result never exceeds default."""
    if hist == TIMEOUT:
        return int(min(known_timeout, default))
    if isinstance(hist, (int, float)) and not isinstance(hist, bool):
        return int(min(default, max(floor, factor * hist)))
    return int(default)


def _complete_rows(rows):
    """True when the rows of one phase cover the whole unit and none timed out."""
    # A unit ran only if it finished and produced a parsed result: a timeout or a crash with no JSON did not.
    if not rows or any(r.get('exit') == 'timeout' or r.get('summary') is None for r in rows):
        return False
    parts = [r.get('part') for r in rows]
    if None in parts:
        return len(rows) == 1
    shards = [re.fullmatch(r's(\d+)of(\d+)', str(p)) for p in parts]
    if all(shards):
        n = int(shards[0].group(2))
        return {int(m.group(2)) for m in shards} == {n} and sorted(int(m.group(1)) for m in shards) == list(range(n))
    return sorted(parts) == ['exc', 'inc']


def scope_report(directory):
    """What a train gate actually ran, from finished result rows. train-scope.json is only the plan."""
    d = Path(directory)
    scope = json.loads((d / 'train-scope.json').read_text())
    planned = list(scope.get('planned', []))
    try:
        check_plan = set(json.loads((d / 'check-plan.json').read_text()))
    except (OSError, ValueError):
        check_plan = set(planned)
    rows = {}
    try:
        for line in (d / 'results.jsonl').read_text().splitlines():
            try:
                r = json.loads(line)
                rows.setdefault((r['repo'], r['phase']), []).append(r)
            except (ValueError, KeyError, TypeError, AttributeError):
                continue
    except OSError:
        pass
    ran = []
    for repo in planned:
        if repo in check_plan:
            chk = rows.get((repo, 'check'), [])
            if len(chk) != 1 or not _complete_rows(chk):
                continue
        if _complete_rows(rows.get((repo, 'test'), [])):
            ran.append(repo)
    unfinished = sorted(set(planned) - set(ran))
    not_planned = list(scope.get('not_planned', []))
    return {'planned': planned, 'ran': sorted(ran), 'not_run': sorted(unfinished + not_planned),
            'planned_not_finished': unfinished, 'not_planned': not_planned,
            'config_sha256': scope.get('config_sha256')}


def main():
    a = argparse.ArgumentParser(description=__doc__.split('\n')[0])
    sub = a.add_subparsers(dest='cmd', required=True)
    sub.add_parser('scope', help='write train-ran.json (actual execution) next to train-scope.json in an out directory').add_argument('dir')
    args = a.parse_args()
    d = Path(args.dir)
    try:
        report = scope_report(d)
    except (OSError, ValueError) as error:
        sys.exit('scope: %s' % error)
    tmp = d / '.train-ran.json.tmp'
    tmp.write_text(json.dumps(report, indent=1) + '\n')
    os.replace(tmp, d / 'train-ran.json')
    print('ran=%d not_run=%d (planned_not_finished=%d not_planned=%d)' % (
        len(report['ran']), len(report['not_run']), len(report['planned_not_finished']), len(report['not_planned'])))
    print('ran: ' + ' '.join(report['ran']))
    print('not_run: ' + ' '.join(report['not_run']))
    sys.exit(3 if report['planned_not_finished'] else 0)


if __name__ == '__main__':
    main()
