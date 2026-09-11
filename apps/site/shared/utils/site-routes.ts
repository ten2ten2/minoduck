export const siteContentPages = [
  'integrations',
  'integrations/openai',
  'integrations/anthropic',
  'integrations/openrouter',
  'integrations/csv',
  'docs',
  'security',
  'privacy',
  'terms',
]

const paths = ['', '/pricing', ...siteContentPages.map((page) => `/${page}`)]
export const siteRoutes = ['', '/zh-hans', '/zh-hant'].flatMap((prefix) =>
  paths.map((path) => prefix + path || '/'),
)
