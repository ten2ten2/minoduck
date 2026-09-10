<script setup lang="ts">
const { t, locale } = useI18n()
const config = useRuntimeConfig()
useSeoMeta({
  title: () => `${t('nav.pricing')} · MinoDuck`,
  description: () => t('pricing.predictable'),
})
async function choose(plan: string, interval: string) {
  await navigateTo(
    `${config.public.appUrl}/login?ui_locale=${locale.value}&plan=${plan}&interval=${interval}`,
    { external: true },
  )
}
</script>
<template>
  <section class="site-section stack">
    <h1>{{ t('nav.pricing') }}</h1>
    <PricingCards :contact-email="config.public.contactEmail" @choose="choose" />
  </section>
</template>
