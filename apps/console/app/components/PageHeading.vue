<script setup lang="ts">
defineProps<{ title: string; description?: string }>()
</script>
<template>
  <div class="page-heading">
    <div>
      <h1>{{ title }}</h1>
      <p v-if="description" class="muted">{{ description }}</p>
    </div>
    <div class="row"><slot /></div>
  </div>
</template>
<style scoped>
.page-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1.5rem;
  flex-wrap: wrap;
  margin-bottom: 0.3rem;
}
.page-heading p {
  margin-top: 0.65rem;
  max-width: 760px;
  font-size: 0.9375rem;
}
</style>
