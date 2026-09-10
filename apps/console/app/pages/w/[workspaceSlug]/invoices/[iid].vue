<script setup lang="ts">
const route = useRoute()
</script>
<template><InvoiceManager :id="String(route.params.iid)" /></template>
