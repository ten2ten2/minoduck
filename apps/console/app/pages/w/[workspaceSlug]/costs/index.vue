<script setup lang="ts">
const { t } = useI18n()
const { scoped, workspace, errorText } = useApi()
const { money, date } = useMoney()
const route = useRoute()
const now = new Date()
const filters = reactive({
  start: String(
    route.query.start ??
      `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-01`,
  ),
  end: String(route.query.end ?? new Date(Date.now() + 86400000).toISOString().slice(0, 10)),
  provider: String(route.query.provider ?? ''),
  currency: String(route.query.currency ?? ''),
  model: String(route.query.model ?? ''),
  cost_kind: String(route.query.cost_kind ?? 'actual'),
})
const page = ref(Number(route.query.page ?? 1)),
  message = ref(''),
  actionError = ref(''),
  busy = ref(false)
const { data, error, status, refresh } = await useAsyncData(
  () => `costs-${workspace.value?.id}`,
  (_app, { signal }) => scoped('/costs', { signal, query: { ...filters, page: page.value } }),
)
async function apply(nextPage = 1) {
  page.value = nextPage
  await navigateTo({ path: route.path, query: { ...filters, page: nextPage } }, { replace: true })
  await refresh()
}
async function exportCSV() {
  busy.value = true
  actionError.value = ''
  try {
    await scoped('/exports', { method: 'POST', query: { ...filters } })
    message.value = t('costs.exportCreated')
  } catch (e) {
    actionError.value = errorText(e)
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <div class="stack">
    <PageHeading :title="t('nav.costs')" :description="t('costs.subtitle')">
      <button :disabled="busy" @click="exportCSV">{{ t('costs.export') }}</button>
    </PageHeading>
    <form class="panel filters" @submit.prevent="apply()">
      <label>
        {{ t('common.start') }}
        <input v-model="filters.start" type="date" required />
      </label>
      <label>
        {{ t('common.end') }}
        <input v-model="filters.end" type="date" required />
      </label>
      <label>
        {{ t('common.provider') }}
        <select v-model="filters.provider">
          <option value="">{{ t('common.all') }}</option>
          <option>openai</option>
          <option>anthropic</option>
          <option>openrouter</option>
          <option>csv</option>
        </select>
      </label>
      <label>
        {{ t('common.currency') }}
        <select v-model="filters.currency">
          <option value="">{{ t('common.all') }}</option>
          <option>USD</option>
          <option>CNY</option>
          <option>EUR</option>
          <option>JPY</option>
        </select>
      </label>
      <label>
        {{ t('costs.basis') }}
        <select v-model="filters.cost_kind">
          <option
            v-for="kind in ['actual', 'billed', 'calculated', 'estimated']"
            :key="kind"
            :value="kind"
          >
            {{ t(`costKind.${kind}`) }}
          </option>
        </select>
      </label>
      <label>
        {{ t('common.model') }}
        <input v-model="filters.model" type="search" />
      </label>
      <button :disabled="status === 'pending'">{{ t('common.apply') }}</button>
    </form>
    <div v-if="message" class="notice" role="status">{{ message }}</div>
    <div v-if="actionError" class="notice error" role="alert">{{ actionError }}</div>
    <UpgradeNotice v-if="(error?.data as any)?.error?.code === 'HISTORY_LIMIT'" />
    <div v-else-if="error" class="notice error" role="alert">{{ errorText(error) }}</div>
    <article v-else class="panel" :aria-busy="status === 'pending'">
      <div v-if="data?.items?.length" class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>{{ t('common.start') }}</th>
              <th>{{ t('common.provider') }}</th>
              <th>{{ t('common.model') }}</th>
              <th>{{ t('common.category') }}</th>
              <th class="amount">{{ t('common.amount') }}</th>
              <th>{{ t('costs.basis') }}</th>
              <th>{{ t('common.source') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="entry in data.items" :key="entry.id">
              <td>
                <NuxtLink class="link" :to="`/w/${workspace?.slug}/costs/${entry.id}`">
                  {{ date(entry.period_start) }}
                </NuxtLink>
              </td>
              <td>{{ entry.billing_provider }}</td>
              <td>
                <span class="truncate" :title="entry.model">
                  {{ entry.model || t('costs.unknownModel') }}
                </span>
              </td>
              <td>{{ entry.charge_category }}</td>
              <td class="amount">{{ money(entry.amount, entry.currency) }}</td>
              <td>{{ t(`costKind.${entry.cost_kind}`) }}</td>
              <td>
                <span class="truncate" :title="entry.source_scope">{{ entry.source_scope }}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="empty">
        <h2>{{ t('costs.empty') }}</h2>
        <p>{{ t('costs.emptyHelp') }}</p>
      </div>
      <div class="pagination">
        <span class="muted">{{ t('common.page', { page, total: data?.total ?? 0 }) }}</span>
        <button :disabled="page <= 1 || status === 'pending'" @click="apply(page - 1)">
          {{ t('common.previous') }}
        </button>
        <button
          :disabled="page * 50 >= (data?.total ?? 0) || status === 'pending'"
          @click="apply(page + 1)"
        >
          {{ t('common.next') }}
        </button>
      </div>
    </article>
  </div>
</template>
