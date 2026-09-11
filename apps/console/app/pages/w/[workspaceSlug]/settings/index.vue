<script setup lang="ts">
const route = useRoute()
await navigateTo(`/w/${String(route.params.workspaceSlug)}/settings/general`, { replace: true })
</script>
<template><div /></template>
