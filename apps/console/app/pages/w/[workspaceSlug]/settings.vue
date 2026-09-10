<script setup lang="ts">
const { t } = useI18n()
const { workspace } = useApi()
</script>
<template>
  <div class="stack">
    <div class="row">
      <NuxtLink
        v-for="tab in ['general', 'members', 'billing']"
        :key="tab"
        class="button"
        :to="`/w/${workspace?.slug}/settings/${tab}`"
      >
        {{ t(`nav.${tab}`) }}
      </NuxtLink>
    </div>
    <NuxtPage />
  </div>
</template>
