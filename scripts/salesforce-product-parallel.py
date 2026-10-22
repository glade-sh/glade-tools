"""Run product tests across jobs, retaining the existing verifier evidence contract."""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

PREFIX = 'github.com/glade-sh/glade/'
KINDS = {'apextest': ('internal/apextest', 16),
         'lwc-compile': ('internal/lwc/compile', 8),
         'visualforce': ('internal/visualforce', 4)}
TOOLS = Path(__file__).resolve().parent.parent


def require(condition, message):
    if not condition:
        raise ValueError(message)


def unique(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'duplicate JSON key')
        result[key] = value
    return result


def read_json(path):
    return json.loads(path.read_text(), object_pairs_hook=unique)


def write_json(path, value):
    path.write_text(json.dumps(value, sort_keys=True, separators=(',', ':')) + '\n')


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def capture(root, *args):
    return subprocess.check_output(['go', '-C', str(root), *args], text=True)


def identity(root):
    def head(path):
        return subprocess.check_output(['git', '-C', str(path), 'rev-parse', 'HEAD'], text=True).strip()
    result = {'gladeSHA': head(root), 'toolsSHA': head(TOOLS),
              'runID': os.environ['GITHUB_RUN_ID'], 'runAttempt': os.environ['GITHUB_RUN_ATTEMPT']}
    require(all(re.fullmatch('[0-9a-f]{40}', result[k]) for k in ('gladeSHA', 'toolsSHA')), 'invalid checkout identity')
    require(all(re.fullmatch('[1-9][0-9]*', result[k]) for k in ('runID', 'runAttempt')), 'invalid run identity')
    for env, key in [('GLADE_SHA', 'gladeSHA'), ('GITHUB_SHA', 'toolsSHA')]:
        require(not os.environ.get(env) or os.environ[env].lower() == result[key], 'requested checkout mismatch')
    return result


def package_groups(packages):
    groups = {}
    remaining = []
    for package in packages:
        if package in [PREFIX + p for p, _ in KINDS.values()]:
            continue
        suffix = package.removeprefix(PREFIX)
        if suffix in ['internal/sema', 'internal/vm', 'internal/gladecli']:
            groups[suffix.split('/')[-1]] = [package]
        else:
            remaining.append(package)
    for index in range(min(4, len(remaining))):
        groups[f'remaining-{index}'] = remaining[index::4]
    return [{'id': 'packages-' + name, 'kind': 'packages', 'packages': values}
            for name, values in sorted(groups.items())]


def validate_test_plan(plan, package, names, count):
    require(isinstance(plan, dict) and set(plan) == {'version', 'package', 'historyUsed', 'shards'}, 'invalid shard plan schema')
    require(type(plan['version']) is int and plan['version'] == 1 and plan['package'] == package and type(plan['historyUsed']) is bool, 'invalid shard plan identity')
    require(isinstance(plan['shards'], list) and len(plan['shards']) == count, 'invalid shard count')
    union = []
    for index, shard in enumerate(plan['shards']):
        require(isinstance(shard, dict) and set(shard) == {'index', 'tests', 'estimatedDurationMillis', 'regex'}, 'invalid shard schema')
        tests = shard['tests']
        require(type(shard['index']) is int and shard['index'] == index and isinstance(tests, list) and tests and tests == sorted(tests), 'invalid shard selection')
        require(all(isinstance(n, str) and re.fullmatch(r'Test[A-Za-z0-9_]*', n) for n in tests), 'invalid test name')
        require(type(shard['estimatedDurationMillis']) is int and shard['estimatedDurationMillis'] >= 0, 'invalid shard estimate')
        require(shard['regex'] == '^(?:' + '|'.join(re.escape(n) for n in tests) + ')$', 'invalid shard regex')
        union.extend(tests)
    require(len(union) == len(set(union)) and sorted(union) == names, 'shard union differs from discovery')


