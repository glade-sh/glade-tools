import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { mkdtemp, readFile, rm, unlink, writeFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { publishPlugins, previewBucket, releaseR2Fetch, requireCloudflareCredentials } from './plugin-release-publish.mjs'

const version = 'v1.2.3'
const toolsSHA = '2'.repeat(40)
const plugins = ['compat', 'orgpackage', 'performance']
const platforms = [['darwin', 'amd64'], ['darwin', 'arm64'], ['linux', 'amd64'], ['linux', 'arm64']]
const hash = bytes => createHash('sha256').update(bytes).digest('hex')

test('requires both Cloudflare release credentials', () => {
  assert.throws(() => requireCloudflareCredentials({}), /CLOUDFLARE_ACCOUNT_ID/)
  assert.throws(() => requireCloudflareCredentials({ CLOUDFLARE_ACCOUNT_ID: 'a'.repeat(32) }), /CLOUDFLARE_API_TOKEN/)
  assert.doesNotThrow(() => requireCloudflareCredentials({ CLOUDFLARE_ACCOUNT_ID: 'a'.repeat(32), CLOUDFLARE_API_TOKEN: 'token' }))
})

async function fixture(t) {
  const root = await mkdtemp(path.join(os.tmpdir(), 'glade-plugin-publish-test-'))
  t.after(() => rm(root, { recursive: true, force: true }))
  const rows = [], checksums = []
  for (const plugin of plugins) {
    const assets = []
    for (const [goos, goarch] of platforms) {
      const name = `glade-plugin-${plugin}_1.2.3_${goos}_${goarch}.tar.gz`
      const bytes = Buffer.from(name)
      const sha256 = hash(bytes)
      assets.push({ os: goos, arch: goarch, url: `https://plugins.glade.sh/${version}/${name}`, sha256 })
      checksums.push(`${sha256}  ${name}`)
      await writeFile(path.join(root, name), bytes)
    }
    rows.push({ name: `@glade/${plugin}`, version: '1.2.3', assets })
  }
  await writeFile(path.join(root, 'checksums.txt'), `${checksums.sort().join('\n')}\n`)
  await writeFile(path.join(root, 'index.json'), `${JSON.stringify({ version: 1, plugins: rows }, null, 2)}\n`)

  const objects = new Map(), calls = []
  const bucket = {
    async get(key) {
      calls.push(['get', key])
      const bytes = objects.get(key)
      return bytes ? { etag: hash(bytes), arrayBuffer: async () => bytes } : null
    },
    async put(key, body, options) {
      calls.push(['put', key, options])
      const bytes = Buffer.from(body), old = objects.get(key)
      if (options.onlyIf.etagDoesNotMatch === '*' && old) return null
      if (options.onlyIf.etagMatches && (!old || hash(old) !== options.onlyIf.etagMatches)) return null
      objects.set(key, bytes)
      return { etag: hash(bytes) }
    },
  }
  return { root, objects, calls, bucket }
}

test('publishes verified versioned objects first and the CAS root index last, then resumes', async t => {
  const { root, bucket: storage, calls } = await fixture(t)
  const env = { VERSION: version, BUCKET: {
    async get(key) {
      const object = await storage.get(key)
      return object && { etag: object.etag, body: await object.arrayBuffer() }
    },
    async put(key, body, options) {
      return storage.put(key, Buffer.from(await new Response(body).arrayBuffer()), options)
    },
  } }
  const bucket = previewBucket((url, init) => releaseR2Fetch(new Request(url, init), env))
  await publishPlugins(bucket, root, version, toolsSHA)

  const writes = calls.filter(([method]) => method === 'put')
  assert.equal(writes.length, 14)
  assert.equal(writes.at(-1)[1], 'index.json')
  for (const [, key, options] of writes.slice(0, -1)) {
    assert.match(key, /^v1\.2\.3\/[^/]+$/)
    assert.deepEqual(options.onlyIf, { etagDoesNotMatch: '*' })
    assert.equal(options.httpMetadata.cacheControl, 'public, max-age=31536000, immutable')
    assert.ok(calls.some(([method, readKey]) => method === 'get' && readKey === key), `missing readback for ${key}`)
  }
  assert.equal(writes.at(-1)[2].httpMetadata.cacheControl, 'no-cache')

  await publishPlugins(bucket, root, version, toolsSHA)
  assert.equal(calls.filter(([method]) => method === 'put').length, 14)
})

test('rejects an incomplete local inventory before touching storage', async t => {
  const { root, bucket, calls } = await fixture(t)
  await unlink(path.join(root, 'glade-plugin-compat_1.2.3_linux_amd64.tar.gz'))
  await assert.rejects(publishPlugins(bucket, root, version, toolsSHA), /archive inventory/)
  assert.equal(calls.length, 0)
})

test('rejects a checksum mismatch before touching storage', async t => {
  const { root, bucket, calls } = await fixture(t)
  const checksums = await readFile(path.join(root, 'checksums.txt'), 'utf8')
  await writeFile(path.join(root, 'checksums.txt'), checksums.replace(/^[0-9a-f]/, 'f'))
  await assert.rejects(publishPlugins(bucket, root, version, toolsSHA), /checksum/)
  assert.equal(calls.length, 0)
})

test('never overwrites conflicting immutable bytes or advances the root index', async t => {
  const { root, bucket, objects } = await fixture(t)
  const key = `${version}/glade-plugin-compat_1.2.3_darwin_amd64.tar.gz`
  objects.set(key, Buffer.from('conflicting immutable bytes'))
  await assert.rejects(publishPlugins(bucket, root, version, toolsSHA), /immutable object differs/)
  assert.equal(objects.get(key).toString(), 'conflicting immutable bytes')
  assert.equal(objects.has('index.json'), false)
})

test('does not roll a newer registry backward', async t => {
  const { root, bucket, objects, calls } = await fixture(t)
  objects.set('index.json', Buffer.from(JSON.stringify({
    version: 1,
    plugins: plugins.map(plugin => ({ name: `@glade/${plugin}`, version: '1.2.4', assets: [] })),
  })))
  await assert.rejects(publishPlugins(bucket, root, version, toolsSHA), /newer registry/)
  assert.equal(calls.some(([method]) => method === 'put'), false)
})

test('loses a root-index race instead of overwriting the concurrent publisher', async t => {
  const { root, bucket, objects } = await fixture(t)
  objects.set('index.json', Buffer.from(JSON.stringify({
    version: 1,
    plugins: plugins.map(plugin => ({ name: `@glade/${plugin}`, version: '1.2.2', assets: [] })),
  })))
  const put = bucket.put.bind(bucket)
  bucket.put = async (...args) => {
    const result = await put(...args)
    if (args[0] === `${version}/checksums.txt`) objects.set('index.json', Buffer.from(JSON.stringify({ version: 1, plugins: [] })))
    return result
  }
  await assert.rejects(publishPlugins(bucket, root, version, toolsSHA), /conditional publication conflict/)
  assert.deepEqual(JSON.parse(objects.get('index.json')), { version: 1, plugins: [] })
})

test('fails when versioned-object readback differs and leaves the root index unchanged', async t => {
  const { root, bucket, objects } = await fixture(t)
  const originalGet = bucket.get.bind(bucket)
  let altered = false
  bucket.get = async key => {
    const object = await originalGet(key)
    if (!altered && object && key === `${version}/checksums.txt`) {
      altered = true
      return { etag: object.etag, arrayBuffer: async () => Buffer.from('altered') }
    }
    return object
  }
  await assert.rejects(publishPlugins(bucket, root, version, toolsSHA), /readback differs/)
  assert.equal(objects.has('index.json'), false)
})

test('remote preview rejects bare, foreign, nested, traversal, backslash, and unsafe writes', async () => {
  let writes = 0
  const env = { VERSION: version, BUCKET: { get() { assert.fail('unexpected read') }, put() { writes += 1; return { etag: 'stored' } } } }
  const request = (key, options) => new Request(`http://localhost/?key=${encodeURIComponent(key)}`, {
    method: 'PUT', body: 'x', headers: { 'x-r2-options': JSON.stringify({ sha256: 'a'.repeat(64), ...options }) },
  })
  for (const [key, options, status] of [
    ['checksums.txt', { onlyIf: { etagDoesNotMatch: '*' } }, 403],
    ['v1.2.2/checksums.txt', { onlyIf: { etagDoesNotMatch: '*' } }, 403],
    [`${version}/nested/file`, { onlyIf: { etagDoesNotMatch: '*' } }, 403],
    [`${version}/../index.json`, { onlyIf: { etagDoesNotMatch: '*' } }, 403],
    [`${version}/bad\\name`, { onlyIf: { etagDoesNotMatch: '*' } }, 403],
    [`${version}/checksums.txt`, {}, 400],
    [`${version}/checksums.txt`, { onlyIf: { etagMatches: 'existing' } }, 400],
  ]) {
    const response = await releaseR2Fetch(request(key, options), env)
    assert.equal(response.status, status, key)
  }
  assert.equal(writes, 0)
})

test('remote preview accepts create-only versioned writes and CAS root-index writes', async () => {
  const calls = []
  const env = { VERSION: version, BUCKET: { async put(key, body, options) {
    calls.push([key, Buffer.from(await new Response(body).arrayBuffer()), options])
    return { etag: 'stored' }
  } } }
  for (const [key, onlyIf] of [
    [`${version}/checksums.txt`, { etagDoesNotMatch: '*' }],
    ['index.json', { etagMatches: 'current' }],
  ]) {
    const response = await releaseR2Fetch(new Request(`http://localhost/?key=${encodeURIComponent(key)}`, {
      method: 'PUT', body: 'x', headers: { 'x-r2-options': JSON.stringify({ onlyIf, sha256: hash('x') }) },
    }), env)
    assert.equal(response.status, 200)
  }
  assert.deepEqual(calls.map(([key]) => key), [`${version}/checksums.txt`, 'index.json'])
})
