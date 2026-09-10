<script setup lang="ts">
const { t } = useI18n()
const { scoped, workspace, errorText } = useApi()
const { money } = useMoney()
const busy = ref(false),
  actionError = ref('')
const codeError = (code: string) => errorText({ data: { error: { code } } })
const { data, error, refresh } = await useAsyncData(
  () => `insights-${workspace.value?.id}`,
  (_app, { signal }) => scoped<any[]>('/insights', { signal }),
)
async function update(id: string, state: string) {
  busy.value = true
  try {
    await scoped(`/insights/${id}`, { method: 'PATCH', body: { state } })
    await refresh()
  } catch (e) {
    actionError.value = errorText(e)
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <div class="stack">
    <PageHeading :title="t('nav.insights')" :description="t('insights.subtitle')">
      <button @click="refresh()">{{ t('common.refresh') }}</button>
    </PageHeading>
    <PriceTools v-if="workspace?.role !== 'viewer'" @updated="refresh()" />
    <div v-if="error || actionError" class="notice error" role="alert">
      {{ actionError || errorText(error) }}
    </div>
    <template v-if="data?.length">
      <article v-for="insight in data" :key="insight.id" class="panel stack">
        <div class="row between">
          <h2>{{ t(`alerts.${insight.kind}`) }}</h2>
          <StatusBadge :value="insight.state" />
        </div>
        <UpgradeNotice v-if="insight.details_locked" />
        <template v-else>
          <div class="row">
            <span v-if="insight.evidence.actual" class="badge num">
              {{ t('overview.actual') }}: {{ money(insight.evidence.actual, insight.currency) }}
            </span>
            <span v-if="insight.evidence.budget" class="badge num">
              {{ t('alerts.budget') }}: {{ money(insight.evidence.budget, insight.currency) }}
            </span>
            <span v-if="insight.evidence.baseline_median" class="badge num">
              {{ money(insight.evidence.baseline_median, insight.currency) }}
            </span>
          </div>
          <p v-if="insight.evidence.error_code" class="notice warning">
            {{ codeError(insight.evidence.error_code) }}
          </p>
          <p v-if="insight.estimated_savings" class="num">
            {{ t('prices.savings') }}: {{ money(insight.estimated_savings, insight.currency) }}
          </p>
          <p v-if="insight.kind === 'price_candidate'" class="notice warning">
            {{ t('prices.help') }}
          </p>
          <p class="muted">{{ t('insights.appliedNote') }}</p>
          <div v-if="workspace?.role !== 'viewer'" class="row">
            <button :disabled="busy" @click="update(insight.id, 'applied')">
              {{ t('insights.applied') }}
            </button>
            <button :disabled="busy" @click="update(insight.id, 'dismissed')">
              {{ t('insights.dismiss') }}
            </button>
          </div>
        </template>
      </article>
    </template>
    <div v-else-if="!error" class="panel empty">
      <h2>{{ t('insights.empty') }}</h2>
      <p>{{ t('insights.emptyHelp') }}</p>
      <NuxtLink class="button" :to="`/w/${workspace?.slug}/alerts`">{{ t('alerts.add') }}</NuxtLink>
    </div>
  </div>
</template>
