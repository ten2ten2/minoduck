<script setup lang="ts">
const route = useRoute()
</script>
<template><ConnectionManager :id="String(route.params.cid)" /></template>
