<script setup lang="ts">
const { t } = useI18n()
const { scoped, workspace, errorText } = useApi()
const { date } = useMoney()
const { data, error, refresh } = await useAsyncData(`exports-${workspace.value?.id}`, () =>
  scoped<any[]>('/exports'),
)
</script>
<template>
  <div class="stack">
    <PageHeading :title="t('nav.exports')" :description="t('exports.subtitle')">
      <button @click="refresh()">{{ t('common.refresh') }}</button>
    </PageHeading>
    <div v-if="error" class="notice error" role="alert">{{ errorText(error) }}</div>
    <div v-else-if="data?.length" class="panel table-scroll">
      <table>
        <thead>
          <tr>
            <th>{{ t('common.start') }}</th>
            <th>{{ t('common.status') }}</th>
            <th>{{ t('exports.expires') }}</th>
            <th />
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in data" :key="item.id">
            <td>{{ date(item.created_at) }}</td>
            <td><StatusBadge :value="item.state" /></td>
            <td>{{ date(item.expires_at) }}</td>
            <td>
              <a
                v-if="item.state === 'ready' && new Date(item.expires_at).getTime() > Date.now()"
                class="button"
                :href="`/api/v1/workspaces/${workspace?.id}/exports/${item.id}/download`"
              >
                {{ t('exports.download') }}
              </a>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-else class="panel empty">
      <h2>{{ t('exports.empty') }}</h2>
    </div>
  </div>
</template>
