<script setup lang="ts">
const props = defineProps<{
  rows: { date: string; currency: string; amount: string }[]
  currency: string
}>()
const { t } = useI18n()
const { money, date } = useMoney()
const points = computed(() => props.rows.filter((r) => r.currency === props.currency))
const max = computed(() => Math.max(1, ...points.value.map((p) => Number(p.amount))))
const min = computed(() => Math.min(0, ...points.value.map((p) => Number(p.amount))))
const y = (amount: string) => 170 - ((Number(amount) - min.value) / (max.value - min.value)) * 140
const chartPath = computed(() =>
  points.value
    .map(
      (p, i) =>
        `${i ? 'L' : 'M'} ${30 + (i / Math.max(1, points.value.length - 1)) * 730} ${y(p.amount)}`,
    )
    .join(' '),
)
</script>
<template>
  <div v-if="points.length" class="cost-chart">
    <svg viewBox="0 0 790 210" role="img" :aria-label="t('overview.trend')">
      <line
        v-for="line in [30, 65, 100, 135, 170]"
        :key="line"
        x1="30"
        :y1="line"
        x2="760"
        :y2="line"
        stroke="var(--line)"
        stroke-dasharray="3 5"
      />
      <path
        v-if="points.length > 1"
        :d="chartPath"
        fill="none"
        stroke="var(--accent)"
        stroke-width="3"
        stroke-linejoin="round"
      />
      <circle
        v-for="(p, i) in points"
        :key="p.date"
        :cx="30 + (i / Math.max(1, points.length - 1)) * 730"
        :cy="y(p.amount)"
        r="4"
        fill="var(--accent)"
      >
        <title>{{ date(p.date) }} · {{ money(p.amount, p.currency) }}</title>
      </circle>
    </svg>
    <div class="row between muted">
      <small>{{ date(points[0]?.date) }}</small>
      <small>{{ currency }}</small>
      <small>{{ date(points[points.length - 1]?.date) }}</small>
    </div>
    <details>
      <summary>{{ t('overview.table') }}</summary>
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>{{ t('common.start') }}</th>
              <th class="amount">{{ t('common.amount') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in points" :key="p.date">
              <td>{{ date(p.date) }}</td>
              <td class="amount">{{ money(p.amount, p.currency) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </details>
  </div>
  <div v-else class="empty">
    <p>{{ t('common.unavailable') }}</p>
  </div>
</template>
<style scoped>
svg {
  width: 100%;
  display: block;
  max-height: 260px;
}
details {
  margin-top: 1rem;
  font-size: 0.8rem;
  color: var(--muted);
}
summary {
  cursor: pointer;
}
</style>
