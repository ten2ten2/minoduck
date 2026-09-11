import { isAllowedBffRoute } from '../../utils/bff-routes'

export default defineEventHandler(async (event) => {
  const config = useRuntimeConfig(event)
  const request = getRequestURL(event)
  const path = request.pathname.slice('/api/v1'.length)
  const method = getMethod(event)
  setResponseHeader(event, 'Cache-Control', 'private, no-store')
  if (!isAllowedBffRoute(method, path)) {
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
  // Cloudflare overwrites CF-Connecting-IP at the edge. Do not trust a caller's
  // first X-Forwarded-For value for auth throttling.
  const clientAddress =
    getRequestHeader(event, 'cf-connecting-ip') ?? getRequestIP(event) ?? 'unknown'
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(clientAddress))
  headers.set(
    'X-MinoDuck-Client-Hash',
    Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join(''),
  )
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
  const bodyLimit = path.includes('/imports') ? 21 * 1024 * 1024 : 64 * 1024
  if (Number(getRequestHeader(event, 'content-length') ?? 0) > bodyLimit)
    throw createError({ statusCode: 413 })
  const body = method === 'GET' || method === 'HEAD' ? undefined : await readRawBody(event, false)
  if (body && body.byteLength > bodyLimit) throw createError({ statusCode: 413 })
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
  } catch (error) {
    const requestId = crypto.randomUUID()
    console.error('BFF upstream request failed', {
      requestId,
      method,
      path,
      error: error instanceof Error ? error.name : 'UnknownError',
    })
    setResponseHeader(event, 'X-Request-ID', requestId)
    setResponseStatus(event, 503)
    return { error: { code: 'API_UNAVAILABLE', retryable: true, request_id: requestId } }
  }
})
