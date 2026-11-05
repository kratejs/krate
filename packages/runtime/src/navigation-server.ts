// The router hooks for server rendering (SSR, ISR and streaming requests, and
// the portable handler). They read the request being rendered, so a page
// rendered per request sees its own URL. See navigation.ts for the browser side.

import { getRequestContext } from './request-context.js';
import { redirect } from './action-context.js';

export { redirect as permanentRedirect };

type ParamValue = string | string[];

/** The URL path of the request: the route pattern with its params filled in. */
function requestPath(): { path: string; route: string; params: Record<string, string> } {
  const ctx = getRequestContext();
  if (!ctx) return { path: '/', route: '', params: {} };
  const route = ctx.url.split('?')[0] || '/';
  const params = ctx.params || {};
  const parts: string[] = [];
  for (const seg of route.split('/').filter(Boolean)) {
    const m = /^\[(\[)?(\.\.\.)?([^\]]+)\]?\]$/.exec(seg);
    if (!m) {
      parts.push(seg);
      continue;
    }
    const value = params[m[3]];
    if (value !== undefined && value !== '') parts.push(...value.split('/').filter(Boolean));
  }
  return { path: '/' + parts.join('/'), route, params };
}

function decode(segment: string): string {
  try {
    return decodeURIComponent(segment);
  } catch {
    return segment;
  }
}

/** The current URL path without the query ("/blog/hello"). */
export function usePathname(): string {
  return requestPath().path;
}

/** The query string of the request. */
export function useSearchParams(): URLSearchParams {
  const ctx = getRequestContext();
  const out = new URLSearchParams();
  for (const [k, v] of Object.entries((ctx && ctx.searchParams) || {})) out.append(k, String(v));
  return out;
}

/** The dynamic route params of the request; catch-all params are arrays. */
export function useParams<T extends Record<string, ParamValue> = Record<string, ParamValue>>(): T {
  const { route, params } = requestPath();
  const out: Record<string, ParamValue> = {};
  for (const seg of route.split('/').filter(Boolean)) {
    const m = /^\[(\[)?(\.\.\.)?([^\]]+)\]?\]$/.exec(seg);
    if (!m) continue;
    const value = params[m[3]];
    if (value === undefined || value === '') continue;
    out[m[3]] = m[2] ? value.split('/').filter(Boolean).map(decode) : decode(value);
  }
  return out as T;
}

export interface AppRouterInstance {
  push(href: string, options?: { scroll?: boolean }): void;
  replace(href: string, options?: { scroll?: boolean }): void;
  back(): void;
  forward(): void;
  refresh(): void;
  prefetch(href: string): void;
}

function browserOnly(name: string): () => never {
  return () => {
    throw new Error(`useRouter().${name}() can only be called in the browser; use redirect() while rendering`);
  };
}

/** A router whose methods throw: navigation belongs in event handlers, which run in the browser. */
export function useRouter(): AppRouterInstance {
  return {
    push: browserOnly('push'),
    replace: browserOnly('replace'),
    back: browserOnly('back'),
    forward: browserOnly('forward'),
    refresh: browserOnly('refresh'),
    prefetch: () => {},
  };
}

/** The path segments below depth (the layout's depth, passed by the compiler). */
export function useSelectedLayoutSegments(_parallelRouteKey?: string, depth?: number): string[] {
  return requestPath()
    .path.split('/')
    .filter(Boolean)
    .map(decode)
    .slice(typeof depth === 'number' ? depth : 0);
}

/** The first segment below the layout, or null. */
export function useSelectedLayoutSegment(parallelRouteKey?: string, depth?: number): string | null {
  const segments = useSelectedLayoutSegments(parallelRouteKey, depth);
  return segments.length > 0 ? segments[0] : null;
}
