<script setup lang="ts">
const { t, locale } = useI18n()
const { api, loadUser, errorText } = useApi()
const route = useRoute()
const { capture } = useBillingIntent()
capture(route.query)
const email = ref(''),
  error = ref(''),
  link = ref('')
const sent = ref(false),
  busy = ref(false)
const token = ref(String(route.query.token ?? ''))
if (import.meta.client && (token.value || route.query.ui_locale !== undefined)) {
  const query = { ...route.query }
  delete query.token
  delete query.ui_locale
  await navigateTo({ path: '/login', query }, { replace: true })
}
async function send() {
  busy.value = true
  error.value = ''
  try {
    const result = await api('/auth/email/start', {
      method: 'POST',
      body: { email: email.value, locale: locale.value },
    })
    sent.value = true
    link.value = result.development_link ?? ''
  } catch (e) {
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
async function verify() {
  busy.value = true
  error.value = ''
  try {
    await api('/auth/email/verify', { method: 'POST', body: { token: token.value } })
    await loadUser()
  } catch (e) {
    error.value = errorText(e)
    return
  } finally {
    busy.value = false
  }
  token.value = ''
  await navigateTo('/onboarding')
}
</script>
<template>
  <div class="login-page">
    <header>
      <BrandWordmark />
      <PreferencesControl />
    </header>
    <main class="login-card panel stack">
      <div class="eyebrow">{{ t('site.eyebrow') }}</div>
      <h1>{{ t('login.title') }}</h1>
      <p class="muted">{{ t('login.subtitle') }}</p>
      <div v-if="error" class="notice error" role="alert">{{ error }}</div>
      <template v-if="token">
        <button class="primary" :disabled="busy" @click="verify">
          {{ t(busy ? 'login.verifying' : 'login.verify') }}
        </button>
      </template>
      <template v-else-if="sent">
        <h2>{{ t('login.check') }}</h2>
        <p>{{ t('login.checkHelp') }}</p>
        <a v-if="link" class="button primary" :href="link">{{ t('login.development') }}</a>
        <button @click="sent = false">{{ t('common.previous') }}</button>
      </template>
      <form v-else class="stack" @submit.prevent="send">
        <label>
          {{ t('common.email') }}
          <input v-model="email" type="email" autocomplete="email" required maxlength="254" />
        </label>
        <button class="primary" :disabled="busy">{{ t('login.send') }}</button>
        <p class="muted or">{{ t('login.or') }}</p>
        <a class="button" href="/api/v1/auth/google/start">{{ t('login.google') }}</a>
      </form>
    </main>
  </div>
</template>
<style scoped>
.login-page {
  min-height: 100vh;
  background: radial-gradient(ellipse at 15% 40%, var(--accent-soft), transparent 55%);
}
header {
  padding: 1.8rem 3rem;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 1rem;
  flex-wrap: wrap;
}
.login-card {
  max-width: 440px;
  margin: 7vh auto;
  padding: 2.4rem;
  box-shadow: 0 15px 70px #153d7020;
}
.login-card h1 {
  font-size: 2rem;
}
.or {
  text-align: center;
  font-size: 0.875rem;
}
@media (max-width: 600px) {
  header {
    padding: 1rem;
  }
  .login-card {
    margin: 2rem 1rem;
    padding: 1.5rem;
  }
}
</style>
