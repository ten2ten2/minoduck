export default defineNuxtRouteMiddleware(async (to) => {
  if (to.path === '/login' || to.path === '/invitations/accept') return

  const { user, load } = useSession()
  if (user.value) return

  try {
    await load()
  } catch (error: any) {
    if (error?.status === 401 || error?.statusCode === 401) return navigateTo('/login')
    throw createError({ statusCode: 503, statusMessage: 'Application service unavailable' })
  }
})
