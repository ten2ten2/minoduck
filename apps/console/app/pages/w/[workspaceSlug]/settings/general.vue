<script setup lang="ts">
const { t } = useI18n()
const { scoped, workspace, errorText, loadUser } = useApi()
const name = ref(workspace.value?.name ?? ''),
  confirmId = ref(''),
  busy = ref(false),
  error = ref(''),
  message = ref('')
async function save() {
  busy.value = true
  try {
    await scoped('', { method: 'PATCH', body: { name: name.value } })
    await loadUser()
    message.value = t('common.saved')
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
async function remove() {
  busy.value = true
  try {
    await scoped('/deletion-requests', { method: 'POST', body: { confirm: confirmId.value } })
    await loadUser()
    await navigateTo('/onboarding')
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <div class="stack">
    <PageHeading :title="t('nav.general')" />
    <div v-if="error" class="notice error" role="alert">{{ error }}</div>
    <div v-if="message" class="notice">{{ message }}</div>
    <form class="panel stack" @submit.prevent="save">
      <label>
        {{ t('common.name') }}
        <input v-model="name" required maxlength="100" :disabled="workspace?.role === 'viewer'" />
      </label>
      <div v-if="workspace?.role !== 'viewer'">
        <button class="primary" :disabled="busy">{{ t('common.save') }}</button>
      </div>
    </form>
    <details v-if="workspace?.role === 'owner'" class="panel">
      <summary class="danger">{{ t('settings.deleteWorkspace') }}</summary>
      <form class="stack deletion" @submit.prevent="remove">
        <p>{{ t('settings.deleteHelp') }}</p>
        <code>{{ workspace?.id }}</code>
        <label>
          {{ t('settings.typeId') }}
          <input v-model="confirmId" required />
        </label>
        <div>
          <button class="danger" :disabled="busy || confirmId !== workspace?.id">
            {{ t('settings.deleteWorkspace') }}
          </button>
        </div>
      </form>
    </details>
  </div>
</template>
<style scoped>
.deletion {
  margin-top: 1rem;
}
summary {
  cursor: pointer;
}
</style>
