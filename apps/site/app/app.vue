<script setup lang="ts">
const { t, locale } = useI18n()
const localePath = useLocalePath()
const switchLocalePath = useSwitchLocalePath()
const route = useRoute()
const config = useRuntimeConfig()
const appLink = computed(() => `${config.public.appUrl}/login?ui_locale=${locale.value}`)
useHead(() => ({
  htmlAttrs: {
    lang: locale.value === 'zh-hans' ? 'zh-Hans' : locale.value === 'zh-hant' ? 'zh-Hant' : 'en',
  },
  link: [
    { rel: 'canonical', href: config.public.siteUrl + route.path },
    ...(['en', 'zh-hans', 'zh-hant'] as const).map((code) => ({
      rel: 'alternate' as const,
      hreflang: code === 'zh-hans' ? 'zh-Hans' : code === 'zh-hant' ? 'zh-Hant' : 'en',
      href: config.public.siteUrl + switchLocalePath(code),
    })),
  ],
}))
</script>
<template>
  <div class="site">
    <header class="site-header">
      <NuxtLink :to="localePath('/')"><BrandWordmark /></NuxtLink>
      <nav>
        <NuxtLink
          v-for="page in ['pricing', 'integrations', 'docs']"
          :key="page"
          :to="localePath(`/${page}`)"
        >
          {{ t(`nav.${page}`) }}
        </NuxtLink>
      </nav>
      <div class="row">
        <PreferencesControl localized-routes />
        <a :href="appLink" class="button primary">{{ t('site.openApp') }}</a>
      </div>
    </header>
    <main><NuxtPage /></main>
    <footer>
      <BrandWordmark />
      <div class="row">
        <NuxtLink
          v-for="page in ['security', 'privacy', 'terms']"
          :key="page"
          :to="localePath(`/${page}`)"
        >
          {{ t(page === 'security' ? 'nav.security' : `site.${page}`) }}
        </NuxtLink>
      </div>
      <small class="muted">© {{ new Date().getUTCFullYear() }} MinoDuck</small>
    </footer>
  </div>
</template>
<style>
.site {
  max-width: 1380px;
  margin: auto;
  padding: 0 3rem;
}
.site-header {
  min-height: 92px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1.5rem;
  border-bottom: 1px solid var(--line);
  flex-wrap: wrap;
}
.site-header nav {
  display: flex;
  gap: 1.6rem;
  font-size: 0.875rem;
}
.site-header nav a:hover {
  color: var(--accent);
}
.site > main {
  min-height: 70vh;
}
footer {
  border-top: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  flex-wrap: wrap;
  padding: 2.2rem 0;
  font-size: 0.875rem;
}
.site-section {
  padding: 4rem 0;
}
.site-section > h1 {
  max-width: 780px;
  margin-bottom: 1.5rem;
}
.site-section > .muted {
  margin-bottom: 2rem;
  max-width: 780px;
}
@media (max-width: 1050px) {
  .site {
    padding: 0 1.5rem;
  }
  .site-header {
    padding: 1.2rem 0;
  }
  .site-header nav {
    order: 3;
    width: 100%;
  }
}
@media (max-width: 600px) {
  .site {
    padding: 0 1rem;
  }
  .site-header > .row {
    width: 100%;
    justify-content: space-between;
  }
  .site-header .preferences select {
    max-width: 130px;
  }
  .site-section {
    padding: 2.5rem 0;
  }
}
</style>
