export default {
  fetch(request) {
    const url = new URL(request.url)
    if (url.hostname !== 'minoduck.ai') return new Response('Not found', { status: 404 })
    url.hostname = 'www.minoduck.ai'
    url.protocol = 'https:'
    url.port = ''
    return Response.redirect(url.toString(), 308)
  },
}
