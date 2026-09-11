import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath } from 'node:url'

const paths = [
  '',
  '/pricing',
  '/integrations',
  '/integrations/openai',
  '/integrations/anthropic',
  '/integrations/openrouter',
  '/integrations/csv',
  '/docs',
  '/security',
  '/privacy',
  '/terms',
]
const uiComponentsDir = fileURLToPath(new URL('../../packages/ui/components', import.meta.url))

export default defineNuxtConfig({
  compatibilityDate: '2026-09-10',
  devtools: { enabled: false },
  modules: ['@nuxtjs/i18n'],
  css: [fileURLToPath(new URL('../../packages/ui/theme.css', import.meta.url))],
  components: [
    { path: uiComponentsDir, pathPrefix: false },
    { path: '~/components', pathPrefix: false },
  ],
  vite: { plugins: [tailwindcss()] },
  nitro: {
    preset: 'cloudflare_module',
    cloudflare: { deployConfig: false, nodeCompat: true },
    prerender: {
      routes: ['', '/zh-hans', '/zh-hant'].flatMap((prefix) =>
        paths.map((path) => prefix + path || '/'),
      ),
    },
  },
  runtimeConfig: {
    public: {
      appUrl: 'http://localhost:3001',
      siteUrl: 'http://localhost:3000',
      contactEmail: '',
    },
  },
  i18n: {
    strategy: 'prefix_except_default',
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
      title: 'MinoDuck · AI Billing Intelligence',
      meta: [
        {
          name: 'description',
          content:
            'Bring AI provider costs together. Reconcile the difference. Make informed cost decisions.',
        },
      ],
    },
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
