import tailwindcss from '@tailwindcss/vite'

export default defineNuxtConfig({
  extends: ['../../packages/ui'],
  ssr: false,
  components: [{ path: '~/components', pathPrefix: false }],
  vite: { plugins: [tailwindcss()] },
  runtimeConfig: {
    apiOrigin: 'http://localhost:8080',
    bffServiceToken: '',
    public: { siteUrl: 'http://localhost:3000' },
  },
  i18n: {
    strategy: 'no_prefix',
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
        'Referrer-Policy': 'no-referrer',
      },
    },
  },
})
