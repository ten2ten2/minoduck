<script setup lang="ts">
defineProps<{ busy?: boolean; current?: string; contactEmail?: string }>()
const emit = defineEmits<{ choose: [plan: string, interval: 'month' | 'year'] }>()
const { t } = useI18n()
const interval = ref<'month' | 'year'>('month')
const plans = [
  {
    code: 'free',
    monthly: 0,
    yearly: 0,
    spend: '500',
    connections: 2,
    history: '30',
    members: 1,
    workspaces: 1,
  },
  {
    code: 'starter',
    monthly: 29,
    yearly: 290,
    spend: '5,000',
    connections: 5,
    history: '180',
    members: 3,
    workspaces: 1,
  },
  {
    code: 'team',
    monthly: 79,
    yearly: 790,
    spend: '25,000',
    connections: 15,
    history: '730',
    members: 10,
    workspaces: 3,
  },
]
</script>
<template>
  <section class="stack">
    <div class="row between">
      <p class="muted">{{ t('pricing.predictable') }}</p>
      <div class="tabs">
        <button :aria-pressed="interval === 'month'" @click="interval = 'month'">
          {{ t('pricing.monthly') }}
        </button>
        <button :aria-pressed="interval === 'year'" @click="interval = 'year'">
          {{ t('pricing.yearly') }} · {{ t('pricing.saveTwo') }}
        </button>
      </div>
    </div>
    <div class="pricing-grid">
      <article
        v-for="plan in plans"
        :key="plan.code"
        class="panel price-card"
        :class="{ featured: plan.code === 'starter' }"
      >
        <div class="row between">
          <h2>{{ t(`plans.${plan.code}`) }}</h2>
          <span v-if="plan.code === 'starter'" class="badge">{{ t('pricing.forBuilders') }}</span>
        </div>
        <div class="price num">
          <small>USD</small>
          {{ interval === 'month' ? plan.monthly : plan.yearly }}
          <span>
            /
            {{ t(interval === 'year' && plan.code !== 'free' ? 'pricing.year' : 'pricing.month') }}
          </span>
        </div>
        <p class="muted billing-note">
          {{
            plan.code === 'free'
              ? t('pricing.freeForever')
              : interval === 'year'
                ? t('pricing.annualCharge', { price: plan.yearly })
                : t('pricing.monthlyCharge')
          }}
        </p>
        <p class="spend">{{ t('pricing.managed', { amount: plan.spend }) }}</p>
        <ul>
          <li>{{ t('pricing.connections', { count: plan.connections }) }}</li>
          <li>{{ t('pricing.history', { days: plan.history }) }}</li>
          <li>{{ t('pricing.people', { members: plan.members, workspaces: plan.workspaces }) }}</li>
          <li>{{ t(plan.code === 'free' ? 'pricing.summary' : 'pricing.full') }}</li>
          <li>{{ t(plan.code === 'free' ? 'pricing.preview' : 'pricing.optimization') }}</li>
        </ul>
        <button
          :class="plan.code === 'starter' ? 'primary' : ''"
          :disabled="busy || (plan.code === current && plan.code === 'free')"
          @click="emit('choose', plan.code, interval)"
        >
          {{
            t(
              plan.code === current
                ? 'pricing.current'
                : plan.code === 'free'
                  ? 'pricing.startFree'
                  : 'pricing.choose',
              { plan: t(`plans.${plan.code}`) },
            )
          }}
        </button>
      </article>
      <article class="panel price-card business">
        <h2>Business</h2>
        <div class="price">{{ t('pricing.custom') }}</div>
        <p class="muted">{{ t('pricing.business') }}</p>
        <p>{{ t('pricing.businessHelp') }}</p>
        <a v-if="contactEmail" class="button" :href="`mailto:${contactEmail}`">
          {{ t('pricing.contact') }}
        </a>
        <p v-else class="muted">{{ t('pricing.businessSoon') }}</p>
      </article>
    </div>
    <p class="muted">
      <small>{{ t('pricing.noOverage') }}</small>
    </p>
  </section>
</template>
<style scoped>
.pricing-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 1rem;
}
.price-card {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  padding: 1.4rem;
}
.featured {
  border: 2px solid #3869d8;
  position: relative;
}
.price {
  font-size: 2.35rem;
  line-height: 1.1;
  letter-spacing: -0.06em;
  margin-top: 0.5rem;
  font-weight: 650;
}
.price small {
  font-size: 0.75rem;
  letter-spacing: 0;
  font-weight: 500;
}
.price span {
  font-size: 0.8rem;
  color: var(--muted);
  letter-spacing: 0;
  font-weight: 400;
}
.billing-note {
  font-size: 0.8rem;
  min-height: 2.5rem;
}
.spend {
  font-size: 0.9rem;
  font-weight: 600;
}
.price-card ul {
  padding-left: 1rem;
  margin: 0 0 0.6rem;
  display: grid;
  gap: 0.65rem;
  font-size: 0.875rem;
  flex: 1;
}
.price-card button,
.price-card > .button {
  width: 100%;
  margin-top: auto;
}
.business .price {
  font-size: 1.7rem;
}
.business p {
  font-size: 0.875rem;
}
@media (max-width: 1180px) {
  .pricing-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 600px) {
  .pricing-grid {
    grid-template-columns: 1fr;
  }
}
</style>
