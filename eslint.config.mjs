import { createConfigForNuxt } from '@nuxt/eslint-config'
import accessibility from 'eslint-plugin-vuejs-accessibility'

export default createConfigForNuxt({
  features: { typescript: true },
  dirs: {
    root: ['apps/console', 'apps/site', 'packages/ui'],
    src: ['apps/console/app', 'apps/site/app', 'packages/ui'],
  },
})
  .append(accessibility.configs['flat/recommended'])
  .append({
    files: ['**/*.vue'],
    rules: {
      'vue/html-self-closing': ['error', { html: { void: 'always' } }],
      'vuejs-accessibility/label-has-for': ['error', { required: { some: ['nesting', 'id'] } }],
    },
  })
  .append({
    files: ['apps/**/*.{ts,vue}', 'packages/**/*.{ts,vue}', 'infra/**/*.mjs'],
    rules: {
      'no-console': ['error', { allow: ['warn', 'error'] }],
      '@typescript-eslint/no-explicit-any': 'error',
    },
  })
