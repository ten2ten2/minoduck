<script setup lang="ts">
const route = useRoute()
</script>
<template><ReconciliationView :id="String(route.params.rid)" /></template>
