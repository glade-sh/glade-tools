#!/usr/bin/env node
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { lstat, mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import os from 'node:os'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

const sha256 = bytes => createHash('sha256').update(bytes).digest('hex')
const stable = /^v\d+\.\d+\.\d+$/
const sourceSHA = /^[0-9a-f]{40}$/
const checksumSHA = /^[0-9a-f]{64}$/
const plugins = ['compat', 'orgpackage', 'performance']
const platforms = [['darwin', 'amd64'], ['darwin', 'arm64'], ['linux', 'amd64'], ['linux', 'arm64']]
const newer = (left, right) => left.localeCompare(right, 'en', { numeric: true }) > 0

export function requireCloudflareCredentials(env) {
  assert.match(env.CLOUDFLARE_ACCOUNT_ID || '', /^[a-f0-9]{32}$/, 'CLOUDFLARE_ACCOUNT_ID is required')
  assert.ok(env.CLOUDFLARE_API_TOKEN, 'CLOUDFLARE_API_TOKEN is required')
}

async function regularFile(filename, label) {
  let info
  try {
    info = await lstat(filename)
  } catch {
    assert.fail(`missing required ${label}`)
  }
  assert.ok(info.isFile(), `not a regular ${label}`)
  return readFile(filename)
}

function validateIndex(index, version) {
  assert.equal(index?.version, 1, 'invalid registry schema version')
  assert.ok(Array.isArray(index.plugins), 'invalid registry plugins')
  assert.deepEqual(index.plugins.map(row => row?.name), plugins.map(name => `@glade/${name}`), 'invalid registry plugin inventory')
  const release = version.slice(1)
  const expectedNames = []
  for (const [offset, plugin] of plugins.entries()) {
    const row = index.plugins[offset]
    assert.equal(row.version, release, `registry version mismatch for ${plugin}`)
    assert.ok(Array.isArray(row.assets), `invalid registry assets for ${plugin}`)
    assert.deepEqual(row.assets.map(asset => [asset?.os, asset?.arch]), platforms, `invalid registry platform inventory for ${plugin}`)
    for (const asset of row.assets) {
      const name = `glade-plugin-${plugin}_${release}_${asset.os}_${asset.arch}.tar.gz`
      assert.equal(asset.url, `https://plugins.glade.sh/${version}/${name}`, `registry URL mismatch for ${name}`)
      assert.match(asset.sha256 || '', checksumSHA, `invalid registry checksum for ${name}`)
      expectedNames.push(name)
    }
  }
  return expectedNames
}

function currentRegistryVersion(bytes) {
  let index
  try {
    index = JSON.parse(bytes)
  } catch {
    assert.fail('invalid current registry JSON')
  }
  assert.equal(index?.version, 1, 'invalid current registry schema version')
  assert.ok(Array.isArray(index.plugins), 'invalid current registry plugins')
  assert.deepEqual(index.plugins.map(row => row?.name), plugins.map(name => `@glade/${name}`), 'invalid current registry plugin inventory')
  const versions = [...new Set(index.plugins.map(row => row?.version))]
  assert.equal(versions.length, 1, 'current registry plugin versions differ')
  assert.match(versions[0] || '', /^\d+\.\d+\.\d+$/, 'invalid current registry version')
  return versions[0]
}

export async function publishPlugins(bucket, root, version, expectedToolsSHA) {
  assert.match(version, stable, 'expected a stable plugin release version')
  assert.match(expectedToolsSHA, sourceSHA, 'expected the tagged Tools SHA')
  const indexBytes = await regularFile(path.join(root, 'index.json'), 'registry index')
  let index
  try {
    index = JSON.parse(indexBytes)
  } catch {
    assert.fail('invalid registry index JSON')
  }
  const expectedNames = validateIndex(index, version)
  const expectedSet = new Set(expectedNames)

  const localArchives = (await readdir(root)).filter(name => name.startsWith('glade-plugin-') && name.endsWith('.tar.gz')).sort()
  assert.deepEqual(localArchives, [...expectedNames].sort(), 'local plugin archive inventory mismatch')

  const checksumBytes = await regularFile(path.join(root, 'checksums.txt'), 'checksums file')
  const checksumRows = new Map()
  for (const [offset, line] of checksumBytes.toString('utf8').split('\n').entries()) {
    if (!line && offset === checksumBytes.toString('utf8').split('\n').length - 1) continue
    const match = line.match(/^([0-9a-f]{64}) {2}([^/\\]+)$/)
    assert.ok(match, `invalid checksum row ${offset + 1}`)
    const [, checksum, name] = match
    assert.ok(expectedSet.has(name), `unexpected checksum archive: ${name}`)
    assert.ok(!checksumRows.has(name), `duplicate checksum archive: ${name}`)
    checksumRows.set(name, checksum)
  }
  assert.deepEqual([...checksumRows.keys()].sort(), [...expectedNames].sort(), 'checksum inventory mismatch')

  const files = new Map()
  for (const name of [...expectedNames].sort()) {
    const bytes = await regularFile(path.join(root, name), `archive: ${name}`)
    const checksum = sha256(bytes)
    assert.equal(checksum, checksumRows.get(name), `checksums file mismatch for ${name}`)
    const asset = index.plugins.flatMap(row => row.assets).find(candidate => path.basename(new URL(candidate.url).pathname) === name)
    assert.equal(checksum, asset.sha256, `registry checksum mismatch for ${name}`)
    files.set(`${version}/${name}`, bytes)
  }
  files.set(`${version}/checksums.txt`, checksumBytes)

  async function snapshot(key) {
    const object = await bucket.get(key)
    return object ? { etag: object.etag, bytes: Buffer.from(await object.arrayBuffer()) } : null
  }
  const previousIndex = await snapshot('index.json')
  if (previousIndex) {
    const current = currentRegistryVersion(previousIndex.bytes)
    const release = version.slice(1)
    assert.ok(!newer(current, release), 'refusing to replace a newer registry')
    assert.ok(current !== release || previousIndex.bytes.equals(indexBytes), 'current registry differs for this release')
  }

  async function putVerified(key, bytes, old, mutable) {
    if (old && old.bytes.equals(bytes)) return
    assert.ok(mutable || !old, `immutable object differs: ${key}`)
    const result = await bucket.put(key, bytes, {
      onlyIf: old ? { etagMatches: old.etag } : { etagDoesNotMatch: '*' },
      sha256: sha256(bytes),
      httpMetadata: {
        contentType: key.endsWith('.json') ? 'application/json' : key.endsWith('.tar.gz') ? 'application/gzip' : 'text/plain',
        cacheControl: mutable ? 'no-cache' : 'public, max-age=31536000, immutable',
      },
    })
    assert.ok(result, `conditional publication conflict: ${key}; retry after inspection`)
    const stored = await snapshot(key)
    assert.ok(stored && stored.bytes.equals(bytes), `release readback differs: ${key}`)
    console.log(`Verified ${key}`)
  }

  for (const [key, bytes] of files) await putVerified(key, bytes, await snapshot(key), false)
  await putVerified('index.json', indexBytes, previousIndex, true)
  const finalIndex = await snapshot('index.json')
  assert.ok(finalIndex && finalIndex.bytes.equals(indexBytes), 'final registry index differs')
  return { version, toolsSHA: expectedToolsSHA, versionedObjects: files.size }
}

export async function releaseR2Fetch(request, env) {
  const key = new URL(request.url).searchParams.get('key') || ''
  const mutable = key === 'index.json'
  const filename = key.startsWith(`${env.VERSION}/`) ? key.slice(env.VERSION.length + 1) : ''
  const release = String(env.VERSION || '').slice(1).replaceAll('.', '\\.')
  const versionedName = new RegExp(`^(?:checksums\\.txt|glade-plugin-(?:compat|orgpackage|performance)_${release}_(?:darwin|linux)_(?:amd64|arm64)\\.tar\\.gz)$`)
  if (!mutable && (!filename || filename.includes('/') || filename.includes('..') || filename.includes('\\') || !versionedName.test(filename))) {
    return new Response(null, { status: 403 })
  }
  if (request.method === 'GET') {
    const object = await env.BUCKET.get(key)
    return object ? new Response(object.body, { headers: { etag: object.etag } }) : new Response(null, { status: 404 })
  }
  if (request.method !== 'PUT') return new Response(null, { status: 405 })
  let options
  try {
    options = JSON.parse(request.headers.get('x-r2-options') || '{}')
  } catch {
    return new Response(null, { status: 400 })
  }
  const onlyIf = options.onlyIf || {}
  const conditions = Object.keys(onlyIf)
  const createOnly = conditions.length === 1 && onlyIf.etagDoesNotMatch === '*'
  const compareAndSwap = conditions.length === 1 && typeof onlyIf.etagMatches === 'string' && onlyIf.etagMatches.length > 0
  if ((!mutable && !createOnly) || (mutable && !createOnly && !compareAndSwap) || !/^[0-9a-f]{64}$/.test(options.sha256 || '')) {
    return new Response(null, { status: 400 })
  }
  const object = await env.BUCKET.put(key, request.body, options)
  return Response.json(object ? { etag: object.etag } : null)
}

export function previewBucket(fetch) {
  return {
    async get(key) {
      const response = await fetch(`http://localhost/?key=${encodeURIComponent(key)}`, { signal: AbortSignal.timeout(120000) })
      if (response.status === 404) return null
      assert.ok(response.ok, `remote R2 read failed: ${response.status}`)
      return { etag: response.headers.get('etag'), arrayBuffer: () => response.arrayBuffer() }
    },
    async put(key, body, options) {
      const response = await fetch(`http://localhost/?key=${encodeURIComponent(key)}`, {
        method: 'PUT', body, headers: { 'x-r2-options': JSON.stringify(options) }, signal: AbortSignal.timeout(120000),
      })
      assert.ok(response.ok, `remote R2 write failed: ${response.status}`)
      return response.json()
    },
  }
}

async function main() {
  assert.equal(process.argv.length, 5, 'usage: plugin-release-publish.mjs <archive-dir> <version> <approved-tools-sha>')
  const [root, version, expectedToolsSHA] = process.argv.slice(2)
  assert.match(version, stable)
  assert.match(expectedToolsSHA, sourceSHA)
  assert.equal(execFileSync('git', ['rev-parse', `${version}^{commit}`], { encoding: 'utf8' }).trim(), expectedToolsSHA, 'tagged Tools SHA differs')
  requireCloudflareCredentials(process.env)
  const require = createRequire(import.meta.url)
  const { unstable_dev } = require(process.env.WRANGLER_MODULE || 'wrangler')
  const temporary = await mkdtemp(path.join(os.tmpdir(), 'glade-plugin-publisher-'))
  let worker
  try {
    const configPath = path.join(temporary, 'wrangler.json')
    await writeFile(configPath, JSON.stringify({
      name: 'glade-plugin-publisher',
      compatibility_date: '2026-09-01',
      account_id: process.env.CLOUDFLARE_ACCOUNT_ID,
      vars: { VERSION: version },
      r2_buckets: [{ binding: 'BUCKET', bucket_name: 'glade-plugins', remote: true }],
    }), { mode: 0o600 })
    const script = path.join(temporary, 'publisher.mjs')
    await writeFile(script, `export default { fetch: ${releaseR2Fetch.toString()} };\n`, { mode: 0o600 })
    worker = await unstable_dev(script, {
      config: configPath,
      local: false,
      experimental: { disableExperimentalWarning: true, disableDevRegistry: true },
    })
    console.log(JSON.stringify(await publishPlugins(previewBucket((...args) => worker.fetch(...args)), root, version, expectedToolsSHA)))
  } finally {
    await worker?.stop()
    await rm(temporary, { recursive: true, force: true })
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  main().catch(error => { console.error(error.message); process.exitCode = 1 })
}
