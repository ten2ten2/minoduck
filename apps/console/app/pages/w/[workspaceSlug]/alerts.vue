<script setup lang="ts">
const { t } = useI18n()
const { scoped, workspace, errorText } = useApi()
const { money } = useMoney()
const { data, error, refresh } = await useAsyncData(
  () => `alerts-${workspace.value?.id}`,
  (_app, { signal }) => scoped<AlertRule[]>('/alert-rules', { signal }),
)
const open = ref(false),
  busy = ref(false),
  actionError = ref(''),
  editing = ref('')
const form = reactive({ name: '', kind: 'budget', currency: 'USD', amount: '', enabled: true })
function resetForm() {
  Object.assign(form, { name: '', kind: 'budget', currency: 'USD', amount: '', enabled: true })
}
function edit(rule: AlertRule) {
  editing.value = rule.id
  Object.assign(form, {
    name: rule.name,
    kind: rule.kind,
    currency: rule.currency ?? 'USD',
    amount: rule.amount ?? '',
    enabled: rule.enabled,
  })
  open.value = true
}
async function save() {
  busy.value = true
  actionError.value = ''
  try {
    const body = { ...form }
    if (body.kind === 'sync_failure') {
      body.currency = ''
      body.amount = ''
    }
    await scoped('/alert-rules' + (editing.value ? `/${editing.value}` : ''), {
      method: editing.value ? 'PATCH' : 'POST',
      body,
    })
    open.value = false
    editing.value = ''
    resetForm()
    await refresh()
  } catch (e) {
    actionError.value = errorText(e)
  } finally {
    busy.value = false
  }
}
async function remove(id: string) {
  busy.value = true
  actionError.value = ''
  try {
    await scoped(`/alert-rules/${id}`, { method: 'DELETE' })
    await refresh()
  } catch (e) {
    actionError.value = errorText(e)
  } finally {
    busy.value = false
  }
}
function startAdding() {
  editing.value = ''
  resetForm()
  open.value = true
}
function closeForm() {
  open.value = false
  editing.value = ''
  resetForm()
}
</script>
<template>
  <div class="stack">
    <PageHeading :title="t('nav.alerts')" :description="t('alerts.subtitle')">
      <button v-if="workspace?.role !== 'viewer'" class="primary" @click="startAdding">
        {{ t('alerts.add') }}
      </button>
    </PageHeading>
    <div v-if="error || actionError" class="notice error" role="alert">
      {{ actionError || errorText(error) }}
    </div>
    <form v-if="open" class="panel stack" @submit.prevent="save">
      <div class="grid-2">
        <label>
          {{ t('common.name') }}
          <input v-model="form.name" required maxlength="100" />
        </label>
        <label>
          {{ t('alerts.kind') }}
          <select v-model="form.kind">
            <option v-for="kind in ['budget', 'spike', 'sync_failure']" :key="kind" :value="kind">
              {{ t(`alerts.${kind}`) }}
            </option>
          </select>
        </label>
        <label v-if="form.kind !== 'sync_failure'">
          {{ t('common.amount') }}
          <input v-model="form.amount" inputmode="decimal" required />
        </label>
        <label v-if="form.kind !== 'sync_failure'">
          {{ t('common.currency') }}
          <input v-model="form.currency" pattern="[A-Z]{3}" required />
        </label>
      </div>
      <p v-if="form.kind !== 'sync_failure'" class="muted">{{ t('alerts.amountHelp') }}</p>
      <label class="check">
        <input v-model="form.enabled" type="checkbox" />
        {{ t('common.enabled') }}
      </label>
      <div class="row">
        <button class="primary" :disabled="busy">{{ t('common.save') }}</button>
        <button type="button" @click="closeForm">{{ t('common.cancel') }}</button>
      </div>
    </form>
    <div v-if="data?.length" class="grid-2">
      <article v-for="rule in data" :key="rule.id" class="panel stack">
        <div class="row between">
          <h2>{{ rule.name }}</h2>
          <span class="badge">{{ t(`alerts.${rule.kind}`) }}</span>
        </div>
        <strong v-if="rule.kind !== 'sync_failure'" class="num">
          {{ money(rule.amount, rule.currency) }}
        </strong>
        <div v-if="workspace?.role !== 'viewer'" class="row">
          <button @click="edit(rule)">{{ t('common.edit') }}</button>
          <details>
            <summary class="danger">{{ t('common.delete') }}</summary>
            <button class="danger" :disabled="busy" @click="remove(rule.id)">
              {{ t('common.delete') }} · {{ rule.name }}
            </button>
          </details>
        </div>
      </article>
    </div>
    <div v-else-if="!error" class="panel empty">
      <h2>{{ t('alerts.empty') }}</h2>
    </div>
  </div>
</template>
