// Router hooks for client code: usePathname, useSearchParams, useParams,
// useRouter and useSelectedLayoutSegment(s), with the Next.js signatures.
//
// Each hook returns a derived getter (see krateDerived). Code compiled from
// React source reads a hook result through krateHook(), so the value follows
// the URL across SPA navigation, back and forward. The getters carry the
// current value as their string/JSON form too, for code that does not go
// through the compiler's hook lowering.

import { createSignal, krateDerived } from './signal.js';
import { getRequestContext } from './request-context.js';
import * as serverNav from './navigation-server.js';
import { getNavigationState, krateRouter, subscribeNavigation } from './router.js';

interface RouteSnapshot {
  pathname: string;
  search: string;
  /** The route pattern of the current document ("/blog/[slug]"), "" for a static route. */
  route: string;
}

/** The route pattern the compiler wrote into a dynamic page ("/blog/[slug]"). */
function documentRoute(): string {
  const set = (globalThis as { __KRATE_ROUTE__?: unknown }).__KRATE_ROUTE__;
  if (typeof set === 'string') return set;
  if (typeof document === 'undefined') return '';
  const el = document.getElementById('__krate_page');
  if (!el) return '';
  try {
    const data = JSON.parse(el.textContent || '{}') as { r?: string };
    return typeof data.r === 'string' ? data.r : '';
  } catch {
    return '';
  }
}

/** The path without a trailing slash, as Next.js reports it ("/" stays). */
function cleanPath(pathname: string): string {
  return pathname.length > 1 && pathname.endsWith('/') ? pathname.replace(/\/+$/, '') || '/' : pathname;
}

function snapshot(): RouteSnapshot {
  if (typeof location === 'undefined') return { pathname: '/', search: '', route: '' };
  const { pathname, search } = getNavigationState();
  return { pathname: cleanPath(pathname), search, route: documentRoute() };
}

let state: (() => RouteSnapshot) | null = null;

/** The shared reactive route snapshot; created on first use, in the browser only. */
function routeState(): () => RouteSnapshot {
  if (state) return state;
  const [read, write] = createSignal<RouteSnapshot>(snapshot());
  state = read;
  if (typeof window !== 'undefined') {
    const refresh = () => {
      const next = snapshot();
      const prev = read();
      if (next.pathname !== prev.pathname || next.search !== prev.search || next.route !== prev.route) write(next);
    };
    subscribeNavigation(refresh);
    // The router swaps the document before it emits the final status.
    window.addEventListener('krate:navigate-end', refresh);
  }
  return read;
}

/** Test hook: forgets the shared snapshot so the next hook call reads the URL again. */
export function krateResetNavigation(): void {
  state = null;
}

function describe<T>(getter: () => T, text: () => string): () => T {
  // A getter that reads as its value: `${usePathname()}` works for code that
  // was not lowered to read hook results through a getter.
  const g = getter as unknown as { toString: () => string; valueOf: () => unknown; toJSON: () => unknown };
  g.toString = text;
  g.toJSON = () => getter();
  return getter;
}

/** The current URL path without the base path and the query ("/blog/hello"). */
export function usePathname(): string {
  // Server rendering runs the page source as written, so a hook returns the
  // plain value for the request being rendered.
  if (getRequestContext()) return serverNav.usePathname();
  const read = routeState();
  const derived = krateDerived(() => read().pathname);
  return describe(derived, () => derived()) as unknown as string;
}

/** The query string as a URLSearchParams (read-only in Next.js). */
export function useSearchParams(): URLSearchParams {
  if (getRequestContext()) return serverNav.useSearchParams();
  const read = routeState();
  const derived = krateDerived(() => new URLSearchParams(read().search));
  return describe(derived, () => read().search) as unknown as URLSearchParams;
}

type ParamValue = string | string[];

function safeDecode(segment: string): string {
  try {
    return decodeURIComponent(segment);
  } catch {
    return segment;
  }
}

/**
 * Matches a pathname against a route pattern ("/blog/[slug]", "/docs/[...rest]",
 * "/shop/[[...path]]"). Catch-all params are arrays; an optional catch-all that
 * matched nothing is absent.
 */
