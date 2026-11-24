import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawn, spawnSync} from 'node:child_process';

const script = path.resolve('scripts/release-local-apex-check.sh');

test('releases the lock after abrupt termination without allowing a surviving candidate to overlap', async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'local-gate-kill-'));
  let gate, candidate;
  try {
    const source = path.join(root, 'source'), bin = path.join(root, 'bin'), output = path.join(root, 'output');
    fs.mkdirSync(source); fs.mkdirSync(bin); fs.mkdirSync(output);
    const git = (...args) => { const r = spawnSync('git', ['-C', source, ...args], {encoding: 'utf8'}); assert.equal(r.status, 0, r.stderr); return r.stdout.trim(); };
    git('init', '-q');
    git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '--allow-empty', '-qm', 'fixture');
    const sha = git('rev-parse', 'HEAD');
    const binary = path.join(bin, 'glade'), marker = path.join(root, 'candidate');
    fs.writeFileSync(binary, `#!${process.execPath}\nrequire('fs').writeFileSync(process.env.MARKER, String(process.pid));\nif (process.env.WAIT_CANDIDATE) setInterval(() => {}, 1000); else process.exit(17);\n`, {mode: 0o755});
    fs.writeFileSync(path.join(bin, 'go'), '#!/bin/sh\nprintf "build vcs.revision=%s\\nbuild vcs.modified=false\\n" "$REVISION"\n', {mode: 0o755});
    const env = {...process.env, TMPDIR: output, PATH: `${bin}:${process.env.PATH}`, REVISION: sha, MARKER: marker};
    gate = spawn('bash', [script, binary, source], {env: {...env, WAIT_CANDIDATE: '1'}, stdio: 'ignore'});
    const deadline = Date.now() + 10000;
    while (!fs.existsSync(marker) && Date.now() < deadline) await new Promise(resolve => setTimeout(resolve, 20));
    assert.ok(fs.existsSync(marker), 'candidate did not start');
    candidate = Number(fs.readFileSync(marker, 'utf8'));
    gate.kill('SIGKILL');
    await new Promise(resolve => gate.once('exit', resolve));
    const overlap = spawnSync('bash', [script, binary, source], {env, encoding: 'utf8'});
    assert.match(overlap.stderr, /already running/);
    assert.equal(Number(fs.readFileSync(marker, 'utf8')), candidate);
    process.kill(candidate, 'SIGKILL');
    candidate = undefined;
    let retry;
    do {
      retry = spawnSync('bash', [script, binary, source], {env, encoding: 'utf8'});
      if (!/already running/.test(retry.stderr)) break;
      await new Promise(resolve => setTimeout(resolve, 20));
    } while (Date.now() < deadline);
    assert.equal(retry.status, 17, retry.stderr);
    assert.notEqual(Number(fs.readFileSync(marker, 'utf8')), gate.pid);
  } finally {
    if (candidate) { try { process.kill(candidate, 'SIGKILL'); } catch {} }
    if (gate?.exitCode === null && gate?.signalCode === null) gate.kill('SIGKILL');
    fs.rmSync(root, {recursive: true, force: true});
  }
});

test('rejects missing, mismatched and dirty build identity without leaving a stale receipt', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'local-gate-test-'));
  try {
    const source = path.join(root, 'source');
    const bin = path.join(root, 'bin');
    const output = path.join(root, 'output');
    fs.mkdirSync(source); fs.mkdirSync(bin); fs.mkdirSync(output);
    const git = (...args) => { const r = spawnSync('git', ['-C', source, ...args], {encoding: 'utf8'}); assert.equal(r.status, 0, r.stderr); return r.stdout.trim(); };
    git('init', '-q');
    git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '--allow-empty', '-qm', 'fixture');
    const sha = git('rev-parse', 'HEAD');
    const binary = path.join(bin, 'glade');
    fs.writeFileSync(binary, '#!/bin/sh\necho called >> "$CALLS"\nexit 0\n', {mode: 0o755});
    fs.writeFileSync(path.join(bin, 'go'), '#!/bin/sh\nprintf "%s\\n" "$BUILD_INFO"\n', {mode: 0o755});
    for (const info of ['', `build vcs.revision=${'0'.repeat(40)}\nbuild vcs.modified=false`, `build vcs.revision=${sha}\nbuild vcs.modified=true`]) {
      const receipt = path.join(output, 'release-local-apex-summary.json');
      fs.writeFileSync(receipt, 'old receipt');
      const result = spawnSync('bash', [script, binary, source], {env: {...process.env, TMPDIR: output, PATH: `${bin}:${process.env.PATH}`, BUILD_INFO: info, CALLS: path.join(root, 'calls')}});
      assert.notEqual(result.status, 0);
      assert.equal(fs.existsSync(receipt), false);
      assert.equal(fs.existsSync(path.join(root, 'calls')), false);
    }
  } finally { fs.rmSync(root, {recursive: true, force: true}); }
});

test('rejects an unreadable git index before running the candidate', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'local-gate-index-'));
  try {
    const source = path.join(root, 'source'), bin = path.join(root, 'bin'), output = path.join(root, 'output');
    fs.mkdirSync(source); fs.mkdirSync(bin); fs.mkdirSync(output);
    const git = (...args) => { const r = spawnSync('git', ['-C', source, ...args], {encoding: 'utf8'}); assert.equal(r.status, 0, r.stderr); return r.stdout.trim(); };
    git('init', '-q');
    git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '--allow-empty', '-qm', 'fixture');
    const sha = git('rev-parse', 'HEAD');
    fs.writeFileSync(path.join(source, '.git', 'index'), 'corrupt index');
    const binary = path.join(bin, 'glade'), calls = path.join(root, 'calls');
    fs.writeFileSync(binary, '#!/bin/sh\necho called >> "$CALLS"\nexit 1\n', {mode: 0o755});
    fs.writeFileSync(path.join(bin, 'go'), '#!/bin/sh\nprintf "build vcs.revision=%s\\nbuild vcs.modified=false\\n" "$REVISION"\n', {mode: 0o755});
    const receipt = path.join(output, 'release-local-apex-summary.json');
    fs.writeFileSync(receipt, 'old receipt');
    const result = spawnSync('bash', [script, binary, source], {env: {...process.env, TMPDIR: output, PATH: `${bin}:${process.env.PATH}`, REVISION: sha, CALLS: calls}, encoding: 'utf8'});
    assert.notEqual(result.status, 0);
    assert.equal(fs.existsSync(receipt), false);
    assert.equal(fs.existsSync(calls), false, 'candidate must not run when git status fails');
  } finally { fs.rmSync(root, {recursive: true, force: true}); }
});
