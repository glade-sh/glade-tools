#!/usr/bin/env python3
"""Record checks before launching independently running lanes."""
import argparse
import json
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument('out')
parser.add_argument('--shard')
parser.add_argument('repos', nargs='*')
args = parser.parse_args()
out = Path(args.out)
repos = args.repos
if args.shard:
    repo, spec = args.shard.split('=', 1)
    indices = spec.split('/')[0].split(',') if '/' in spec else ['0']
    if '0' not in indices:
        repos = [r for r in repos if r != repo]
out.mkdir(parents=True, exist_ok=True)
for repo in repos:
    (out / (repo + '.check-equivalence.json')).unlink(missing_ok=True)
(out / 'check-plan.json').write_text(json.dumps(repos) + '\n')
