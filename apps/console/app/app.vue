<script setup lang="ts">
const { locale } = useI18n()
useHead(() => ({
  htmlAttrs: {
    lang: locale.value === 'zh-hans' ? 'zh-Hans' : locale.value === 'zh-hant' ? 'zh-Hant' : 'en',
  },
}))
</script>
<template>
  <NuxtLayout><NuxtPage /></NuxtLayout>
</template>