function matchRouteParams(route: string, pathname: string): Record<string, ParamValue> {
  const out: Record<string, ParamValue> = {};
  if (!route) return out;
  const pat = route.split('/').filter(Boolean);
  const parts = pathname.split('/').filter(Boolean);
  for (let i = 0; i < pat.length; i++) {
    const seg = pat[i];
    const m = /^\[(\[)?(\.\.\.)?([^\]]+)\]?\]$/.exec(seg);
    if (!m) continue;
    const name = m[3];
    if (m[2]) {
      const rest = parts.slice(i).map(safeDecode);
      if (rest.length > 0) out[name] = rest;
      return out;
    }
    if (i < parts.length) out[name] = safeDecode(parts[i]);
  }
  return out;
}

/** The dynamic route params of the current page ({ slug: "hello" }). */
export function useParams<T extends Record<string, ParamValue> = Record<string, ParamValue>>(): T {
  if (getRequestContext()) return serverNav.useParams<T>();
  const read = routeState();
  const derived = krateDerived(() => {
    const s = read();
    return matchRouteParams(s.route, s.pathname) as T;
  });
  return describe(derived, () => JSON.stringify(derived())) as unknown as T;
}

export interface AppRouterInstance {
  push(href: string, options?: { scroll?: boolean }): void;
  replace(href: string, options?: { scroll?: boolean }): void;
  back(): void;
  forward(): void;
  refresh(): void;
  prefetch(href: string): void;
}

const routerInstance: AppRouterInstance = {
  push: (href, options) => void krateRouter.push(href, options),
  replace: (href, options) => void krateRouter.replace(href, options),
  back: () => krateRouter.back(),
  forward: () => krateRouter.forward(),
  refresh: () => void krateRouter.refresh(),
  prefetch: (href) => void krateRouter.prefetch(href),
};

/** Programmatic navigation: push, replace, back, forward, refresh, prefetch. */
export function useRouter(): AppRouterInstance {
  if (getRequestContext()) return serverNav.useRouter();
  return routerInstance;
}

/**
 * The path segments below depth, as the layout at that depth sees them:
 * `useSelectedLayoutSegments()` in app/dashboard/layout.tsx on
 * /dashboard/settings/profile is ["settings", "profile"]. The compiler passes
 * the layout's depth; a call without one counts from the root.
 */
export function useSelectedLayoutSegments(parallelRouteKey?: string, depth?: number): string[] {
  if (getRequestContext()) return serverNav.useSelectedLayoutSegments(parallelRouteKey, depth);
  const read = routeState();
  void parallelRouteKey;
  const at = typeof depth === 'number' ? depth : 0;
  const derived = krateDerived(() => {
    const parts = read().pathname.split('/').filter(Boolean).map(safeDecode);
    return parts.slice(at);
  });
  return describe(derived, () => derived().join('/')) as unknown as string[];
}

/** The first segment below the layout, or null on the layout's own page. */
export function useSelectedLayoutSegment(parallelRouteKey?: string, depth?: number): string | null {
  if (getRequestContext()) return serverNav.useSelectedLayoutSegment(parallelRouteKey, depth);
  const read = routeState();
  void parallelRouteKey;
  const at = typeof depth === 'number' ? depth : 0;
  const derived = krateDerived(() => {
    const parts = read().pathname.split('/').filter(Boolean).map(safeDecode);
    return parts.length > at ? parts[at] : null;
  });
  return describe(derived, () => String(derived())) as unknown as string | null;
}

/** Throws the signal the host answers with a redirect while rendering; navigates in the browser. */
export function redirect(url: string): never {
  if (typeof location !== 'undefined') void krateRouter.push(url);
  const err = new Error('Server action redirect to ' + url) as Error & { __krateRedirect: string };
  err.__krateRedirect = url;
  throw err;
}

/** redirect() with a replace navigation. */
export function permanentRedirect(url: string): never {
  return redirect(url);
}

export { notFound, forbidden, unauthorized } from './request-context.js';
