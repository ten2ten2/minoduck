<script setup lang="ts">
const props = defineProps<{ localizedRoutes?: boolean }>()
const { locale, setLocale, t } = useI18n()
const theme = useCookie<string>('md_theme', { default: () => 'system', sameSite: 'lax' })
const language = useCookie<string>('md_locale', { default: () => 'en', sameSite: 'lax' })
const emit = defineEmits<{ change: [locale: string, theme: string] }>()
const applyTheme = () => {
  if (!import.meta.client) return
  document.documentElement.dataset.theme =
    theme.value === 'system'
      ? matchMedia('(prefers-color-scheme: dark)').matches
        ? 'dark'
        : 'light'
      : theme.value
}
const changeLocale = async (event: Event) => {
  const value = (event.target as HTMLSelectElement).value as 'en' | 'zh-hans' | 'zh-hant'
  language.value = value
  await setLocale(value)
  emit('change', value, theme.value)
}
watch(theme, () => {
  applyTheme()
  emit('change', locale.value, theme.value)
})
let media: MediaQueryList | undefined
onMounted(() => {
  applyTheme()
  media = matchMedia('(prefers-color-scheme: dark)')
  media.addEventListener('change', applyTheme)
})
onUnmounted(() => media?.removeEventListener('change', applyTheme))
</script>
<template>
  <div class="preferences">
    <select :value="locale" :aria-label="t('common.language')" @change="changeLocale">
      <option value="en">English</option>
      <option value="zh-hans">简体中文</option>
      <option value="zh-hant">繁體中文</option>
    </select>
    <select v-model="theme" :aria-label="t('common.theme')">
      <option value="system">{{ t('common.system') }}</option>
      <option value="light">{{ t('common.light') }}</option>
      <option value="dark">{{ t('common.dark') }}</option>
    </select>
  </div>
</template>
