export default defineEventHandler((event) => {
  const config = useRuntimeConfig(event)
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
  setResponseHeader(event, 'Content-Type', 'application/xml; charset=utf-8')
  return (
    '<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">' +
    ['', '/zh-hans', '/zh-hant']
      .flatMap((prefix) =>
        paths.map(
          (path) => `<url><loc>${config.public.siteUrl}${prefix + path || '/'}</loc></url>`,
        ),
      )
      .join('') +
    '</urlset>'
  )
})
