<script setup lang="ts">
const { t } = useI18n()
const { destination } = useBillingIntent()
const { api, workspaces, loadUser, errorText } = useApi()
const { preferenceError, savePreferences } = useAccountPreferences()
const name = ref(''),
  slug = ref(''),
  busy = ref(false),
  error = ref('')
async function create() {
  busy.value = true
  error.value = ''
  try {
    const w = await api<Workspace>('/workspaces', {
      method: 'POST',
      body: { name: name.value, slug: slug.value },
    })
    await loadUser()
    await navigateTo(destination(w.slug, 'connections'))
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <div class="onboarding">
    <header class="row between">
      <BrandWordmark />
      <PreferencesControl @change="savePreferences" />
    </header>
    <main class="stack">
      <h1>{{ t('onboarding.title') }}</h1>
      <p class="muted">{{ t('onboarding.subtitle') }}</p>
      <div v-if="preferenceError" class="notice error" role="alert">{{ preferenceError }}</div>
      <div v-if="workspaces.length" class="panel stack">
        <h2>{{ t('onboarding.existing') }}</h2>
        <NuxtLink v-for="w in workspaces" :key="w.id" :to="destination(w.slug)" class="button">
          {{ w.name }}
        </NuxtLink>
      </div>
      <form class="panel stack" @submit.prevent="create">
        <div v-if="error" class="notice error" role="alert">{{ error }}</div>
        <label>
          {{ t('common.name') }}
          <input v-model="name" required maxlength="100" />
        </label>
        <label>
          {{ t('onboarding.slug') }}
          <input v-model="slug" required pattern="[a-z0-9][a-z0-9-]{1,47}" />
          <small class="muted">{{ t('onboarding.slugHelp') }}</small>
        </label>
        <button class="primary" :disabled="busy">{{ t('onboarding.create') }}</button>
      </form>
    </main>
  </div>
</template>
<style scoped>
.onboarding {
  max-width: 1000px;
  margin: auto;
  padding: 2rem;
}
main {
  max-width: 520px;
  margin: 4rem auto;
}
</style>
