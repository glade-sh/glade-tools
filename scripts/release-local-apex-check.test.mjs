import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';

const script = path.resolve('scripts/release-local-apex-check.sh');

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
