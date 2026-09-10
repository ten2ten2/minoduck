import { readFile, readdir } from 'node:fs/promises'
import assert from 'node:assert/strict'
const root = new URL('../packages/locales/', import.meta.url)
const flatten = (v, prefix = '') =>
  Object.entries(v).flatMap(([k, val]) =>
    typeof val === 'object' ? flatten(val, `${prefix}${k}.`) : [`${prefix}${k}`],
  )
const catalogs = await Promise.all(
  ['en', 'zh-hans', 'zh-hant'].map(async (l) =>
    JSON.parse(await readFile(new URL(`${l}.json`, root), 'utf8')),
  ),
)
const keys = flatten(catalogs[0]).sort()
for (const [i, catalog] of catalogs.entries())
  assert.deepEqual(flatten(catalog).sort(), keys, `Locale ${i} is missing keys`)
const lookup = (key) => key.split('.').reduce((obj, k) => obj?.[k], catalogs[0])
async function scan(dir) {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    if (entry.name.startsWith('.') || entry.name === 'node_modules') continue
    const url = new URL(entry.name + (entry.isDirectory() ? '/' : ''), dir)
    if (entry.isDirectory()) await scan(url)
    else if (/\.(vue|ts)$/.test(entry.name)) {
      const text = await readFile(url, 'utf8')
      for (const match of text.matchAll(/\bt\(['"]([a-zA-Z][\w.]*)['"]/g))
        assert.ok(lookup(match[1]), `Missing ${match[1]} in ${url.pathname}`)
    }
  }
}
await scan(new URL('../apps/', import.meta.url))
await scan(new URL('../packages/ui/', import.meta.url))
console.log(`Verified ${keys.length} matching keys across three locales and literal UI references.`)
