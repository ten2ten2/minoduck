<script setup lang="ts">
const props = defineProps<{ value: string }>()
const { t, te } = useI18n()
const tone = computed(() =>
  ['ready', 'succeeded', 'matched', 'active'].includes(props.value)
    ? 'success'
    : ['mismatch', 'error', 'failed', 'past_due'].includes(props.value)
      ? 'danger'
      : ['partial', 'not_comparable', 'pending_source', 'validating'].includes(props.value)
        ? 'warning'
        : '',
)
</script>
<template>
  <span class="badge" :class="tone">
    {{ te(`status.${value}`) ? t(`status.${value}`) : value }}
  </span>
</template>
