<script setup lang="ts">
import {
  LayoutDashboard,
  ReceiptText,
  GitCompareArrows,
  Lightbulb,
  Bell,
  Plug,
  Settings,
  FileText,
  Download,
  Menu,
  LogOut,
  Plus,
} from '@lucide/vue'
const { t } = useI18n()
const { user, workspaces, workspace, api, errorText } = useApi()
const { preferenceError, savePreferences } = useAccountPreferences()
const route = useRoute()
const mobileOpen = ref(false)
const error = ref('')
const base = computed(() => `/w/${workspace.value?.slug}`)
const main = [
  { key: 'overview', icon: LayoutDashboard },
  { key: 'costs', icon: ReceiptText },
  { key: 'reconciliation', icon: GitCompareArrows },
  { key: 'insights', icon: Lightbulb },
  { key: 'alerts', icon: Bell },
]
const secondary = [
  { key: 'connections', path: 'connections', icon: Plug },
  { key: 'invoices', path: 'invoices', icon: FileText },
  { key: 'exports', path: 'exports', icon: Download },
  { key: 'settings', path: 'settings/general', icon: Settings },
]
const active = (path: string) => route.path.includes(`${base.value}/${path}`)
const selectWorkspace = (event: Event) =>
  navigateTo(`/w/${(event.target as HTMLSelectElement).value}/overview`)
async function signout() {
  try {
    await api('/auth/logout', { method: 'POST' })
    clearNuxtData()
    user.value = null
    workspaces.value = []
    await navigateTo('/login')
  } catch (e) {
    error.value = errorText(e)
  }
}
watch(
  () => route.path,
  () => {
    mobileOpen.value = false
  },
)
</script>
<template>
  <div class="console-shell">
    <div v-if="mobileOpen" class="nav-backdrop" @click="mobileOpen = false" />
    <aside class="sidebar" :class="{ opened: mobileOpen }">
      <NuxtLink to="/onboarding" class="sidebar-brand"><BrandWordmark /></NuxtLink>
      <label class="workspace-select">
        <span>{{ t('nav.workspace') }}</span>
        <select :value="workspace?.slug" @change="selectWorkspace">
          <option v-for="w in workspaces" :key="w.id" :value="w.slug">{{ w.name }}</option>
        </select>
      </label>
      <nav :aria-label="t('nav.workspace')">
        <NuxtLink
          v-for="item in main"
          :key="item.key"
          :to="`${base}/${item.key}`"
          :class="{ selected: active(item.key) }"
          :aria-current="active(item.key) ? 'page' : undefined"
        >
          <component :is="item.icon" :size="18" />
          <span>{{ t(`nav.${item.key}`) }}</span>
        </NuxtLink>
      </nav>
      <nav class="secondary">
        <NuxtLink
          v-for="item in secondary"
          :key="item.key"
          :to="`${base}/${item.path}`"
          :class="{ selected: active(item.key) }"
        >
          <component :is="item.icon" :size="18" />
          <span>{{ t(`nav.${item.key}`) }}</span>
        </NuxtLink>
      </nav>
      <div class="sidebar-foot">
        <NuxtLink to="/onboarding">
          <Plus :size="16" />
          {{ t('onboarding.add') }}
        </NuxtLink>
        <div class="account-email">{{ user?.email }}</div>
        <button @click="signout">
          <LogOut :size="16" />
          {{ t('nav.signout') }}
        </button>
      </div>
    </aside>
    <div class="console-body">
      <header class="console-top">
        <div class="row">
          <button
            class="mobile-menu"
            :aria-label="t('nav.menu')"
            :aria-expanded="mobileOpen"
            @click="mobileOpen = !mobileOpen"
          >
            <Menu :size="20" />
          </button>
          <span class="workspace-name">{{ workspace?.name }}</span>
          <span class="separator">/</span>
          <span class="muted">{{ t('site.eyebrow') }}</span>
        </div>
        <PreferencesControl @change="savePreferences" />
      </header>
      <main class="console-main">
        <div v-if="error || preferenceError" class="notice error" role="alert">
          {{ error || preferenceError }}
        </div>
        <slot />
      </main>
    </div>
  </div>
</template>
<style scoped>
.console-shell {
  min-height: 100vh;
  display: flex;
}
.sidebar {
  width: 238px;
  position: fixed;
  inset: 0 auto 0 0;
  display: flex;
  flex-direction: column;
  background: var(--sidebar);
  color: #c4cddd;
  padding: 1.75rem 1rem;
  z-index: 30;
}
.sidebar-brand {
  color: #f2f5fa;
  padding: 0 0.5rem;
  margin-bottom: 2rem;
}
.workspace-select {
  padding: 0 0.5rem;
  margin-bottom: 1.75rem;
  gap: 0.6rem;
  font-size: 0.75rem;
  color: #8f9db3;
}
.workspace-select select {
  background: #ffffff09;
  color: #e3eaf6;
  border-color: #ffffff20;
  font-size: 0.875rem;
}
.sidebar nav {
  display: grid;
  gap: 5px;
}
.sidebar nav a {
  display: flex;
  align-items: center;
  gap: 0.8rem;
  padding: 0.7rem 0.85rem;
  border-radius: 7px;
  font-size: 0.875rem;
}
.sidebar nav a:hover {
  background: #ffffff0b;
}
.sidebar nav .selected {
  background: #2a3952;
  color: #fff;
}
.sidebar nav .selected svg {
  color: #f0bd48;
}
.secondary {
  margin-top: 2rem;
  border-top: 1px solid #ffffff12;
  padding-top: 1.2rem;
}
.sidebar-foot {
  margin-top: auto;
  padding: 0.9rem 0.5rem 0;
}
.sidebar-foot a {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.8rem;
}
.account-email {
  font-size: 0.75rem;
  overflow: hidden;
  text-overflow: ellipsis;
  margin-top: 1.3rem;
  color: #8f9db3;
}
.sidebar-foot button {
  border: 0;
  background: transparent;
  color: #b5c2d6;
  padding: 0.5rem 0;
  font-weight: 400;
}
.console-body {
  margin-left: 238px;
  min-width: 0;
  flex: 1;
}
.console-top {
  height: 78px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0 2.2rem;
  background: var(--panel);
  font-size: 0.875rem;
}
.workspace-name {
  font-weight: 600;
}
.separator {
  color: var(--line);
}
.console-main {
  max-width: 1560px;
  margin: 0 auto;
  padding: 2.2rem;
  display: grid;
  gap: 1.2rem;
}
.mobile-menu {
  display: none;
}
.nav-backdrop {
  display: none;
}
@media (max-width: 1000px) {
  .console-top {
    padding: 1rem 1.25rem;
    height: auto;
    min-height: 78px;
    flex-wrap: wrap;
  }
  .console-main {
    padding: 1.4rem;
  }
  .console-top .separator,
  .console-top .muted {
    display: none;
  }
}
@media (max-width: 800px) {
  .sidebar {
    transform: translateX(-100%);
    transition: transform 0.2s;
  }
  .sidebar.opened {
    transform: translateX(0);
  }
  .console-body {
    margin-left: 0;
  }
  .mobile-menu {
    display: flex;
    padding: 0.4rem;
  }
  .nav-backdrop {
    display: block;
    position: fixed;
    inset: 0;
    background: #0006;
    z-index: 29;
  }
  .console-main {
    padding: 1rem;
  }
}
</style>
