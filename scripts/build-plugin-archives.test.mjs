import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';

const root = path.resolve('.');

test('writes archives to relative and absolute output directories', () => {
  const scratch = fs.mkdtempSync(path.join(root, '.archive-test-'));
  const bin = fs.mkdtempSync(path.join(os.tmpdir(), 'archive-tools-'));
  try {
    fs.writeFileSync(path.join(bin, 'go'), `#!/bin/sh\nwhile [ "$#" -gt 0 ]; do if [ "$1" = -o ]; then shift; printf 'binary' > "$1"; chmod +x "$1"; exit; fi; shift; done\nexit 1\n`, {mode: 0o755});
    fs.writeFileSync(path.join(bin, 'python3'), `#!/bin/sh\ncase "$1" in */release-go-notices.py) mkdir -p "$4"; exit 0;; */validate-plugin-archive.py) exit 0;; esac\nexec "${process.env.PYTHON_FOR_TEST || '/usr/bin/python3'}" "$@"\n`, {mode: 0o755});
    for (const output of [path.relative(root, path.join(scratch, 'relative')), path.join(scratch, 'absolute')]) {
      const run = spawnSync('bash', ['scripts/build-plugin-archives.sh', '1.2.3'], {cwd: root, encoding: 'utf8', env: {...process.env, PATH: `${bin}:${process.env.PATH}`, TARGETS: 'linux/amd64', OUT_DIR: output}});
      assert.equal(run.status, 0, run.stderr);
      const directory = path.resolve(root, output);
      for (const name of ['compat', 'orgpackage', 'performance']) assert.ok(fs.existsSync(path.join(directory, `glade-plugin-${name}_1.2.3_linux_amd64.tar.gz`)));
      assert.equal(fs.readFileSync(path.join(directory, 'checksums.txt'), 'utf8').trim().split('\n').length, 3);
    }
  } finally { fs.rmSync(scratch, {recursive: true, force: true}); fs.rmSync(bin, {recursive: true, force: true}); }
});
