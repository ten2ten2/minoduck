<script setup lang="ts">
const { t } = useI18n()
const { workspace } = useApi()
</script>
<template>
  <div class="panel stack">
    <h2>{{ t('upgrade.title') }}</h2>
    <p class="muted">{{ t('upgrade.help') }}</p>
    <div>
      <NuxtLink class="button primary" :to="`/w/${workspace?.slug}/settings/billing`">
        {{ t('upgrade.button') }}
      </NuxtLink>
    </div>
  </div>
</template>
