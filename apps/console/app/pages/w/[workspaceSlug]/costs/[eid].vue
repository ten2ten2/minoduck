<script setup lang="ts">
const { t } = useI18n()
const { scoped, workspace, errorText } = useApi()
const { money, date } = useMoney()
const route = useRoute()
const { data, error } = await useAsyncData(
  () => `cost-${workspace.value?.id}-${route.params.eid}`,
  (_app, { signal }) => scoped<CostDetailResponse>(`/costs/${route.params.eid}`, { signal }),
)
</script>
<template>
  <div class="stack">
    <PageHeading :title="t('costs.history')">
      <NuxtLink class="button" :to="`/w/${workspace?.slug}/costs`">{{ t('nav.costs') }}</NuxtLink>
    </PageHeading>
    <div v-if="error" class="notice error" role="alert">{{ errorText(error) }}</div>
    <template v-else-if="data">
      <div class="panel stack">
        <h2>{{ money(data.entry.amount, data.entry.currency) }}</h2>
        <p>
          {{ data.entry.billing_provider }} · {{ data.entry.model || t('costs.unknownModel') }} ·
          {{ t(`costKind.${data.entry.cost_kind}`) }}
        </p>
        <p class="muted">
          {{ data.entry.source_scope }} · {{ date(data.entry.period_start) }} —
          {{ date(data.entry.period_end) }}
        </p>
        <StatusBadge :value="data.entry.coverage" />
      </div>
      <div class="panel table-scroll">
        <table>
          <thead>
            <tr>
              <th>{{ t('costs.revision') }}</th>
              <th class="amount">{{ t('common.amount') }}</th>
              <th>{{ t('common.source') }}</th>
              <th>{{ t('common.start') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="revision in data.history" :key="revision.revision">
              <td>{{ revision.revision }}</td>
              <td class="amount">{{ money(revision.amount, revision.currency) }}</td>
              <td>
                <code>{{ revision.source_batch_id }}</code>
                <br />
                <small>{{ revision.source_record_ref }}</small>
              </td>
              <td>{{ date(revision.ingested_at) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </div>
</template>
