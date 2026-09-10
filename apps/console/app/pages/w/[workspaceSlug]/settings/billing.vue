<script setup lang="ts">
const { t } = useI18n()
const { scoped, workspace, errorText } = useApi()
const { money, date } = useMoney()
const route = useRoute()
const { data, error, refresh } = await useAsyncData(
  () => `billing-${workspace.value?.id}`,
  (_app, { signal }) => scoped('/subscription', { signal }),
)
const busy = ref(false),
  actionError = ref(''),
  notice = ref(route.query.checkout === 'success' ? 'billing.pending' : ''),
  selection = ref<{ plan: string; interval: 'month' | 'year' }>()
const { intent } = useBillingIntent()
if (intent.value) {
  selection.value = { ...intent.value }
  intent.value = null
}
const canManage = computed(() => workspace.value?.role === 'owner')
async function choose(plan: string, interval: 'month' | 'year') {
  if (plan === 'free') return
  if (!data.value?.subscription.has_subscription) {
    await checkout(plan, interval)
  } else {
    selection.value = { plan, interval }
  }
}
async function checkout(plan: string, interval: string) {
  busy.value = true
  actionError.value = ''
  try {
    const out = await scoped('/subscription/checkout', {
      method: 'POST',
      body: { plan, billing_interval: interval },
    })
    await navigateTo(out.url, { external: true })
  } catch (e) {
    actionError.value = errorText(e)
  } finally {
    busy.value = false
  }
}
async function change() {
  if (!selection.value) return
  if (!data.value?.subscription.has_subscription) {
    await checkout(selection.value.plan, selection.value.interval)
    return
  }
  busy.value = true
  try {
    const out = await scoped('/subscription/change', {
      method: 'POST',
      body: { plan: selection.value.plan, billing_interval: selection.value.interval },
    })
    notice.value = out.status === 'scheduled' ? 'billing.scheduled' : 'billing.pending'
    selection.value = undefined
    await refresh()
  } catch (e) {
    actionError.value = errorText(e)
  } finally {
    busy.value = false
  }
}
async function portal() {
  busy.value = true
  try {
    const out = await scoped('/subscription/portal', { method: 'POST' })
    await navigateTo(out.url, { external: true })
  } catch (e) {
    actionError.value = errorText(e)
  } finally {
    busy.value = false
  }
}
async function cancel() {
  busy.value = true
  try {
    await scoped('/subscription/cancel', { method: 'POST' })
    notice.value = 'billing.pending'
    await refresh()
  } catch (e) {
    actionError.value = errorText(e)
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <div class="stack">
    <PageHeading :title="t('nav.billing')" :description="t('billing.title')">
      <button @click="refresh()">{{ t('common.refresh') }}</button>
    </PageHeading>
    <div v-if="error || actionError" class="notice error" role="alert">
      {{ actionError || errorText(error) }}
    </div>
    <div v-if="notice" class="notice" role="status">{{ t(notice) }}</div>
    <template v-if="data">
      <div v-if="!data.stripe_configured" class="notice warning">
        {{ t('billing.notConfigured') }}
      </div>
      <div v-if="data.subscription.status === 'past_due'" class="notice warning">
        {{ t('billing.grace') }}
      </div>
      <div v-if="data.spend_level !== 'normal'" class="notice warning">
        {{ t(`billing.${data.spend_level}`) }}
      </div>
      <div v-if="data.non_usd_requires_review" class="notice warning">
        {{ t('billing.mixedCurrency') }}
      </div>
      <article class="panel stack">
        <div class="row between">
          <h2>{{ t('billing.current') }} · {{ t(`plans.${data.entitlements.code}`) }}</h2>
          <StatusBadge :value="data.subscription.status" />
        </div>
        <p v-if="data.subscription.current_period_end" class="muted">
          {{ t('billing.renews') }}: {{ date(data.subscription.current_period_end) }}
        </p>
        <div class="row">
          <span v-for="(amount, currency) in data.managed_spend" :key="currency" class="badge num">
            {{ money(String(amount), String(currency)) }}
          </span>
          <span class="muted">/ USD {{ data.entitlements.spend_limit }}</span>
        </div>
        <p v-if="data.subscription.scheduled_plan" class="notice">
          {{ t('billing.scheduled') }} · {{ data.subscription.scheduled_plan }}
        </p>
        <div v-if="canManage" class="row">
          <button :disabled="busy || !data.stripe_configured" @click="portal">
            {{ t('billing.portal') }}
          </button>
          <details v-if="data.entitlements.code !== 'free'">
            <summary class="danger">{{ t('billing.cancel') }}</summary>
            <p class="muted">{{ t('billing.cancelHelp') }}</p>
            <button class="danger" :disabled="busy" @click="cancel">
              {{ t('billing.cancel') }}
            </button>
          </details>
        </div>
      </article>
      <article v-if="selection" class="panel stack">
        <h2>{{ t('billing.changeConfirm') }} · {{ selection.plan }}</h2>
        <p>{{ t('billing.immediate') }}</p>
        <p>{{ t('billing.deferred') }}</p>
        <div class="row">
          <button class="primary" :disabled="busy" @click="change">{{ t('common.save') }}</button>
          <button @click="selection = undefined">{{ t('common.cancel') }}</button>
        </div>
      </article>
      <PricingCards
        :busy="busy || !canManage || !data.stripe_configured"
        :current="data.entitlements.code"
        @choose="choose"
      />
    </template>
  </div>
</template>
