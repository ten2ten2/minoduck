<script setup lang="ts">
const route = useRoute()
await navigateTo(`/w/${String(route.params.workspaceSlug)}/overview`, { replace: true })
</script>
<template><div /></template>
