<script setup lang="ts">
const { t } = useI18n()
const route = useRoute()
const localePath = useLocalePath()
const config = useRuntimeConfig()
const slug = computed(() =>
  Array.isArray(route.params.slug) ? route.params.slug.join('/') : String(route.params.slug ?? ''),
)
if (!siteContentPages.includes(slug.value))
  throw createError({ statusCode: 404, statusMessage: 'Page not found' })
const provider = computed(() =>
  slug.value.startsWith('integrations/') ? slug.value.split('/')[1] : '',
)
const title = computed(() =>
  provider.value
    ? (
        {
          openai: 'OpenAI',
          anthropic: 'Anthropic',
          openrouter: 'OpenRouter',
          csv: 'CSV',
        } as Record<string, string>
      )[provider.value]
    : t(['privacy', 'terms'].includes(slug.value) ? `site.${slug.value}` : `nav.${slug.value}`),
)
useSeoMeta({ title: () => `${title.value} · MinoDuck`, description: () => t('site.readOnly') })
</script>
<template>
  <section class="site-section stack">
    <h1>{{ title }}</h1>
    <template v-if="slug === 'integrations'">
      <p class="muted">{{ t('site.integrationsIntro') }}</p>
      <div class="grid-2">
        <NuxtLink
          v-for="p in ['openai', 'anthropic', 'openrouter', 'csv']"
          :key="p"
          class="panel stack"
          :to="localePath(`/integrations/${p}`)"
        >
          <h2>
            {{
              { openai: 'OpenAI', anthropic: 'Anthropic', openrouter: 'OpenRouter', csv: 'CSV' }[p]
            }}
          </h2>
          <p class="muted">{{ t(`providers.${p}Warning`) }}</p>
          <span class="link">{{ t('common.view') }}</span>
        </NuxtLink>
      </div>
      <p class="notice">{{ t('site.readOnly') }}</p>
    </template>
    <template v-else-if="provider">
      <p class="notice warning">{{ t(`providers.${provider}Warning`) }}</p>
      <p>{{ t('site.readOnly') }}</p>
      <NuxtLink class="button" :to="localePath('/docs')">{{ t('nav.docs') }}</NuxtLink>
    </template>
    <template v-else-if="slug === 'docs'">
      <h2>{{ t('site.docsIntro') }}</h2>
      <p>{{ t('site.docsSteps') }}</p>
      <div class="grid-2">
        <article v-for="key in ['one', 'two', 'three']" :key="key" class="panel stack">
          <h2>{{ t(`site.${key}`) }}</h2>
          <p class="muted">{{ t(`site.${key}Body`) }}</p>
        </article>
      </div>
      <p class="notice">{{ t('imports.limit') }}</p>
      <a class="button" :href="`${config.public.appUrl}/templates/cost-report.csv`" download>
        {{ t('imports.template') }}
      </a>
    </template>
    <template v-else-if="slug === 'security'">
      <p>{{ t('site.securityBody') }}</p>
      <p class="notice">{{ t('site.readOnly') }}</p>
    </template>
    <template v-else>
      <p class="notice warning">{{ t('site.legalDraft') }}</p>
      <p>{{ t(slug === 'privacy' ? 'site.privacyBody' : 'site.termsBody') }}</p>
    </template>
  </section>
</template>
