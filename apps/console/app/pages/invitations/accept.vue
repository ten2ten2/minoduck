<script setup lang="ts">
const { t } = useI18n()
const { api, loadUser, errorText } = useApi()
const route = useRoute()
const token = ref(String(route.query.token ?? '')),
  error = ref(''),
  busy = ref(false)
if (import.meta.client && token.value) {
  const query = { ...route.query }
  delete query.token
  await navigateTo({ path: route.path, query }, { replace: true })
}
async function accept() {
  busy.value = true
  error.value = ''
  try {
    await loadUser()
    await api('/invitations/accept', { method: 'POST', body: { token: token.value } })
    token.value = ''
    await loadUser()
  } catch (e) {
    error.value = errorText(e)
    return
  } finally {
    busy.value = false
  }
  await navigateTo('/onboarding')
}
</script>
<template>
  <main class="invitation panel stack">
    <BrandWordmark />
    <h1>{{ t('settings.accept') }}</h1>
    <p>{{ t('settings.acceptHelp') }}</p>
    <p v-if="error" class="notice error">{{ error }}</p>
    <NuxtLink class="button" to="/login" target="_blank">{{ t('login.subtitle') }}</NuxtLink>
    <button class="primary" :disabled="busy || !token" @click="accept">
      {{ t('settings.accept') }}
    </button>
  </main>
</template>
<style scoped>
.invitation {
  max-width: 520px;
  margin: 10vh auto;
  padding: 2rem;
}
@media (max-width: 600px) {
  .invitation {
    margin: 2rem 1rem;
  }
}
</style>
