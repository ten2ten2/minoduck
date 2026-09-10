export default defineNuxtPlugin(async (nuxtApp) => {
  const route = useRoute()
  const localeCookie = useCookie<string>('md_locale')
  const allowed = ['en', 'zh-hans', 'zh-hant']
  const incoming = String(route.query.ui_locale ?? '').toLowerCase()
  const browser = navigator.language.toLowerCase()
  const detected =
    browser.startsWith('zh-tw') || browser.startsWith('zh-hk') || browser.startsWith('zh-hant')
      ? 'zh-hant'
      : browser.startsWith('zh')
        ? 'zh-hans'
        : 'en'
  const value = allowed.includes(incoming)
    ? incoming
    : allowed.includes(localeCookie.value)
      ? localeCookie.value
      : detected
  localeCookie.value = value
  await nuxtApp.$i18n.setLocale(value as 'en' | 'zh-hans' | 'zh-hant')
  if (incoming) {
    const query = { ...route.query }
    delete query.ui_locale
    await navigateTo({ path: route.path, query }, { replace: true })
  }
})
