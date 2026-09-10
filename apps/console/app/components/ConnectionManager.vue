<script setup lang="ts">
const props = defineProps<{ id?: string }>()
const { t } = useI18n()
const { api, scoped, workspace, errorText } = useApi()
const { date } = useMoney()
const { data, error, refresh, status } = await useAsyncData(
  () => `connections-${workspace.value?.id}-${props.id ?? 'all'}`,
  async (_app, { signal }) => {
    const [accounts, providers] = await Promise.all([
      scoped<any[]>('/connections', { signal }),
      api<any[]>('/providers', { signal }),
    ])
    return { accounts, providers }
  },
)
const selected = computed(() => data.value?.accounts.find((a) => a.id === props.id))
const adding = ref(false),
  replacing = ref(false),
  showImport = ref(false),
  busy = ref(false),
  message = ref(''),
  actionError = ref('')
const form = reactive({ provider: 'openai', name: '', account_ref: '', credential: '' })
const canEdit = computed(() => workspace.value?.role !== 'viewer')
async function action(fn: () => Promise<any>, notice = 'common.saved') {
  busy.value = true
  actionError.value = ''
  try {
    await fn()
    form.credential = ''
    message.value = t(notice)
    await refresh()
  } catch (e) {
    actionError.value = errorText(e)
  } finally {
    busy.value = false
  }
}
async function create() {
  await action(async () => {
    const a = await scoped('/connections', { method: 'POST', body: { ...form } })
    adding.value = false
    await navigateTo(`/w/${workspace.value?.slug}/connections/${a.id}`)
  })
}
function cancelAdding() {
  adding.value = false
  form.credential = ''
}
</script>
<template>
  <div class="stack">
    <PageHeading :title="selected?.name ?? t('nav.connections')" :description="t('site.readOnly')">
      <button v-if="canEdit && !id" class="primary" @click="adding = !adding">
        {{ t('connections.add') }}
      </button>
      <button v-if="canEdit && data?.accounts.length" @click="showImport = !showImport">
        {{ t('imports.title') }}
      </button>
    </PageHeading>
    <div v-if="error" class="notice error" role="alert">{{ errorText(error) }}</div>
    <div v-if="actionError" class="notice error" role="alert">{{ actionError }}</div>
    <div v-if="message" class="notice" role="status">{{ message }}</div>
    <form
      v-if="canEdit && (adding || (!data?.accounts.length && status !== 'pending'))"
      class="panel stack"
      @submit.prevent="create"
    >
      <h2>{{ t('connections.add') }}</h2>
      <div class="grid-2">
        <label>
          {{ t('common.provider') }}
          <select v-model="form.provider">
            <option v-for="p in data?.providers" :key="p.id" :value="p.id">{{ p.name }}</option>
          </select>
        </label>
        <label>
          {{ t('common.name') }}
          <input v-model="form.name" required maxlength="100" />
        </label>
        <label>
          {{ t('connections.account') }}
          <input v-model="form.account_ref" required maxlength="160" />
          <small class="muted">{{ t('connections.accountHelp') }}</small>
        </label>
        <label v-if="form.provider !== 'csv'">
          {{ t('connections.credential') }}
          <input
            v-model="form.credential"
            type="password"
            autocomplete="off"
            required
            maxlength="4096"
          />
          <small class="muted">{{ t('connections.credentialHelp') }}</small>
        </label>
      </div>
      <p class="notice warning">{{ t(`providers.${form.provider}Warning`) }}</p>
      <div class="row">
        <button class="primary" :disabled="busy">{{ t('connections.add') }}</button>
        <button type="button" @click="cancelAdding">
          {{ t('common.cancel') }}
        </button>
      </div>
    </form>
    <template v-if="selected">
      <article class="panel stack">
        <div class="row between">
          <h2>{{ selected.provider }}</h2>
          <StatusBadge :value="selected.status" />
        </div>
        <p class="notice warning">{{ t(`providers.${selected.provider}Warning`) }}</p>
        <p>{{ t('connections.account') }}: {{ selected.external_account_ref }}</p>
        <p class="muted">{{ t('connections.through') }}: {{ date(selected.data_through) }} · UTC</p>
        <p v-if="selected.credential_suffix" class="muted">
          {{ t('connections.credential') }}: •••• {{ selected.credential_suffix }}
        </p>
        <div class="row">
          <button
            v-if="canEdit && selected.provider !== 'csv' && selected.status !== 'disconnected'"
            :disabled="busy"
            @click="
              action(
                () => scoped(`/connections/${id}/sync`, { method: 'POST' }),
                'connections.syncQueued',
              )
            "
          >
            {{ t('connections.sync') }}
          </button>
          <button
            v-if="canEdit && selected.provider !== 'csv' && selected.status !== 'disconnected'"
            @click="replacing = !replacing"
          >
            {{ t('connections.replace') }}
          </button>
          <button @click="refresh()">{{ t('common.refresh') }}</button>
        </div>
        <form
          v-if="replacing"
          class="stack"
          @submit.prevent="
            action(async () => {
              await scoped(`/connections/${id}/credentials`, {
                method: 'POST',
                body: { credential: form.credential },
              })
              replacing = false
            })
          "
        >
          <label>
            {{ t('connections.credential') }}
            <input v-model="form.credential" type="password" autocomplete="off" required />
          </label>
          <button :disabled="busy">{{ t('common.save') }}</button>
        </form>
        <details v-if="canEdit && selected.status !== 'disconnected'">
          <summary class="danger">{{ t('connections.disconnect') }}</summary>
          <p class="muted">{{ t('connections.disconnectHelp') }}</p>
          <button
            class="danger"
            :disabled="busy"
            @click="action(() => scoped(`/connections/${id}`, { method: 'DELETE' }))"
          >
            {{ t('connections.disconnect') }}
          </button>
        </details>
      </article>
    </template>
    <div v-else-if="data?.accounts.length" class="grid-3">
      <NuxtLink
        v-for="a in data.accounts"
        :key="a.id"
        class="panel stack connection-card"
        :to="`/w/${workspace?.slug}/connections/${a.id}`"
      >
        <div class="row between">
          <h2>{{ a.name }}</h2>
          <StatusBadge :value="a.status" />
        </div>
        <p class="muted">{{ a.provider }}</p>
        <small>{{ t('connections.through') }}: {{ date(a.data_through) }}</small>
        <span class="link">{{ t('connections.view') }}</span>
      </NuxtLink>
    </div>
    <ImportReport
      v-if="canEdit && showImport && data?.accounts.length"
      :accounts="data.accounts"
      :account-id="id"
      @committed="refresh()"
    />
  </div>
</template>
<style scoped>
details {
  padding-top: 1rem;
  border-top: 1px solid var(--line);
}
summary {
  cursor: pointer;
  font-size: 0.875rem;
}
details p {
  margin: 0.7rem 0;
}
.connection-card:hover {
  border-color: var(--accent);
}
</style>
