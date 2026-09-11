export default {
  fetch(request) {
    const url = new URL(request.url)
    if (url.hostname !== 'minoduck.ai') return new Response('Not found', { status: 404 })
    url.hostname = 'www.minoduck.ai'
    url.protocol = 'https:'
    url.port = ''
    return new Response(null, {
      status: 308,
      headers: {
        Location: url.toString(),
        'Strict-Transport-Security': 'max-age=31536000; includeSubDomains',
        'X-Content-Type-Options': 'nosniff',
        'Referrer-Policy': 'strict-origin-when-cross-origin',
      },
    })
  },
}
