import { NextResponse, type NextRequest } from 'next/server';

// Answers /mw/* itself and echoes the path it saw, so a case can tell whether
// the trailing-slash redirect ran before it.
export function proxy(req: NextRequest) {
  return new NextResponse('middleware saw ' + req.nextUrl.pathname, {
    headers: { 'content-type': 'text/plain', 'x-mw-path': req.nextUrl.pathname },
  });
}

export const config = { matcher: ['/mw/:path*'] };
