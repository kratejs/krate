// Next's request extras on a route handler's Request: a read-only
// request.cookies and request.nextUrl. Shared by the server handler and
// @krate/runtime/testing so tests see what the server passes.

export function requestCookiesOf(headers: Headers): Map<string, string> {
  const jar = new Map<string, string>();
  for (const part of String(headers.get("cookie") || "").split(";")) {
    const i = part.indexOf("=");
    if (i <= 0) continue;
    const name = part.slice(0, i).trim();
    if (!name || jar.has(name)) continue;
    // Same parsing as the action context and the Node sidecar: unquote, then percent-decode.
    let value = part.slice(i + 1).trim();
    if (value.startsWith('"') && value.endsWith('"')) value = value.slice(1, -1);
    try {
      value = decodeURIComponent(value);
    } catch {
      // keep the raw value
    }
    jar.set(name, value);
  }
  return jar;
}

export function attachCookies(request: Request, basePath = ""): Request {
  try {
    const jar = requestCookiesOf(request.headers);
    Object.defineProperty(request, "cookies", {
      value: {
        get: (n: string) => (jar.has(String(n)) ? { name: String(n), value: jar.get(String(n)) } : undefined),
        getAll: (n?: string) => [...jar].filter(([k]) => n === undefined || k === String(n)).map(([name, value]) => ({ name, value })),
        has: (n: string) => jar.has(String(n)),
      },
      enumerable: false,
    });
    Object.defineProperty(request, "nextUrl", { value: makeNextUrl(request.url, basePath), enumerable: false });
  } catch {
    // frozen request objects keep working without the cookies accessor
  }
  return request;
}

/** A URL carrying Next's NextURL extras: basePath (pathname never includes it) and clone(). */
export function makeNextUrl(href: string, basePath: string): URL {
  const u = new URL(href);
  Object.defineProperty(u, "basePath", { value: basePath, enumerable: false });
  Object.defineProperty(u, "clone", { value: () => makeNextUrl(u.href, basePath), enumerable: false });
  return u;
}
