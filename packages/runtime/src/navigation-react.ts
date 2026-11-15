// Router hooks for React-model components: modules that run on Preact compat
// or on real React (a "use client" module under a server component, an island
// that uses an npm React library). They read hook results as plain values, so
// each hook returns a value and the component re-renders when the URL changes.
// Krate-compiled components use the getter-returning hooks of navigation.ts.
//
// The compiler's island builds replace the router hooks of `@krate/runtime`
// with createValueNavigationHooks(useSyncExternalStore) of their React.

import { getRequestContext } from './request-context.js';
import * as serverNav from './navigation-server.js';
import { krateRouter } from './router.js';
import { krateRouteStore, krateMatchParams, krateLayoutSegments } from './navigation.js';
import type { AppRouterInstance } from './navigation.js';

type ParamValue = string | string[];
type UseSyncExternalStore = <T>(subscribe: (cb: () => void) => () => void, getSnapshot: () => T, getServerSnapshot?: () => T) => T;

export interface ValueNavigationHooks {
  usePathname(): string;
  useSearchParams(): URLSearchParams;
  useParams<T extends Record<string, ParamValue> = Record<string, ParamValue>>(): T;
  useRouter(): AppRouterInstance;
  useSelectedLayoutSegments(parallelRouteKey?: string, depth?: number): string[];
  useSelectedLayoutSegment(parallelRouteKey?: string, depth?: number): string | null;
}

const routerInstance: AppRouterInstance = {
  push: (href, options) => void krateRouter.push(href, options),
  replace: (href, options) => void krateRouter.replace(href, options),
  back: () => krateRouter.back(),
  forward: () => krateRouter.forward(),
  refresh: () => void krateRouter.refresh(),
  prefetch: (href) => void krateRouter.prefetch(href),
};

export function createValueNavigationHooks(useSyncExternalStore: UseSyncExternalStore): ValueNavigationHooks {
  const store = krateRouteStore();
  const route = () => useSyncExternalStore(store.subscribe, store.getSnapshot, store.getSnapshot);
  // Values derived from the snapshot are cached so they keep their identity
  // between renders of the same URL, as React's hooks do.
  let paramsKey = '';
  let paramsVal: URLSearchParams = new URLSearchParams();
  let routeKey = '';
  let routeVal: Record<string, ParamValue> = {};
  let segKey = '';
  let segVal: string[] = [];

  const segments = (depth?: number): string[] => {
    const s = route();
    const key = s.pathname + '\0' + (depth ?? '');
    if (key !== segKey) {
      segKey = key;
      segVal = krateLayoutSegments(s.pathname, depth);
    }
    return segVal;
  };

  return {
    usePathname() {
      if (getRequestContext()) return serverNav.usePathname();
      return route().pathname;
    },
    useSearchParams() {
      if (getRequestContext()) return serverNav.useSearchParams();
      const s = route();
      if (s.search !== paramsKey) {
        paramsKey = s.search;
        paramsVal = new URLSearchParams(s.search);
      }
      return paramsVal;
    },
    useParams<T extends Record<string, ParamValue> = Record<string, ParamValue>>(): T {
      if (getRequestContext()) return serverNav.useParams<T>();
      const s = route();
      const key = s.route + '\0' + s.pathname;
      if (key !== routeKey) {
        routeKey = key;
        routeVal = krateMatchParams(s.route, s.pathname);
      }
      return routeVal as T;
    },
    useRouter() {
      if (getRequestContext()) return serverNav.useRouter();
      return routerInstance;
    },
    useSelectedLayoutSegments(parallelRouteKey, depth) {
      if (getRequestContext()) return serverNav.useSelectedLayoutSegments(parallelRouteKey, depth);
      return segments(depth);
    },
    useSelectedLayoutSegment(parallelRouteKey, depth) {
      if (getRequestContext()) return serverNav.useSelectedLayoutSegment(parallelRouteKey, depth);
      const at = typeof depth === 'number' ? depth : 0;
      const parts = krateLayoutSegments(route().pathname);
      return parts.length > at ? parts[at] : null;
    },
  };
}
