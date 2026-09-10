<script setup lang="ts">
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const { scoped, errorText } = useApi()
const busy = ref(false),
  error = ref(''),
  message = ref(''),
  loaded = ref(false)
const prices = ref<any[]>([]),
  accounts = ref<any[]>([]),
  adding = ref(false)
const form = reactive({
  billing_provider: 'openai',
  model_version: '',
  currency: 'USD',
  effective_from: '',
  effective_to: '',
  service_tier: '',
  region: '',
  route: '',
  input_per_million: '',
  output_per_million: '',
  cache_read_per_million: '0',
  cache_write_5m_per_million: '0',
  cache_write_1h_per_million: '0',
  price_basis: 'public',
  evidence_url: '',
  assumptions: '',
})
const comparison = reactive({
  account_id: '',
  baseline_price_id: '',
  candidate_price_id: '',
  period_start: '',
  period_end: '',
})
const fields = [
  { key: 'input_per_million', label: 'input' },
  { key: 'output_per_million', label: 'output' },
  { key: 'cache_read_per_million', label: 'read' },
  { key: 'cache_write_5m_per_million', label: 'write5' },
  { key: 'cache_write_1h_per_million', label: 'write1' },
] as const
async function load() {
  try {
    ;[prices.value, accounts.value] = await Promise.all([
      scoped<any[]>('/prices'),
      scoped<any[]>('/connections'),
    ])
    loaded.value = true
  } catch (e) {
    error.value = errorText(e)
  }
}
async function save() {
  busy.value = true
  error.value = ''
  try {
    await scoped('/prices', { method: 'POST', body: { ...form } })
    adding.value = false
    await load()
    message.value = t('common.saved')
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
async function compare() {
  busy.value = true
  error.value = ''
  try {
    const out = await scoped('/price-comparisons', { method: 'POST', body: { ...comparison } })
    message.value = t(out.status === 'no_savings' ? 'prices.noSavings' : 'common.saved')
    emit('updated')
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <details
    class="panel price-tools"
    @toggle="
      (event) => {
        if ((event.target as HTMLDetailsElement).open && !loaded) load()
      }
    "
  >
    <summary>{{ t('prices.title') }}</summary>
    <div class="stack content">
      <p class="muted">{{ t('prices.help') }}</p>
      <p v-if="error" class="notice error" role="alert">{{ error }}</p>
      <p v-if="message" class="notice">{{ message }}</p>
      <button @click="adding = !adding">{{ t('prices.add') }}</button>
      <form v-if="adding" class="stack" @submit.prevent="save">
        <div class="grid-2">
          <label>
            {{ t('common.provider') }}
            <input v-model="form.billing_provider" required />
          </label>
          <label>
            {{ t('prices.model') }}
            <input v-model="form.model_version" required />
          </label>
          <label>
            {{ t('common.currency') }}
            <input v-model="form.currency" pattern="[A-Z]{3}" required />
          </label>
          <label>
            {{ t('prices.basis') }}
            <select v-model="form.price_basis">
              <option value="public">{{ t('prices.public') }}</option>
              <option value="contract">{{ t('prices.contract') }}</option>
            </select>
          </label>
          <label>
            {{ t('common.start') }}
            <input v-model="form.effective_from" type="date" required />
          </label>
          <label>
            {{ t('common.end') }}
            <input v-model="form.effective_to" type="date" required />
          </label>
          <label>
            {{ t('prices.tier') }}
            <input v-model="form.service_tier" />
          </label>
          <label>
            {{ t('prices.region') }}
            <input v-model="form.region" />
          </label>
          <label>
            {{ t('prices.route') }}
            <input v-model="form.route" />
          </label>
          <label v-for="field in fields" :key="field.key">
            {{ t(`prices.${field.label}`) }}
            <input v-model="form[field.key]" inputmode="decimal" required />
          </label>
        </div>
        <label>
          {{ t('prices.url') }}
          <input v-model="form.evidence_url" type="url" required />
        </label>
        <label>
          {{ t('prices.assumptions') }}
          <textarea v-model="form.assumptions" required />
        </label>
        <div>
          <button class="primary" :disabled="busy">{{ t('common.save') }}</button>
        </div>
      </form>
      <form v-if="prices.length > 1" class="stack" @submit.prevent="compare">
        <h2>{{ t('prices.compare') }}</h2>
        <div class="grid-2">
          <label>
            {{ t('connections.account') }}
            <select v-model="comparison.account_id" required>
              <option disabled value="" />
              <option v-for="a in accounts" :key="a.id" :value="a.id">{{ a.name }}</option>
            </select>
          </label>
          <label>
            {{ t('common.start') }}
            <input v-model="comparison.period_start" type="date" required />
          </label>
          <label>
            {{ t('common.end') }}
            <input v-model="comparison.period_end" type="date" required />
          </label>
          <label v-for="key in ['baseline_price_id', 'candidate_price_id'] as const" :key="key">
            {{ t(key === 'baseline_price_id' ? 'prices.baseline' : 'prices.candidate') }}
            <select v-model="comparison[key]" required>
              <option disabled value="" />
              <option v-for="price in prices" :key="price.id" :value="price.id">
                {{ price.billing_provider }} · {{ price.model_version }} ·
                {{ price.effective_from.slice(0, 10) }} · {{ price.route }}
              </option>
            </select>
          </label>
        </div>
        <div>
          <button :disabled="busy">{{ t('prices.compare') }}</button>
        </div>
      </form>
    </div>
  </details>
</template>
<style scoped>
summary {
  cursor: pointer;
  font-weight: 600;
}
.content {
  margin-top: 1.2rem;
}
</style>
