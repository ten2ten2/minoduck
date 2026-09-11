import test from 'node:test'
import assert from 'node:assert/strict'
import { isAllowedBffRoute } from '../apps/console/server/utils/bff-routes.ts'

const wid = '11111111-1111-4111-8111-111111111111'
const id = '22222222-2222-4222-8222-222222222222'

for (const [method, path] of [
  ['GET', '/me'],
  ['GET', '/providers'],
  ['POST', '/auth/email/start'],
  ['GET', '/auth/google/callback'],
  ['POST', '/invitations/accept'],
  ['GET', `/workspaces/${wid}/costs/${id}`],
  ['GET', `/workspaces/${wid}/exports/${id}/download`],
  ['POST', `/workspaces/${wid}/connections/${id}/credentials`],
  ['POST', `/workspaces/${wid}/subscription/checkout`],
  ['POST', `/workspaces/${wid}/subscription/resume`],
  ['PATCH', `/workspaces/${wid}/reconciliation-items/${id}`],
  ['DELETE', `/workspaces/${wid}/connections/${id}`],
]) {
  test(`BFF allows ${method} ${path}`, () => {
    assert.equal(isAllowedBffRoute(method, path), true)
  })
}

for (const [method, path] of [
  ['POST', '/webhooks/stripe'],
  ['GET', '/https://example.com/'],
  ['GET', '//example.com/'],
  ['GET', `/workspaces/not-a-uuid/costs`],
  ['POST', `/workspaces/${wid}/costs`],
  ['POST', `/workspaces/${wid}/subscription/resume/extra`],
  ['GET', `/workspaces/${wid}/exports/${id}/download/extra`],
  ['PATCH', `/workspaces/${wid}/unknown/${id}`],
  ['DELETE', `/workspaces/${wid}`],
  ['OPTIONS', '/me'],
  ['GET', '/me/extra'],
]) {
  test(`BFF rejects ${method} ${path}`, () => {
    assert.equal(isAllowedBffRoute(method, path), false)
  })
}
