export default defineNuxtRouteMiddleware(async (to) => {
  if (to.path === '/login' || to.path === '/invitations/accept') return
  const { user, loadUser, errorText } = useApi(to)
  if (!user.value) {
    try {
      await loadUser()
    } catch (e: any) {
      if (e?.status === 401 || e?.statusCode === 401) return navigateTo('/login')
      throw createError({ statusCode: 503, statusMessage: errorText(e) })
    }
  }
})
