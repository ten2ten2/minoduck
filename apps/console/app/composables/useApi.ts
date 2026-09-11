type RouteLike = {
  params: Record<string, string | string[]>
}

export function useApi(route: RouteLike = useRoute()) {
  const { user, workspaces, load: loadUser } = useSession()
  const { t, te } = useI18n()
  const workspaceSlug = computed(() => {
    const value = route.params.workspaceSlug
    return Array.isArray(value) ? value[0] : value
  })
  const workspace = computed(() => workspaces.value.find((w) => w.slug === workspaceSlug.value))

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

  return { api, scoped, user, workspaces, workspace, errorText, loadUser }
}
