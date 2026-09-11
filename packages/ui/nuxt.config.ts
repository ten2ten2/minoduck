import { resolve } from 'node:path'

export default defineNuxtConfig({
  compatibilityDate: '2026-09-10',
  devtools: { enabled: false },
  modules: ['@nuxtjs/i18n'],
  css: [resolve(import.meta.dirname, 'theme.css')],
  components: [{ path: resolve(import.meta.dirname, 'components'), pathPrefix: false }],
  nitro: {
    preset: 'cloudflare-module',
    cloudflare: { deployConfig: false, nodeCompat: true },
  },
  i18n: {
    defaultLocale: 'en',
    detectBrowserLanguage: false,
    vueI18n: resolve(import.meta.dirname, 'i18n.config.ts'),
    locales: [
      { code: 'en', language: 'en', name: 'English' },
      { code: 'zh-hans', language: 'zh-Hans', name: '简体中文' },
      { code: 'zh-hant', language: 'zh-Hant', name: '繁體中文' },
    ],
  },
  routeRules: {
    '/**': {
      headers: {
        'X-Content-Type-Options': 'nosniff',
        'X-Frame-Options': 'DENY',
        'Referrer-Policy': 'strict-origin-when-cross-origin',
        'Permissions-Policy': 'camera=(), microphone=(), geolocation=()',
        'Content-Security-Policy':
          "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; frame-src 'none'; frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'",
        'Strict-Transport-Security': 'max-age=31536000',
      },
    },
  },
})
