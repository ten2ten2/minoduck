import assert from 'node:assert/strict'
import { test } from 'node:test'
import { ESLint } from 'eslint'

const eslint = new ESLint()
const filePath = 'apps/console/app/components/LintFixture.vue'

test('lint rejects explicit any in Vue scripts', async () => {
  const [result] = await eslint.lintText(
    '<script setup lang="ts">defineProps<{ value: any }>()</script><template><p>{{ value }}</p></template>',
    { filePath },
  )
  assert.ok(result.messages.some(({ ruleId }) => ruleId === '@typescript-eslint/no-explicit-any'))
})

test('accessibility lint checks beyond nested template boundaries', async () => {
  const [result] = await eslint.lintText(
    '<template><section><template v-if="true"><span>Nested</span></template><input type="text" /><img src="test.png" /></section></template>',
    { filePath },
  )
  for (const rule of ['form-control-has-label', 'alt-text']) {
    assert.ok(result.messages.some(({ ruleId }) => ruleId === `vuejs-accessibility/${rule}`))
  }
})

test('accessible nested labels remain valid', async () => {
  const [result] = await eslint.lintText(
    '<template><section><label>Name<input type="text" /></label><button>Save</button></section></template>',
    { filePath },
  )
  assert.equal(
    result.messages.filter(({ ruleId }) => ruleId?.startsWith('vuejs-accessibility/')).length,
    0,
  )
})
