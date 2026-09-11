export default defineNuxtRouteMiddleware(async (to) => {
  if (to.path === '/invitations/accept') return

  const { user, load } = useSession()
  const isLogin = to.path === '/login'
  const hasLoginToken = isLogin && typeof to.query.token === 'string' && to.query.token.length > 0
  if (hasLoginToken) return

  if (!user.value) {
    try {
      await load()
    } catch (error: any) {
      if (error?.status === 401 || error?.statusCode === 401) {
        return isLogin ? undefined : navigateTo('/login')
      }
      throw createError({ statusCode: 503, statusMessage: 'Application service unavailable' })
    }
  }

  if (!isLogin) return

  // Let the login page capture a pricing intent before redirecting an already
  // authenticated user into the app. Plain "Open workspace" goes directly in.
  const hasBillingIntent =
    (to.query.plan === 'starter' || to.query.plan === 'team') &&
    (to.query.interval === 'month' || to.query.interval === 'year')
  if (hasBillingIntent) return
  return navigateTo('/onboarding', { replace: true })
})
