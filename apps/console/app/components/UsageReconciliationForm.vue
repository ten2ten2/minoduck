<script setup lang="ts">
const emit = defineEmits<{ created: [] }>()
const { t } = useI18n()
const { scoped, errorText } = useApi()
const accounts = ref<Connection[]>([]),
  busy = ref(false),
  error = ref(''),
  message = ref('')
const form = reactive({
  account_id: '',
  period_start: '',
  period_end: '',
  currency: 'USD',
  source_scope: 'cost-report',
  coverage_confirmed: false,
})
async function load() {
  try {
    accounts.value = await scoped<Connection[]>('/connections')
  } catch (e) {
    error.value = errorText(e)
  }
}
async function run() {
  busy.value = true
  error.value = ''
  try {
    await scoped('/usage-reconciliation-runs', { method: 'POST', body: { ...form } })
    message.value = t('common.saved')
    emit('created')
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <details
    class="panel"
    @toggle="
      (event) => {
        if ((event.target as HTMLDetailsElement).open) load()
      }
    "
  >
    <summary>{{ t('reconciliation.usage') }}</summary>
    <form class="stack usage-form" @submit.prevent="run">
      <p class="muted">{{ t('reconciliation.usageHelp') }}</p>
      <p v-if="error" class="notice error" role="alert">{{ error }}</p>
      <p v-if="message" class="notice">{{ message }}</p>
      <div class="grid-2">
        <label>
          {{ t('connections.account') }}
          <select v-model="form.account_id" required>
            <option disabled value="" />
            <option
              v-for="a in accounts.filter((v) => v.provider !== 'csv')"
              :key="a.id"
              :value="a.id"
            >
              {{ a.name }}
            </option>
          </select>
        </label>
        <label>
          {{ t('common.currency') }}
          <input v-model="form.currency" pattern="[A-Z]{3}" required />
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
          {{ t('imports.scope') }}
          <input v-model="form.source_scope" required maxlength="120" />
        </label>
      </div>
      <label class="check">
        <input v-model="form.coverage_confirmed" type="checkbox" />
        {{ t('invoices.confirm') }}
      </label>
      <div>
        <button class="primary" :disabled="busy">{{ t('reconciliation.run') }}</button>
      </div>
    </form>
  </details>
</template>
<style scoped>
summary {
  cursor: pointer;
  font-weight: 600;
}
.usage-form {
  margin-top: 1.2rem;
}
</style>
