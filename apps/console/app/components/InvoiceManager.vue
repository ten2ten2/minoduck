<script setup lang="ts">
const props = defineProps<{ id?: string }>()
const { t } = useI18n()
const { scoped, workspace, errorText } = useApi()
const { money, date } = useMoney()
const { data, error, refresh } = await useAsyncData(
  () => `invoices-${workspace.value?.id}-${props.id ?? 'all'}`,
  async (_app, { signal }) => {
    const [invoices, accounts] = await Promise.all([
      scoped<any[]>('/invoices', { signal }),
      scoped<any[]>('/connections', { signal }),
    ])
    return { invoices, accounts }
  },
)
const selected = computed(() => data.value?.invoices.find((v) => v.id === props.id))
const open = ref(false),
  busy = ref(false),
  actionError = ref('')
const form = reactive({
  account_id: '',
  reference: '',
  currency: 'USD',
  period_start: '',
  period_end: '',
  amount: '',
  adjustment: '0',
  source_scope: 'cost-report',
  coverage_confirmed: false,
  evidence_note: '',
})
watch(
  () => form.account_id,
  (id) => {
    const account = data.value?.accounts.find((value) => value.id === id)
    if (account) form.source_scope = account.provider === 'csv' ? 'cost-report' : 'native-cost'
  },
)
const canEdit = computed(() => workspace.value?.role !== 'viewer')
async function add() {
  busy.value = true
  actionError.value = ''
  try {
    await scoped('/invoices', { method: 'POST', body: { ...form } })
    open.value = false
    await refresh()
  } catch (e) {
    actionError.value = errorText(e)
  } finally {
    busy.value = false
  }
}
async function reconcile(id: string) {
  busy.value = true
  actionError.value = ''
  try {
    await scoped('/reconciliation-runs', { method: 'POST', body: { invoice_id: id } })
    await navigateTo(`/w/${workspace.value?.slug}/reconciliation`)
  } catch (e) {
    actionError.value = errorText(e)
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <div class="stack">
    <PageHeading
      :title="selected?.reference ?? t('nav.invoices')"
      :description="t('reconciliation.notPayment')"
    >
      <button v-if="canEdit && !id" class="primary" @click="open = !open">
        {{ t('invoices.add') }}
      </button>
    </PageHeading>
    <div v-if="error || actionError" class="notice error" role="alert">
      {{ actionError || errorText(error) }}
    </div>
    <form v-if="open" class="panel stack" @submit.prevent="add">
      <div class="grid-2">
        <label>
          {{ t('connections.account') }}
          <select v-model="form.account_id" required>
            <option disabled value="">{{ t('connections.account') }}</option>
            <option v-for="a in data?.accounts" :key="a.id" :value="a.id">{{ a.name }}</option>
          </select>
        </label>
        <label>
          {{ t('invoices.reference') }}
          <input v-model="form.reference" required maxlength="200" />
        </label>
        <label>
          {{ t('common.start') }}
          <input v-model="form.period_start" type="date" required />
        </label>
        <label>
          {{ t('common.end') }}
          <input v-model="form.period_end" type="date" required />
        </label>
        <label>
          {{ t('common.amount') }}
          <input v-model="form.amount" inputmode="decimal" required />
        </label>
        <label>
          {{ t('common.currency') }}
          <input v-model="form.currency" pattern="[A-Z]{3}" required />
        </label>
        <label>
          {{ t('invoices.adjustment') }}
          <input v-model="form.adjustment" inputmode="decimal" required />
          <small class="muted">{{ t('invoices.adjustmentHelp') }}</small>
        </label>
        <label>
          {{ t('imports.scope') }}
          <input v-model="form.source_scope" required maxlength="120" />
        </label>
      </div>
      <label>
        {{ t('invoices.evidence') }}
        <textarea v-model="form.evidence_note" required maxlength="4000" />
      </label>
      <label class="check">
        <input v-model="form.coverage_confirmed" type="checkbox" />
        {{ t('invoices.confirm') }}
      </label>
      <div class="row">
        <button class="primary" :disabled="busy">{{ t('common.create') }}</button>
        <button type="button" @click="open = false">{{ t('common.cancel') }}</button>
      </div>
    </form>
    <article v-if="selected" class="panel stack">
      <span class="badge">{{ t('invoices.manual') }}</span>
      <h2>{{ money(selected.amount, selected.currency) }}</h2>
      <p>{{ date(selected.period_start) }} — {{ date(selected.period_end) }}</p>
      <p>{{ t('invoices.adjustment') }}: {{ money(selected.adjustment, selected.currency) }}</p>
      <p>{{ selected.evidence_note }}</p>
      <p class="muted">{{ selected.source_scope }}</p>
      <div>
        <button v-if="canEdit" class="primary" :disabled="busy" @click="reconcile(selected.id)">
          {{ t('reconciliation.run') }}
        </button>
      </div>
    </article>
    <div v-else-if="data?.invoices.length" class="panel table-scroll">
      <table>
        <thead>
          <tr>
            <th>{{ t('invoices.reference') }}</th>
            <th>{{ t('common.period') }}</th>
            <th class="amount">{{ t('common.amount') }}</th>
            <th>{{ t('common.source') }}</th>
            <th />
          </tr>
        </thead>
        <tbody>
          <tr v-for="i in data.invoices" :key="i.id">
            <td>
              <NuxtLink class="link" :to="`/w/${workspace?.slug}/invoices/${i.id}`">
                {{ i.reference }}
              </NuxtLink>
            </td>
            <td>{{ date(i.period_start) }} — {{ date(i.period_end) }}</td>
            <td class="amount">{{ money(i.amount, i.currency) }}</td>
            <td>{{ i.source_scope }}</td>
            <td>
              <button v-if="canEdit" :disabled="busy" @click="reconcile(i.id)">
                {{ t('reconciliation.run') }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-else-if="!error" class="panel empty">
      <h2>{{ t('invoices.empty') }}</h2>
    </div>
  </div>
</template>
