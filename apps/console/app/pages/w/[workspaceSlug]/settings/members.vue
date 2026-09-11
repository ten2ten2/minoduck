<script setup lang="ts">
const { t } = useI18n()
const { scoped, workspace, errorText } = useApi()
const { data, error, refresh } = await useAsyncData(
  () => `members-${workspace.value?.id}`,
  (_app, { signal }) => scoped<Member[]>('/members', { signal }),
)
const open = ref(false),
  email = ref(''),
  role = ref('viewer'),
  busy = ref(false),
  actionError = ref(''),
  message = ref(''),
  devLink = ref('')
const canRemove = (member: Member) =>
  member.role !== 'owner' && (workspace.value?.role === 'owner' || member.role === 'viewer')
async function invite() {
  busy.value = true
  actionError.value = ''
  try {
    const result = await scoped<ApiAction>('/invitations', {
      method: 'POST',
      body: { email: email.value, role: role.value },
    })
    message.value = t('settings.invited')
    devLink.value = result.development_link ?? ''
    open.value = false
  } catch (e) {
    actionError.value = errorText(e)
  } finally {
    busy.value = false
  }
}
async function remove(id: string) {
  busy.value = true
  actionError.value = ''
  try {
    await scoped(`/members/${id}`, { method: 'DELETE' })
    await refresh()
  } catch (e) {
    actionError.value = errorText(e)
  } finally {
    busy.value = false
  }
}
</script>
<template>
  <div class="stack">
    <PageHeading :title="t('nav.members')">
      <button v-if="workspace?.role !== 'viewer'" class="primary" @click="open = !open">
        {{ t('settings.invite') }}
      </button>
    </PageHeading>
    <div v-if="error || actionError" class="notice error" role="alert">
      {{ actionError || errorText(error) }}
    </div>
    <div v-if="message" class="notice">
      {{ message }}
      <a v-if="devLink" class="link" :href="devLink">{{ t('settings.accept') }}</a>
    </div>
    <form v-if="open" class="panel stack" @submit.prevent="invite">
      <label>
        {{ t('common.email') }}
        <input v-model="email" type="email" required />
      </label>
      <label>
        {{ t('common.role') }}
        <select v-model="role">
          <option value="viewer">{{ t('common.viewer') }}</option>
          <option v-if="workspace?.role === 'owner'" value="admin">{{ t('common.admin') }}</option>
        </select>
      </label>
      <div class="row">
        <button class="primary" :disabled="busy">{{ t('settings.invite') }}</button>
        <button type="button" @click="open = false">{{ t('common.cancel') }}</button>
      </div>
    </form>
    <div class="panel table-scroll">
      <table>
        <thead>
          <tr>
            <th>{{ t('common.email') }}</th>
            <th>{{ t('common.role') }}</th>
            <th />
          </tr>
        </thead>
        <tbody>
          <tr v-for="member in data" :key="member.id">
            <td>{{ member.email }}</td>
            <td>{{ t(`common.${member.role}`) }}</td>
            <td>
              <details v-if="canRemove(member)">
                <summary class="danger">{{ t('settings.remove') }}</summary>
                <button class="danger" :disabled="busy" @click="remove(member.id)">
                  {{ t('common.delete') }} · {{ member.email }}
                </button>
              </details>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
