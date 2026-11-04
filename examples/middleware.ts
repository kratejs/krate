import type { Request } from '@krate/runtime'

export function middleware(request: Request) {
  const url = new URL(request.url)

  // Add security header to all responses
  const headers: Record<string, string> = {
    'X-Frame-Options': 'DENY',
    'X-Content-Type-Options': 'nosniff',
  }

  // Redirect /old-blog to /blog
  if (url.pathname === '/old-blog') {
    return new Response(null, {
      status: 301,
      headers: { ...headers, Location: '/blog' },
    })
  }

  // Block /admin in production
  if (url.pathname.startsWith('/admin')) {
    return new Response('Forbidden', {
      status: 403,
      headers,
    })
  }

  // Continue to the page handler, adding headers. Any other Response
  // short-circuits; x-middleware-next marks a pass-through.
  return new Response(null, {
    headers: { ...headers, 'x-middleware-next': '1' },
  })
}
