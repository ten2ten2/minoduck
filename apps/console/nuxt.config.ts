import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath } from 'node:url'

const uiComponentsDir = fileURLToPath(new URL('../../packages/ui/components', import.meta.url))

export default defineNuxtConfig({
  compatibilityDate: '2026-09-10',
  ssr: false,
  devtools: { enabled: false },
  modules: ['@nuxtjs/i18n'],
  css: [fileURLToPath(new URL('../../packages/ui/theme.css', import.meta.url))],
  components: [
    { path: uiComponentsDir, pathPrefix: false },
    { path: '~/components', pathPrefix: false },
  ],
  vite: { plugins: [tailwindcss()] },
  nitro: { preset: 'cloudflare_module', cloudflare: { deployConfig: false, nodeCompat: true } },
  runtimeConfig: {
    apiOrigin: 'http://localhost:8080',
    bffServiceToken: '',
    public: { siteUrl: 'http://localhost:3000' },
  },
  i18n: {
    strategy: 'no_prefix',
    defaultLocale: 'en',
    detectBrowserLanguage: false,
    locales: [
      { code: 'en', language: 'en', name: 'English' },
      { code: 'zh-hans', language: 'zh-Hans', name: '简体中文' },
      { code: 'zh-hant', language: 'zh-Hant', name: '繁體中文' },
    ],
  },
  app: {
    head: {
      title: 'MinoDuck',
      meta: [
        { name: 'robots', content: 'noindex,nofollow' },
        { name: 'description', content: 'Your AI costs, with the evidence behind every number.' },
      ],
    },
  },
  routeRules: {
    '/**': {
      headers: {
        'Cache-Control': 'private, no-store',
        'X-Robots-Tag': 'noindex, nofollow',
        'X-Content-Type-Options': 'nosniff',
        'X-Frame-Options': 'DENY',
        'Referrer-Policy': 'no-referrer',
        'Permissions-Policy': 'camera=(), microphone=(), geolocation=()',
        'Content-Security-Policy': "frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'",
        'Strict-Transport-Security': 'max-age=31536000',
      },
    },
  },
})
