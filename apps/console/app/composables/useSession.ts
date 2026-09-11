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
  billing_suspended?: boolean
}

export function useSession() {
  const user = useState<User | null>('md-user', () => null)
  const workspaces = useState<Workspace[]>('md-workspaces', () => [])

  const load = async () => {
    const [nextUser, nextWorkspaces] = await Promise.all([
      $fetch<User>('/api/v1/me', { credentials: 'same-origin' }),
      $fetch<Workspace[]>('/api/v1/workspaces', { credentials: 'same-origin' }),
    ])
    user.value = nextUser
    workspaces.value = nextWorkspaces
  }

  return { user, workspaces, load }
}
