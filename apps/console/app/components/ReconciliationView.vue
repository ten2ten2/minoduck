<script setup lang="ts">
const props = defineProps<{ id?: string }>()
const { t } = useI18n()
const { scoped, workspace, errorText } = useApi()
const { money, date } = useMoney()
const { data, error, refresh } = await useAsyncData(
  () => `reconciliation-${workspace.value?.id}-${props.id ?? 'all'}`,
  (_app, { signal }) =>
    scoped(props.id ? `/reconciliation-runs/${props.id}` : '/reconciliation-runs', { signal }),
)
const handling = ref('explained'),
  note = ref(''),
  busy = ref(false),
  actionError = ref('')
async function save() {
  busy.value = true
  actionError.value = ''
  try {
    await scoped(`/reconciliation-items/${props.id}`, {
      method: 'PATCH',
      body: { handling_status: handling.value, note: note.value },
    })
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
    <PageHeading :title="t('nav.reconciliation')" :description="t('reconciliation.subtitle')">
      <NuxtLink class="button" :to="`/w/${workspace?.slug}/invoices`">
        {{ t('invoices.add') }}
      </NuxtLink>
    </PageHeading>
    <UsageReconciliationForm v-if="!id && workspace?.role !== 'viewer'" @created="refresh()" />
    <UpgradeNotice v-if="(error?.data as any)?.error?.code === 'UPGRADE_REQUIRED'" />
    <div v-else-if="error" class="notice error" role="alert">{{ errorText(error) }}</div>
    <template v-else-if="id && data">
      <div class="row between">
        <h2>{{ t('reconciliation.version', { version: data.run_version }) }}</h2>
        <StatusBadge :value="data.match_status" />
      </div>
      <div class="grid-3">
        <article class="panel metric">
          <p class="muted">{{ t('reconciliation.expected') }}</p>
          <div class="metric-value">{{ money(data.expected, data.currency) }}</div>
        </article>
        <article class="panel metric">
          <p class="muted">
            {{ t(data.level === 'L1' ? 'reconciliation.actualCompared' : 'reconciliation.billed') }}
          </p>
          <div class="metric-value">{{ money(data.billed, data.currency) }}</div>
        </article>
        <article class="panel metric">
          <p class="muted">{{ t('reconciliation.difference') }}</p>
          <div class="metric-value">{{ money(data.difference, data.currency) }}</div>
        </article>
      </div>
      <p class="muted">{{ t('reconciliation.tolerance') }}</p>
      <article class="panel stack">
        <h2>{{ t('costs.evidence') }}</h2>
        <p>
          {{ data.evidence.source_scope }} · {{ date(data.evidence.period_start) }} —
          {{ date(data.evidence.period_end) }}
        </p>
        <template v-if="data.level === 'L1'">
          <div v-if="data.evidence.price_evidence?.length" class="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>{{ t('common.source') }}</th>
                  <th>{{ t('prices.basis') }}</th>
                  <th class="amount">{{ t('common.amount') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="entry in data.evidence.price_evidence" :key="entry.usage_id">
                  <td>
                    <a class="link" :href="entry.reference" target="_blank" rel="noopener noreferrer">
                      {{ entry.price_version_id }}
                    </a>
                  </td>
                  <td>{{ t(`prices.${entry.price_basis}`) }}</td>
                  <td class="amount">{{ money(entry.amount, data.currency) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <p v-else class="muted">{{ t('reconciliation.usageHelp') }}</p>
        </template>
        <template v-else>
          <p>{{ t('invoices.adjustment') }}: {{ money(data.evidence.adjustment, data.currency) }}</p>
          <div class="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>{{ t('common.source') }}</th>
                  <th>{{ t('costs.revision') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="entry in data.evidence.entry_refs" :key="entry.entry_id">
                  <td>
                    <NuxtLink class="link" :to="`/w/${workspace?.slug}/costs/${entry.entry_id}`">
                      {{ entry.entry_id }}
                    </NuxtLink>
                  </td>
                  <td>{{ entry.revision }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>
      </article>
      <form v-if="workspace?.role !== 'viewer'" class="panel stack" @submit.prevent="save">
        <h2>{{ t('reconciliation.handling') }}</h2>
        <StatusBadge :value="data.handling_status" />
        <p v-if="data.handling_note">{{ data.handling_note }}</p>
        <div v-if="actionError" class="notice error" role="alert">{{ actionError }}</div>
        <label>
          {{ t('reconciliation.handling') }}
          <select v-model="handling">
            <option v-for="value in ['explained', 'ignored', 'open']" :key="value" :value="value">
              {{ t(`status.${value}`) }}
            </option>
          </select>
        </label>
        <label>
          {{ t('reconciliation.note') }}
          <textarea v-model="note" required maxlength="4000" />
        </label>
        <div>
          <button class="primary" :disabled="busy">{{ t('common.save') }}</button>
        </div>
      </form>
    </template>
    <div v-else-if="data?.length" class="panel table-scroll">
      <table>
        <thead>
          <tr>
            <th>{{ t('common.start') }}</th>
            <th>{{ t('costs.revision') }}</th>
            <th>{{ t('common.status') }}</th>
            <th class="amount">{{ t('reconciliation.difference') }}</th>
            <th>{{ t('reconciliation.handling') }}</th>
            <th />
          </tr>
        </thead>
        <tbody>
          <tr v-for="run in data" :key="run.id">
            <td>{{ date(run.created_at) }}</td>
            <td>{{ run.run_version }}</td>
            <td><StatusBadge :value="run.match_status" /></td>
            <td class="amount">{{ money(run.difference, run.currency) }}</td>
            <td><StatusBadge :value="run.handling_status" /></td>
            <td>
              <NuxtLink
                class="link"
                :to="
                  run.details_locked
                    ? `/w/${workspace?.slug}/settings/billing`
                    : `/w/${workspace?.slug}/reconciliation/${run.id}`
                "
              >
                {{ t(run.details_locked ? 'upgrade.button' : 'common.view') }}
              </NuxtLink>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-else-if="!error" class="panel empty">
      <h2>{{ t('reconciliation.empty') }}</h2>
      <p>{{ t('reconciliation.emptyHelp') }}</p>
    </div>
    <div class="notice">{{ t('reconciliation.notPayment') }}</div>
  </div>
</template>
