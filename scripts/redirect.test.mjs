import test from 'node:test'
import assert from 'node:assert/strict'
import worker from '../infra/cloudflare/redirect.mjs'
test('canonical redirect preserves paths, encoding, and queries', () => {
  for (const path of [
    '/',
    '/zh-hans/pricing?ref=launch&next=%2Fa%2Fb',
    '/docs/%252F?x=1&x=2',
    '/中文?source=a',
  ]) {
    const input = new URL(`https://minoduck.ai${path}`)
    const result = worker.fetch(new Request(input))
    input.hostname = 'www.minoduck.ai'
    assert.equal(result.status, 308)
    assert.equal(result.headers.get('location'), input.href)
  }
})
test('app, API, and www never redirect to the canonical root', () => {
  for (const host of ['app.minoduck.ai', 'api.minoduck.ai', 'www.minoduck.ai'])
    assert.equal(worker.fetch(new Request(`https://${host}/w/test/overview`)).status, 404)
})