def shards_from_plan(directory, plan):
    packages = plan['packages']
    require(isinstance(packages, list) and packages and len(set(packages)) == len(packages), 'invalid package discovery')
    require(all(isinstance(p, str) and re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._~/-]*', p) for p in packages), 'invalid package name')
    shards = package_groups(packages)
    for kind, (suffix, count) in KINDS.items():
        package = PREFIX + suffix
        require(packages.count(package) == 1, 'missing specialized package')
        names = (directory / f'{kind}-discovery.txt').read_text().splitlines()
        require(names and names == sorted(set(names)) and all(re.fullmatch(r'Test[A-Za-z0-9_]*', n) for n in names), 'invalid test discovery')
        raw = (directory / f'{kind}-discovery-raw.txt').read_text().splitlines()
        require(sorted(raw) == names, 'raw discovery mismatch')
        selection = read_json(directory / f'{kind}-plan.json')
        validate_test_plan(selection, package, names, count)
        binary = plan['binaries'][kind]
        require(set(binary) == {'sha256', 'sizeBytes'} and re.fullmatch('[0-9a-f]{64}', binary['sha256']) and type(binary['sizeBytes']) is int and binary['sizeBytes'] > 0, 'invalid binary metadata')
        for shard in selection['shards']:
            shards.append({'id': f'{kind}-{shard["index"]:02d}', 'kind': kind,
                           'packages': [package], 'selection': shard})
    return shards


def load_plan(root, directory):
    require(not directory.is_symlink(), 'symlinked plan directory')
    plan = read_json(directory / 'parallel-plan.json')
    require(plan['schemaVersion'] == 1 and plan['identity'] == identity(root), 'plan checkout or run identity mismatch')
    require(plan['shards'] == shards_from_plan(directory, plan), 'parallel shard plan mismatch')
    return plan


def check_binary(directory, plan, kind):
    path = directory / f'{kind}.test'
    require(not path.is_symlink() and path.is_file(), 'missing binary')
    require(path.stat().st_size == plan['binaries'][kind]['sizeBytes'] and digest(path) == plan['binaries'][kind]['sha256'], 'test binary digest mismatch')
    return path


def prepare(root, directory):
    directory.mkdir()  # Never reuse a partial or stale plan.
    packages = capture(root, 'list', '-f', '{{.ImportPath}}', './...').splitlines()
    plan = {'schemaVersion': 1, 'identity': identity(root), 'packages': packages, 'binaries': {}}
    for kind, (suffix, count) in KINDS.items():
        binary = directory / f'{kind}.test'
        subprocess.run(['go', '-C', str(root), 'test', '-c', '-o', str(binary), './' + suffix], check=True)
        plan['binaries'][kind] = {'sha256': digest(binary), 'sizeBytes': binary.stat().st_size}
        # Reject unsupported Example/Fuzz names rather than silently filtering them out.
        raw = subprocess.check_output([str(binary), '-test.list', '.'], cwd=root / suffix, text=True)
        (directory / f'{kind}-discovery-raw.txt').write_text(raw)
        names = raw.splitlines()
        require(names and len(set(names)) == len(names) and all(re.fullmatch(r'Test[A-Za-z0-9_]*', n) for n in names), 'unsupported or duplicate test discovery')
        discovery = directory / f'{kind}-discovery.txt'
        discovery.write_text(''.join(n + '\n' for n in sorted(names)))
        planner = capture(root, 'run', './scripts/internal/cishard', '--package', PREFIX + suffix,
                          '--shards', str(count), '--tests', str(discovery))
        (directory / f'{kind}-plan.json').write_text(planner)
    plan['shards'] = shards_from_plan(directory, plan)
    write_json(directory / 'parallel-plan.json', plan)
    print(json.dumps({'include': [{'id': s['id']} for s in plan['shards']]}))


def read_events(path):
    require(path.stat().st_size > 0, 'missing events')
    with path.open('rb') as source:
        for line in source:
            require(line.strip() and line.endswith(b'\n'), 'empty or unterminated event')
            yield json.loads(line, object_pairs_hook=unique)


def validate_events(path, shard):
    terminals = {}
    tests = {}
    packages_with_tests = set()
    for event in read_events(path):
        require(isinstance(event, dict) and event.get('Package') in shard['packages'], 'unexpected event package')
        package, name, action = event['Package'], event.get('Test'), event.get('Action')
        require(action != 'fail', 'failed test event')
        if name:
            require(isinstance(name, str), 'invalid test event')
            packages_with_tests.add(package)
            if shard['kind'] != 'packages':
                require(name.split('/')[0] in shard['selection']['tests'], 'unselected test event')
            if '/' not in name and action in {'pass', 'skip', 'fail'}:
                tests.setdefault(name, []).append(action)
        elif action in {'pass', 'fail', 'skip'}:
            terminals.setdefault(package, []).append(action)
    require(set(terminals) == set(shard['packages']), 'missing package terminals')
    for package, actions in terminals.items():
        require(actions == ['pass'] or (shard['kind'] == 'packages' and actions == ['skip'] and package not in packages_with_tests), 'invalid package terminal')
    if shard['kind'] != 'packages':
        require(set(tests) == set(shard['selection']['tests']), 'test terminal union mismatch')
        for actions in tests.values():
            # Preserve Go's existing non-Apex skip semantics; Apex requires every test pass.
            require(actions == ['pass'] or (shard['kind'] != 'apextest' and actions == ['skip']), 'invalid test terminal')


def run_shard(root, directory, shard_id, output):
    plan = load_plan(root, directory)
    selected = [s for s in plan['shards'] if s['id'] == shard_id]
    require(len(selected) == 1, 'unknown shard')
    shard = selected[0]
    output.mkdir()
    events = output / 'events.jsonl'
    with events.open('wb') as target:
        if shard['kind'] == 'packages':
            subprocess.run(['go', '-C', str(root), 'test', '-json', '-count=1', '-p', '1', '-timeout=60m', *shard['packages']], stdout=target, check=True)
        else:
            binary = check_binary(directory, plan, shard['kind'])
            binary.chmod(0o755)  # Artifact downloads do not preserve executable bits.
            command = [str(binary), '-test.v=test2json', '-test.count=1', '-test.timeout=60m', '-test.run=' + shard['selection']['regex']]
            with subprocess.Popen(command, cwd=root / KINDS[shard['kind']][0], stdout=subprocess.PIPE) as process:
                converter = subprocess.run(['go', '-C', str(root), 'tool', 'test2json', '-p', shard['packages'][0]], stdin=process.stdout, stdout=target)
                process.stdout.close()
                status = process.wait()
                require(status == 0 and converter.returncode == 0, 'test binary or converter failed')
    validate_events(events, shard)
    write_json(output / 'result.json', {**plan['identity'], 'shardID': shard_id, 'status': 'pass',
                                      'planSHA256': digest(directory / 'parallel-plan.json'), 'eventsSHA256': digest(events)})


def aggregate(root, directory, results, output):
    plan = load_plan(root, directory)
    require(capture(root, 'list', '-f', '{{.ImportPath}}', './...').splitlines() == plan['packages'], 'current package set differs from plan')
    for kind in KINDS:
        check_binary(directory, plan, kind)
    entries = {p.name: p for p in results.iterdir()}
    # download-artifact keeps each artifact in a separate directory.
    prefix = 'salesforce-product-'
    if directory.parent == results:
        require(entries.pop('salesforce-product-plan', None) == directory, 'missing plan artifact')
        require(all(n.startswith(prefix) for n in entries), 'unexpected product artifact')
        entries = {n.removeprefix(prefix): p for n, p in entries.items()}
    require(set(entries) == {s['id'] for s in plan['shards']}, 'missing, duplicate or unexpected shard artifact')
    for shard in plan['shards']:
        entry = entries[shard['id']]
        require(not entry.is_symlink() and entry.is_dir(), 'invalid shard directory')
        require({p.name for p in entry.iterdir()} == {'result.json', 'events.jsonl'}, 'unexpected shard payload')
        require(all(not p.is_symlink() and p.is_file() for p in entry.iterdir()), 'invalid shard file')
        receipt = read_json(entry / 'result.json')
        expected = {**plan['identity'], 'shardID': shard['id'], 'status': 'pass',
                    'planSHA256': digest(directory / 'parallel-plan.json'), 'eventsSHA256': digest(entry / 'events.jsonl')}
        require(receipt == expected, 'shard status, identity or checksum mismatch')
        validate_events(entry / 'events.jsonl', shard)
    output.mkdir(exist_ok=True)
    evidence = output / 'product-test-evidence'
    evidence.mkdir()
    (evidence / 'all-packages.txt').write_text(''.join(p + '\n' for p in plan['packages']))
    non_apex = [p for p in plan['packages'] if p != PREFIX + 'internal/apextest']
    (evidence / 'non-apextest-packages.txt').write_text(''.join(p + '\n' for p in non_apex))
    # Validate each split package first, then normalize to one package terminal for
    # the unchanged verifier. Original worker streams remain in parallel evidence.
    package_actions = {}
    with (evidence / 'non-apextest.jsonl').open('w') as target:
        for shard in plan['shards']:
            if shard['kind'] == 'apextest':
                continue
            for event in read_events(entries[shard['id']] / 'events.jsonl'):
                if event.get('Test') or event.get('Action') not in {'pass', 'fail', 'skip'}:
                    target.write(json.dumps(event, separators=(',', ':')) + '\n')
                else:
                    package_actions[event['Package']] = event['Action']
        for package in non_apex:
            action = package_actions[package]
            target.write(json.dumps({'Action': action, 'Package': package}, separators=(',', ':')) + '\n')
    for filename in ['apextest-discovery-raw.txt', 'apextest-discovery.txt', 'apextest-plan.json']:
        shutil.copyfile(directory / filename, evidence / filename)
    for shard in plan['shards']:
        if shard['kind'] == 'apextest':
            destination = evidence / f'shard-{shard["selection"]["index"]:02d}'
            destination.mkdir()
            write_json(destination / 'selection.json', shard['selection'])
            (destination / 'regex.txt').write_text(shard['selection']['regex'] + '\n')
            shutil.copyfile(entries[shard['id']] / 'events.jsonl', destination / 'events.jsonl')
    parallel = output / 'product-parallel-evidence'
    parallel.mkdir()
    shutil.copytree(directory, parallel / 'plan', ignore=shutil.ignore_patterns('*.test'))
    for name, source in entries.items():
        shutil.copytree(source, parallel / name)
    binary = plan['binaries']['apextest']
    subprocess.run([sys.executable, str(TOOLS / 'scripts/salesforce-product-validation.py'), str(output), str(evidence), str(output / 'product-tests.jsonl'), PREFIX + 'internal/apextest', binary['sha256'], str(binary['sizeBytes'])], check=True)


def main():
    mode, root_text, *args = sys.argv[1:]
    root = Path(root_text).resolve()
    require(os.environ.get('LC_ALL') == 'C' and os.environ.get('GLADE_LWC_COMPILE') == '1' and Path(os.environ.get('GLADE_ROOT', '')).resolve() == root, 'Salesforce product-test environment is not exact')
    paths = [Path(a).absolute() for a in args]
    if mode == 'prepare' and len(args) == 1:
        prepare(root, paths[0])
    elif mode == 'shard' and len(args) == 3:
        run_shard(root, paths[0], args[1], paths[2])
    elif mode == 'aggregate' and len(args) == 3:
        try:
            aggregate(root, *paths)
        except BaseException:
            (paths[2] / 'product-tests.jsonl').unlink(missing_ok=True)
            raise
    else:
        raise ValueError('invalid product-test mode or arguments')


if __name__ == '__main__':
    try:
        main()
    except (ValueError, KeyError, OSError, subprocess.SubprocessError) as error:
        sys.exit('Salesforce product tests: ' + str(error))
