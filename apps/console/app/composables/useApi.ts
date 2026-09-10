export interface User {
  id: string
  email: string
  locale: string
  theme: string
  csrf_token: string
  locale_explicit?: boolean
}
export interface Workspace {
  id: string
  slug: string
  name: string
  role: 'owner' | 'admin' | 'viewer'
}

export function useApi() {
  const user = useState<User | null>('md-user', () => null)
  const workspaces = useState<Workspace[]>('md-workspaces', () => [])
  const route = useRoute()
  const { t, te } = useI18n()
  const workspace = computed(() =>
    workspaces.value.find((w) => w.slug === route.params.workspaceSlug),
  )
  async function api<T = any>(
    path: string,
    options: {
      method?: 'GET' | 'POST' | 'PATCH' | 'DELETE'
      body?: any
      query?: Record<string, any>
      signal?: AbortSignal
    } = {},
  ): Promise<T> {
    return (await $fetch<T>('/api/v1' + path, {
      ...options,
      credentials: 'same-origin',
      headers: user.value ? { 'X-CSRF-Token': user.value.csrf_token } : {},
    })) as T
  }
  const scoped = <T = any>(path: string, options?: Parameters<typeof api>[1]) => {
    if (!workspace.value) return Promise.reject(new Error('Workspace unavailable'))
    return api<T>(`/workspaces/${workspace.value.id}${path}`, options)
  }
  const errorText = (error: any) => {
    const code = error?.data?.error?.code
    return code && te(`errors.${code}`) ? t(`errors.${code}`) : t('common.error')
  }
  const loadUser = async () => {
    user.value = await api<User>('/me')
    workspaces.value = await api<Workspace[]>('/workspaces')
  }
  return { api, scoped, user, workspaces, workspace, errorText, loadUser }
}
