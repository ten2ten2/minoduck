<script setup lang="ts">
const { t, locale } = useI18n()
const localePath = useLocalePath()
const config = useRuntimeConfig()
useSeoMeta({
  title: () => `MinoDuck · ${t('site.eyebrow')}`,
  description: () => t('site.subtitle'),
})
</script>
<template>
  <div>
    <section class="hero">
      <div class="hero-copy">
        <span class="eyebrow">{{ t('site.eyebrow') }}</span>
        <h1>{{ t('site.title') }}</h1>
        <p>{{ t('site.subtitle') }}</p>
        <div class="row">
          <a :href="`${config.public.appUrl}/login?ui_locale=${locale}`" class="button primary">
            {{ t('pricing.startFree') }}
          </a>
          <NuxtLink :to="localePath('/integrations')" class="button">
            {{ t('nav.integrations') }}
          </NuxtLink>
        </div>
        <small class="muted">{{ t('site.noProxy') }}</small>
      </div>
      <div class="ledger-story">
        <div class="ledger-head">
          <span class="brand-mark" aria-hidden="true">md</span>
          <span>{{ t('site.one') }}</span>
        </div>
        <div
          v-for="provider in ['OpenAI', 'Anthropic', 'OpenRouter']"
          :key="provider"
          class="source-row"
        >
          <strong>{{ provider }}</strong>
          <span>{{ t('costKind.actual') }}</span>
          <span class="source-line" aria-hidden="true" />
        </div>
        <div class="source-row">
          <strong>CSV</strong>
          <span>{{ t('nav.invoices') }}</span>
          <span class="source-line" aria-hidden="true" />
        </div>
        <div class="ledger-outcome">
          <strong>{{ t('site.two') }}</strong>
          <p>{{ t('site.three') }}</p>
        </div>
      </div>
    </section>
    <section class="value-grid">
      <article v-for="(key, index) in ['one', 'two', 'three']" :key="key">
        <span class="eyebrow">0{{ index + 1 }}</span>
        <h2>{{ t(`site.${key}`) }}</h2>
        <p class="muted">{{ t(`site.${key}Body`) }}</p>
      </article>
    </section>
  </div>
</template>
<style scoped>
.hero {
  display: grid;
  grid-template-columns: 1.2fr 1fr;
  align-items: center;
  gap: 5rem;
  padding: 6rem 0 5rem;
}
.hero-copy {
  display: grid;
  gap: 1.6rem;
}
.hero h1 {
  font-size: clamp(2.5rem, 4.5vw, 4.25rem);
  line-height: 1.06;
  letter-spacing: -0.06em;
}
.hero-copy > p {
  font-size: 1.1rem;
  line-height: 1.75;
  color: var(--muted);
  max-width: 550px;
}
.hero-copy .button {
  padding: 0.8rem 1.1rem;
}
.ledger-story {
  border: 1px solid var(--line);
  border-radius: 16px;
  background: var(--panel);
  overflow: hidden;
  box-shadow: 0 25px 70px #12356712;
  transform: rotate(-1deg);
}
.ledger-head {
  display: flex;
  align-items: center;
  gap: 0.8rem;
  padding: 1.5rem;
  background: #15243a;
  color: #fff;
  font-size: 0.95rem;
}
.source-row {
  display: grid;
  grid-template-columns: 1fr 1fr 40px;
  align-items: center;
  gap: 1rem;
  padding: 1.15rem 1.5rem;
  border-bottom: 1px solid var(--line);
  font-size: 0.875rem;
}
.source-row > span {
  color: var(--muted);
}
.source-line {
  height: 5px;
  background: var(--accent-soft);
  border-radius: 2px;
}
.ledger-outcome {
  padding: 1.5rem;
  border-left: 5px solid #efbc4b;
}
.ledger-outcome p {
  color: var(--muted);
  margin-top: 0.3rem;
}
.value-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 3rem;
  padding: 2.5rem 0 5rem;
}
.value-grid h2 {
  margin: 1rem 0 0.8rem;
  font-size: 1.3rem;
}
.value-grid p {
  font-size: 0.95rem;
}
@media (max-width: 1000px) {
  .hero {
    gap: 2rem;
    padding: 4rem 0;
    grid-template-columns: 1fr;
  }
  .ledger-story {
    max-width: 600px;
    width: 100%;
  }
  .value-grid {
    gap: 1.5rem;
  }
}
@media (max-width: 650px) {
  .value-grid {
    grid-template-columns: 1fr;
    padding-bottom: 3rem;
  }
  .hero h1 {
    font-size: 2.7rem;
  }
}
</style>
