export interface User {
  id: string
  email: string
  locale: string
  theme: string
  csrf_token: string
  locale_explicit: boolean
}

export interface Workspace {
  id: string
  slug: string
  name: string
  role: 'owner' | 'admin' | 'viewer'
}

export function useSession() {
  const user = useState<User | null>('md-user', () => null)
  const workspaces = useState<Workspace[]>('md-workspaces', () => [])

  const load = async () => {
    const nextUser = await $fetch<User>('/api/v1/me', { credentials: 'same-origin' })
    const nextWorkspaces = await $fetch<Workspace[]>('/api/v1/workspaces', {
      credentials: 'same-origin',
    })
    user.value = nextUser
    workspaces.value = nextWorkspaces
  }

  return { user, workspaces, load }
}
