const rules: [string, RegExp][] = [
  ['GET', /^\/(me|providers|workspaces)$/],
  ['PATCH', /^\/me\/preferences$/],
  ['POST', /^\/(auth\/(email\/(start|verify)|logout)|workspaces|invitations\/accept)$/],
  ['GET', /^\/auth\/google\/(start|callback)$/],
  [
    'GET',
    /^\/workspaces\/[a-f0-9-]{36}(\/(members|connections|connections\/[a-f0-9-]{36}|sync-runs\/[a-f0-9-]{36}|imports\/[a-f0-9-]{36}\/preview|overview|costs|costs\/[a-f0-9-]{36}|invoices|invoices\/[a-f0-9-]{36}|reconciliation-runs|reconciliation-runs\/[a-f0-9-]{36}|insights|alert-rules|exports|exports\/[a-f0-9-]{36}(\/download)?|prices|subscription))?$/,
  ],
  [
    'POST',
    /^\/workspaces\/[a-f0-9-]{36}\/(invitations|deletion-requests|connections|connections\/[a-f0-9-]{36}\/(sync|credentials)|imports|imports\/[a-f0-9-]{36}\/commit|invoices|reconciliation-runs|usage-reconciliation-runs|prices|price-comparisons|alert-rules|exports|subscription\/(checkout|portal|change|cancel))$/,
  ],
  [
    'PATCH',
    /^\/workspaces\/[a-f0-9-]{36}(\/(reconciliation-items|insights|alert-rules)\/[a-f0-9-]{36})?$/,
  ],
  ['DELETE', /^\/workspaces\/[a-f0-9-]{36}\/(members|connections|alert-rules)\/[a-f0-9-]{36}$/],
]

export default defineEventHandler(async (event) => {
  const config = useRuntimeConfig(event)
  const request = getRequestURL(event)
  const path = request.pathname.slice('/api/v1'.length)
  const method = getMethod(event)
  setResponseHeader(event, 'Cache-Control', 'private, no-store')
  if (!rules.some(([m, re]) => m === method && re.test(path))) {
    setResponseStatus(event, 404)
    return { error: { code: 'NOT_FOUND' } }
  }
  if (!config.bffServiceToken) {
    setResponseStatus(event, 503)
    return { error: { code: 'BFF_NOT_CONFIGURED' } }
  }
  const origin = new URL(config.apiOrigin)
  if (origin.protocol !== 'https:' && !['localhost', '127.0.0.1'].includes(origin.hostname))
    throw createError({ statusCode: 503, message: 'Invalid API configuration' })
  const headers = new Headers({ 'X-MinoDuck-Service': config.bffServiceToken })
  for (const key of [
    'accept',
    'content-type',
    'cookie',
    'origin',
    'x-csrf-token',
    'idempotency-key',
  ]) {
    const value = getRequestHeader(event, key)
    if (value) headers.set(key, value)
  }
  if (Number(getRequestHeader(event, 'content-length') ?? 0) > 21 * 1024 * 1024)
    throw createError({ statusCode: 413 })
  const body = method === 'GET' || method === 'HEAD' ? undefined : await readRawBody(event, false)
  if (body && body.byteLength > 21 * 1024 * 1024) throw createError({ statusCode: 413 })
  try {
    const response = await fetch(new URL('/api/v1' + path + request.search, origin), {
      method,
      headers,
      body: body as BodyInit | undefined,
      redirect: 'manual',
      signal: AbortSignal.timeout(85000),
    })
    setResponseStatus(event, response.status)
    for (const key of [
      'content-type',
      'content-disposition',
      'location',
      'x-request-id',
      'retry-after',
    ]) {
      const value = response.headers.get(key)
      if (value) setResponseHeader(event, key, value)
    }
    for (const cookie of response.headers.getSetCookie())
      appendResponseHeader(event, 'set-cookie', cookie)
    return response.body
  } catch {
    setResponseStatus(event, 503)
    return { error: { code: 'API_UNAVAILABLE', retryable: true } }
  }
})
