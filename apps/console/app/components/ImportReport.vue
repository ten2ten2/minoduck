<script setup lang="ts">
const props = defineProps<{ accounts: any[]; accountId?: string }>()
const emit = defineEmits<{ committed: [] }>()
const { t } = useI18n()
const { scoped, errorText } = useApi()
const { money } = useMoney()
const codeError = (code: string) => errorText({ data: { error: { code } } })
const account = ref(props.accountId ?? ''),
  scope = ref('cost-report'),
  timezone = ref('UTC'),
  kind = ref('actual'),
  granularity = ref('aggregate')
const file = ref<File>(),
  busy = ref(false),
  error = ref(''),
  message = ref(''),
  preview = ref<any>(),
  confirmCorrections = ref(false)
watch(
  () => props.accountId,
  (id) => {
    if (id) account.value = id
  },
)
const choose = (event: Event) => {
  file.value = (event.target as HTMLInputElement).files?.[0]
  preview.value = null
}
async function validate() {
  if (!file.value) return
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    const body = new FormData()
    body.append('file', file.value)
    body.append('account_id', account.value)
    body.append('source_scope', scope.value)
    body.append('timezone', timezone.value)
    body.append('cost_kind', kind.value)
    body.append('granularity', granularity.value)
    preview.value = await scoped('/imports', { method: 'POST', body })
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
async function commit() {
  busy.value = true
  error.value = ''
  try {
    await scoped(`/imports/${preview.value.id}/commit`, {
      method: 'POST',
      body: { confirm_corrections: confirmCorrections.value },
    })
    message.value = t('imports.committed')
    preview.value = null
    emit('committed')
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <section class="panel stack">
    <div class="panel-title">
      <h2>{{ t('imports.title') }}</h2>
      <a class="link" href="/templates/cost-report.csv" download>{{ t('imports.template') }}</a>
    </div>
    <p class="muted">{{ t('imports.limit') }}</p>
    <div v-if="error" class="notice error" role="alert">{{ error }}</div>
    <div v-if="message" class="notice" role="status">{{ message }}</div>
    <form v-if="!preview" class="stack" @submit.prevent="validate">
      <div class="grid-2">
        <label>
          {{ t('connections.account') }}
          <select v-model="account" required>
            <option disabled value="">{{ t('common.all') }}</option>
            <option
              v-for="a in accounts.filter((v) => v.status !== 'disconnected')"
              :key="a.id"
              :value="a.id"
            >
              {{ a.name }} · {{ a.provider }}
            </option>
          </select>
        </label>
        <label>
          {{ t('imports.scope') }}
          <input v-model="scope" required maxlength="120" />
          <small class="muted">{{ t('imports.scopeHelp') }}</small>
        </label>
        <label>
          {{ t('imports.timezone') }}
          <input v-model="timezone" required />
        </label>
        <label>
          {{ t('costs.basis') }}
          <select v-model="kind">
            <option
              v-for="k in ['actual', 'billed', 'calculated', 'estimated']"
              :key="k"
              :value="k"
            >
              {{ t(`costKind.${k}`) }}
            </option>
          </select>
        </label>
        <label>
          {{ t('imports.granularity') }}
          <select v-model="granularity">
            <option value="aggregate">{{ t('imports.aggregate') }}</option>
            <option value="event">{{ t('imports.event') }}</option>
          </select>
        </label>
        <label>
          {{ t('imports.file') }}
          <input type="file" accept=".csv,text/csv" required @change="choose" />
        </label>
      </div>
      <div>
        <button class="primary" :disabled="busy || !file">{{ t('imports.preview') }}</button>
      </div>
    </form>
    <template v-else>
      <h3>
        {{
          t('imports.summary', { count: preview.preview.count, rejected: preview.preview.rejected })
        }}
      </h3>
      <div class="row">
        <span
          v-for="(amount, currency) in preview.preview.totals"
          :key="currency"
          class="badge num"
        >
          {{ money(String(amount), String(currency)) }}
        </span>
      </div>
      <div v-if="preview.preview.rejected" class="notice error">
        <p>{{ t('imports.errors') }}</p>
        <ul>
          <li v-for="row in preview.preview.errors" :key="row.row">
            {{ row.row }} · {{ codeError(row.code) }}
          </li>
        </ul>
      </div>
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>{{ t('common.start') }}</th>
              <th>{{ t('common.model') }}</th>
              <th class="amount">{{ t('common.amount') }}</th>
              <th>{{ t('common.status') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(entry, index) in preview.preview.rows" :key="index">
              <td>{{ entry.period_start.slice(0, 10) }}</td>
              <td>{{ entry.model || t('costs.unknownModel') }}</td>
              <td class="amount">{{ money(entry.amount, entry.currency) }}</td>
              <td><StatusBadge :value="entry.coverage" /></td>
            </tr>
          </tbody>
        </table>
      </div>
      <label class="check">
        <input v-model="confirmCorrections" type="checkbox" />
        {{ t('imports.corrections') }}
      </label>
      <div class="row">
        <button class="primary" :disabled="busy || preview.preview.rejected > 0" @click="commit">
          {{ t('imports.commit') }}
        </button>
        <button :disabled="busy" @click="preview = null">{{ t('common.cancel') }}</button>
      </div>
    </template>
  </section>
</template>
