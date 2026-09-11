import tailwindcss from '@tailwindcss/vite'
import { siteRoutes } from './shared/utils/site-routes'

export default defineNuxtConfig({
  extends: ['../../packages/ui'],
  vite: { plugins: [tailwindcss()] },
  nitro: {
    prerender: { routes: siteRoutes },
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
})
