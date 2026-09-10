<script setup lang="ts">
import { Plug, ArrowUpRight } from '@lucide/vue'
const { t } = useI18n()
const { scoped, workspace, errorText } = useApi()
const { money, date } = useMoney()
const { data, error, status, refresh } = await useAsyncData(
  () => `overview-${workspace.value?.id}`,
  (_app, { signal }) => scoped('/overview', { signal }),
)
const currency = ref('USD')
const currencies = computed<string[]>(() => data.value?.totals?.map((v: any) => v.currency) ?? [])
watch(
  currencies,
  (values) => {
    if (values.length && !values.includes(currency.value)) currency.value = values[0]!
  },
  { immediate: true },
)
const total = computed(() => data.value?.totals?.find((v: any) => v.currency === currency.value))
const billed = computed(() => data.value?.billed?.find((v: any) => v.currency === currency.value))
const providers = computed(
  () => data.value?.providers?.filter((v: any) => v.currency === currency.value) ?? [],
)
const maxProvider = computed(() =>
  Math.max(1, ...providers.value.map((v: any) => Number(v.amount))),
)
</script>
<template>
  <div class="stack">
    <PageHeading :title="t('nav.overview')" :description="t('overview.subtitle')">
      <button :disabled="status === 'pending'" @click="refresh()">
        {{ t('common.refresh') }}
      </button>
    </PageHeading>
    <div v-if="error" class="notice error" role="alert">{{ errorText(error) }}</div>
    <div v-else-if="status === 'pending'" class="grid-4" aria-busy="true">
      <div v-for="n in 4" :key="n" class="skeleton" />
    </div>
    <template v-else-if="data">
      <div v-if="!data.sources.length" class="panel empty">
        <Plug />
        <h2>{{ t('overview.noSources') }}</h2>
        <p>{{ t('overview.noSourcesHelp') }}</p>
        <NuxtLink class="button primary" :to="`/w/${workspace?.slug}/connections`">
          {{ t('connections.add') }}
        </NuxtLink>
      </div>
      <template v-else>
        <div class="row between">
          <div class="row">
            <span class="eyebrow">{{ t('overview.actual') }}</span>
            <StatusBadge :value="data.coverage === 'empty' ? 'pending_source' : data.coverage" />
          </div>
          <label v-if="currencies.length">
            <span class="visually-hidden">{{ t('common.currency') }}</span>
            <select v-model="currency">
              <option v-for="cur in currencies" :key="cur">{{ cur }}</option>
            </select>
          </label>
        </div>
        <div class="grid-4">
          <article class="panel metric">
            <p class="muted">{{ t('overview.actual') }}</p>
            <div class="metric-value">{{ money(total?.amount, currency) }}</div>
            <small class="muted">{{ t('costKind.actual') }} · {{ t('common.period') }}</small>
          </article>
          <article class="panel metric">
            <p class="muted">{{ t('overview.billed') }}</p>
            <div class="metric-value">{{ money(billed?.amount, currency) }}</div>
            <small class="muted">{{ t('costKind.billed') }}</small>
          </article>
          <article class="panel metric">
            <p class="muted">{{ t('overview.differences') }}</p>
            <div class="metric-value">{{ data.unexplained_count }}</div>
            <NuxtLink class="link row" :to="`/w/${workspace?.slug}/reconciliation`">
              <small>{{ t('common.view') }}</small>
              <ArrowUpRight :size="14" />
            </NuxtLink>
          </article>
          <article class="panel metric">
            <p class="muted">{{ t('overview.opportunities') }}</p>
            <div class="metric-value">{{ data.insight_count }}</div>
            <NuxtLink class="link row" :to="`/w/${workspace?.slug}/insights`">
              <small>{{ t('common.view') }}</small>
              <ArrowUpRight :size="14" />
            </NuxtLink>
          </article>
        </div>
        <p class="muted">
          <small>{{ t('baseline.note') }}</small>
        </p>
        <div class="overview-charts">
          <article class="panel">
            <div class="panel-title">
              <h2>{{ t('overview.trend') }}</h2>
              <span class="badge">{{ t('costKind.actual') }}</span>
            </div>
            <CostChart :rows="data.trend" :currency="currency" />
          </article>
          <article class="panel">
            <h2>{{ t('overview.breakdown') }}</h2>
            <div v-if="providers.length" class="provider-breakdown">
              <div v-for="p in providers" :key="p.provider">
                <div class="row between">
                  <span>{{ p.provider }}</span>
                  <strong class="num">{{ money(p.amount, p.currency) }}</strong>
                </div>
                <div class="bar-track">
                  <div
                    :style="{ width: `${(Math.max(0, Number(p.amount)) / maxProvider) * 100}%` }"
                  />
                </div>
              </div>
            </div>
            <div v-else class="empty">{{ t('common.unavailable') }}</div>
          </article>
        </div>
        <article class="panel">
          <div class="panel-title">
            <h2>{{ t('overview.sources') }}</h2>
            <NuxtLink class="link" :to="`/w/${workspace?.slug}/connections`">
              {{ t('nav.connections') }}
            </NuxtLink>
          </div>
          <div class="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>{{ t('common.name') }}</th>
                  <th>{{ t('common.provider') }}</th>
                  <th>{{ t('common.status') }}</th>
                  <th>{{ t('connections.through') }} · UTC</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="source in data.sources" :key="source.id">
                  <td>{{ source.name }}</td>
                  <td>{{ source.provider }}</td>
                  <td><StatusBadge :value="source.status" /></td>
                  <td>{{ date(source.data_through) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </article>
        <div class="notice warning">
          <strong>{{ t('overview.coverage') }}</strong>
          <p>{{ t('overview.coverageHelp') }}</p>
        </div>
      </template>
    </template>
  </div>
</template>
<style scoped>
.overview-charts {
  display: grid;
  grid-template-columns: 1.8fr 1fr;
  gap: 1.2rem;
}
.provider-breakdown {
  display: grid;
  gap: 1.4rem;
  margin-top: 1.5rem;
  font-size: 0.875rem;
}
.bar-track {
  height: 7px;
  background: var(--bg);
  border-radius: 3px;
  margin-top: 0.7rem;
  overflow: hidden;
}
.bar-track > div {
  height: 100%;
  background: var(--accent);
  border-radius: 3px;
}
@media (max-width: 1150px) {
  .overview-charts {
    grid-template-columns: 1fr;
  }
}
</style>
