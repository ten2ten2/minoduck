export function useBillingIntent() {
  const intent = useCookie<{ plan: 'starter' | 'team'; interval: 'month' | 'year' } | null>(
    'md_billing_intent',
    { maxAge: 1800, sameSite: 'lax', path: '/' },
  )
  const capture = (query: Record<string, unknown>) => {
    if (
      (query.plan === 'starter' || query.plan === 'team') &&
      (query.interval === 'month' || query.interval === 'year')
    )
      intent.value = { plan: query.plan, interval: query.interval }
  }
  const destination = (slug: string, fallback = 'overview') =>
    `/w/${slug}/${intent.value ? 'settings/billing' : fallback}`
  return { intent, capture, destination }
}
