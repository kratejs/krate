// Router API for sites with the SPA router disabled (`spa: false`). Every
// navigation is a full document load; the API surface matches router.ts so
// application code (and the Next.js migration shim) works unchanged.

export interface NavigateOptions {
  replace?: boolean;
  scroll?: boolean;
  state?: unknown;
}

export interface NavigationState {
  url: string;
  pathname: string;
  search: string;
  hash: string;
  status: 'idle' | 'loading';
}

function basePath(): string {
  const cfg = (globalThis as any).__KRATE_CFG__ as { basePath?: string } | undefined;
  const bp = (cfg && cfg.basePath) || '';
  if (!bp || bp === '/') return '';
  return '/' + bp.replace(/^\/+|\/+$/g, '');
}

export function getNavigationState(): NavigationState {
  if (typeof location === 'undefined') return { url: '', pathname: '/', search: '', hash: '', status: 'idle' };
  const bp = basePath();
  let pathname = location.pathname;
  if (bp && (pathname === bp || pathname.startsWith(bp + '/'))) pathname = pathname.slice(bp.length) || '/';
  return { url: location.href, pathname, search: location.search, hash: location.hash, status: 'idle' };
}

/** Without the SPA router the URL only changes in-document on hash changes. */
export function subscribeNavigation(fn: (s: NavigationState) => void): () => void {
  if (typeof window === 'undefined') return () => {};
  const handler = () => fn(getNavigationState());
  window.addEventListener('hashchange', handler);
  return () => {
    window.removeEventListener('hashchange', handler);
  };
}

export function getHistoryState(): unknown {
  return typeof history === 'undefined' ? null : history.state;
}

/** No prefetch cache exists without the SPA router. */
export function invalidatePrefetchCache(_url?: string): void {}

export function initRouter(): void {}

export function reinitRouter(): void {}

export function destroyRouter(): void {}

export function navigate(url: string, opts: NavigateOptions = {}): boolean {
  if (typeof location === 'undefined') return true;
  if (opts.replace) location.replace(url);
  else location.assign(url);
  return true;
}

export const krateRouter = {
  push(url: string, opts: NavigateOptions = {}): Promise<void> {
    navigate(url, { ...opts, replace: false });
    return Promise.resolve();
  },
  replace(url: string, opts: NavigateOptions = {}): Promise<void> {
    navigate(url, { ...opts, replace: true });
    return Promise.resolve();
  },
  back(): void {
    if (typeof history !== 'undefined') history.back();
  },
  forward(): void {
    if (typeof history !== 'undefined') history.forward();
  },
  refresh(): Promise<void> {
    if (typeof location !== 'undefined') location.reload();
    return Promise.resolve();
  },
  // Full-page navigations go through the browser's HTTP cache; there is no
  // client document cache to warm.
  prefetch(_url: string): Promise<void> {
    return Promise.resolve();
  },
  subscribe: subscribeNavigation,
  getState: getNavigationState,
  invalidate: invalidatePrefetchCache,
};

if (typeof window !== 'undefined') {
  // Revalidated server data is only visible after a reload without the SPA router.
  window.addEventListener('krate:revalidate', () => {
    if (typeof location !== 'undefined') location.reload();
  });
}
