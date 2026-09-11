import assert from 'node:assert/strict'
import { test } from 'node:test'
import { siteContentPages, siteRoutes } from '../apps/site/shared/utils/site-routes.ts'

test('prerender and sitemap cover each content page once in every locale', () => {
  assert.equal(siteRoutes.length, 33)
  assert.equal(new Set(siteRoutes).size, siteRoutes.length)
  for (const prefix of ['', '/zh-hans', '/zh-hant']) {
    assert.ok(siteRoutes.includes(prefix || '/'))
    assert.ok(siteRoutes.includes(`${prefix}/pricing`))
    for (const page of siteContentPages) assert.ok(siteRoutes.includes(`${prefix}/${page}`))
  }
  assert.ok(siteRoutes.every((path) => path === '/' || !path.endsWith('/')))
})
