<script setup lang="ts">
const { locale } = useI18n()
const route = useRoute()
const { user } = useSession()
const language = useCookie<string>('md_locale', { default: () => 'en', sameSite: 'lax' })
const theme = useCookie<string>('md_theme', { default: () => 'system', sameSite: 'lax' })
const locales = ['en', 'zh-hans', 'zh-hant'] as const
const themes = ['light', 'dark', 'system'] as const
const isLocale = (value: unknown): value is (typeof locales)[number] =>
  typeof value === 'string' && locales.includes(value as (typeof locales)[number])
const isTheme = (value: unknown): value is (typeof themes)[number] =>
  typeof value === 'string' && themes.includes(value as (typeof themes)[number])

const requestedLocale = Array.isArray(route.query.ui_locale)
  ? route.query.ui_locale[0]
  : route.query.ui_locale
if (isLocale(requestedLocale)) language.value = requestedLocale
if (isLocale(language.value)) locale.value = language.value

watch(
  user,
  (value) => {
    if (!value) return
    if (value.locale_explicit && isLocale(value.locale)) {
      language.value = value.locale
      locale.value = value.locale
    }
    if (isTheme(value.theme)) theme.value = value.theme
  },
  { immediate: true },
)

useHead(() => ({
  htmlAttrs: {
    lang: locale.value === 'zh-hans' ? 'zh-Hans' : locale.value === 'zh-hant' ? 'zh-Hant' : 'en',
  },
}))
</script>
<template>
  <NuxtLayout><NuxtPage /></NuxtLayout>
</template>
