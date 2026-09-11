export default defineEventHandler((event) => {
  const config = useRuntimeConfig(event)
  setResponseHeader(event, 'Content-Type', 'text/plain; charset=utf-8')
  return `User-agent: *\nAllow: /\nSitemap: ${config.public.siteUrl}/sitemap.xml\n`
})
