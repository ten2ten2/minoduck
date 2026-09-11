type RouteLike = {
  params: Record<string, string | string[]>
}

type ApiOptions = {
  method?: 'GET' | 'POST' | 'PATCH' | 'DELETE'
  body?: BodyInit | Record<string, unknown>
  query?: Record<string, string | number | boolean | null | undefined>
  signal?: AbortSignal
}

type FetchFailure = {
  data?: { error?: { code?: unknown } }
}

export function useApi(route: RouteLike = useRoute()) {
  const { user, workspaces, load: loadUser } = useSession()
  const { t, te } = useI18n()
  const workspaceSlug = computed(() => {
    const value = route.params.workspaceSlug
    return Array.isArray(value) ? value[0] : value
  })
  const workspace = computed(() => workspaces.value.find((w) => w.slug === workspaceSlug.value))

  async function api<T = void>(path: string, options: ApiOptions = {}): Promise<T> {
    return (await $fetch<T>('/api/v1' + path, {
      ...options,
      credentials: 'same-origin',
      headers: user.value ? { 'X-CSRF-Token': user.value.csrf_token } : {},
    })) as T
  }

  const scoped = <T = void>(path: string, options?: ApiOptions) => {
    if (!workspace.value) return Promise.reject(new Error('Workspace unavailable'))
    return api<T>(`/workspaces/${workspace.value.id}${path}`, options)
  }

  const errorText = (error: unknown) => {
    const code = errorCode(error)
    return typeof code === 'string' && te(`errors.${code}`)
      ? t(`errors.${code}`)
      : t('common.error')
  }
  const errorCode = (error: unknown) => {
    const code = (error as FetchFailure | null)?.data?.error?.code
    return typeof code === 'string' ? code : undefined
  }

  return { api, scoped, user, workspaces, workspace, errorText, errorCode, loadUser }
}
