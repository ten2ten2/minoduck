<script setup lang="ts">
definePageMeta({ layout: 'workspace' })
const { workspace } = useApi()
if (!workspace.value) throw createError({ statusCode: 404, statusMessage: 'Workspace not found' })
</script>
<template><NuxtPage /></template>
