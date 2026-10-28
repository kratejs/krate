// Krate SPA router.
//
// Intercepts clicks on compiled <Link> anchors (a[data-krate-link]) and GET
// form submissions, fetches the target document, and swaps the app shell in
// place. All router state lives at module scope so `reinitRouter()` (called
// by every page's hydration script) only refreshes per-page bindings and never
// loses the prefetch cache, saved scroll positions or the in-flight navigation.

/**
 * The shared runtime chunk is loaded once and must persist across SPA
 * navigations. Page-specific hydration scripts, in contrast, must be
 * re-executed on every visit (against the freshly swapped DOM), so they are
 * removed before loading a new page and re-appended.
 */
const RUNTIME_CHUNK_RE = /\/chunks\/runtime\.[^/]+\.js$/;

/** Site config injected by the compiler (`window.__KRATE_CFG__`). */
interface KrateRuntimeConfig {
  basePath?: string;
  viewTransitions?: boolean | 'auto';
  /** Milliseconds a prefetched document stays fresh (default 30000). */
  prefetchTTL?: number;
  /** Set by `krate dev`: enables router dev warnings. */
  dev?: boolean;
}

function runtimeConfig(): KrateRuntimeConfig {
  return ((globalThis as any).__KRATE_CFG__ as KrateRuntimeConfig) || {};
}

/** Normalizes the configured base path to "" or "/prefix" (no trailing slash). */
function basePath(): string {
  const bp = runtimeConfig().basePath || '';
  if (!bp || bp === '/') return '';
  return '/' + bp.replace(/^\/+|\/+$/g, '');
}

/** Strips the base path from a pathname so route comparisons are consistent. */
function stripBase(pathname: string): string {
  const bp = basePath();
  if (bp && (pathname === bp || pathname.startsWith(bp + '/'))) {
    const rest = pathname.slice(bp.length);
    return rest === '' ? '/' : rest;
  }
  return pathname;
}

/** Compares two pathnames ignoring a trailing slash ("/a/" equals "/a"). */
function normPath(p: string): string {
  return p.length > 1 && p.endsWith('/') ? p.slice(0, -1) : p;
}

function devWarn(msg: string): void {
  if (!runtimeConfig().dev) return;
  try {
    console.warn('[krate router] ' + msg);
  } catch {
    /* ignore */
  }
}

// Resolve an asset URL against the navigated-to page (not the current one), so
// relative srcs like "index.a1b2c3.js" load from the new page's directory.
function resolveSrc(src: string, baseUrl: string): string {
  if (/^(https?:)?\/\//.test(src) || src.startsWith('/')) {
    return new URL(src, baseUrl).href;
  }
  // Relative path: resolve against the base page's directory, accounting for
  // both "/about" and "/about/" style targets.
  let base = baseUrl;
  if (!base.endsWith('/')) {
    const idx = base.lastIndexOf('/');
    base = idx > 0 ? base.slice(0, idx + 1) : '/';
  }
  return new URL(src, base).href;
}

/**
 * The per-page hydration chunk(s) a prefetched document will need, as absolute
 * URLs, excluding the shared runtime chunk and anything already loaded. Prefetching
 * these warms the route's JS in addition to its HTML, so an SPA navigation does
 * not stall waiting on the chunk after the HTML swap.
 */
export function pageAssetURLs(html: string, baseUrl: string, loaded: Set<string>): string[] {
  const doc = new DOMParser().parseFromString(html, 'text/html');
  const out: string[] = [];
  const seen = new Set<string>();
  const consider = (src: string | null) => {
    if (!src) return;
    let abs: string;
    try {
      abs = resolveSrc(src, baseUrl);
    } catch {
      return;
    }
    if (seen.has(abs) || loaded.has(abs) || RUNTIME_CHUNK_RE.test(abs)) return;
    seen.add(abs);
    out.push(abs);
  };
  doc.querySelectorAll('script[src]').forEach((s) => consider(s.getAttribute('src')));
  doc
    .querySelectorAll('link[rel="modulepreload"][href], link[rel="preload"][as="script"][href]')
    .forEach((l) => consider(l.getAttribute('href')));
  return out;
}

export interface NavigateOptions {
  /** Replace the current history entry instead of pushing a new one. */
  replace?: boolean;
  /** Scroll to the top (or the URL hash) after navigating (default true). */
  scroll?: boolean;
  /** State stored on the new history entry (read it with getHistoryState()). */
  state?: unknown;
}

/** Snapshot of the router state delivered to subscribeNavigation listeners. */
export interface NavigationState {
  /** Absolute URL of the rendered route. */
  url: string;
  /** Pathname without the configured base path. */
  pathname: string;
  search: string;
  hash: string;
  /** 'loading' while a navigation is in flight, 'idle' otherwise. */
  status: 'idle' | 'loading';
}

type NavKind = 'push' | 'replace' | 'pop' | 'refresh';

interface NavRequest {
  url: string;
  kind: NavKind;
  scroll: boolean;
  state?: unknown;
  /** History key of the entry being restored (pop). */
  restoreKey?: string;
  /** Skip the prefetch cache (refresh / revalidate). */
  bypassCache?: boolean;
}

interface CachedDoc {
  html: string;
  /** Final URL after redirects. */
  url: string;
  status: number;
  at: number;
}

// Router state (module scope, survives reinitRouter).
let initialized = false;
let navSeq = 0;
let pendingAbort: AbortController | undefined;
let keySeq = 0;
let currentKey = '';
/** pathname+search of the document currently rendered in #root. */
let renderedPath = '';
let status: 'idle' | 'loading' = 'idle';
const listeners = new Set<(s: NavigationState) => void>();

/** Prefetched documents (LRU with TTL; an entry is consumed by the navigation that uses it). */
const prefetchCache = new Map<string, CachedDoc>();
const prefetchInflight = new Map<string, Promise<void>>();
const PREFETCH_CACHE_MAX = 30;
const DEFAULT_PREFETCH_TTL = 30000;

/** Saved scroll positions keyed by history entry key (bounded). */
const scrollPositions = new Map<string, number>();
const SCROLL_HISTORY_MAX = 50;

/** Bounded set of asset URLs already emitted as `<link rel="prefetch">`. */
const prefetchedAssets = new Set<string>();
const PREFETCHED_ASSETS_MAX = 128;

const errorPageCache = new Map<string, string | null>();

let prefetchTimers = new Map<Element, ReturnType<typeof setTimeout>>();
let prefetchObserver: IntersectionObserver | null = null;
let loadingOverlay: Node[] = [];

/** Output prefix of intercepting-route fragments (see the compiler's parallel.go). */
const INTERCEPT_PREFIX = '_krate/i';
const TRANSITION_MS = 150;
const PREFETCH_DELAY = 200;

function now(): number {
  return typeof performance !== 'undefined' && performance.now ? performance.now() : Date.now();
}

function newKey(): string {
  keySeq++;
  return Date.now().toString(36) + '-' + keySeq.toString(36);
}

function cacheKey(url: string): string {
  try {
    const u = new URL(url, location.href);
    u.hash = '';
    return u.href;
  } catch {
    return url;
  }
}

function pathOf(url: string): string {
  const u = new URL(url, location.href);
  return normPath(u.pathname) + u.search;
}

function hardNavigate(url: string, replace = false): void {
  const hook = (globalThis as any).__krateHardNavigate;
  if (typeof hook === 'function') {
    hook(url, replace);
    return;
  }
  if (replace) location.replace(url);
  else location.assign(url);
}

function historyEntry(key: string, state: unknown): Record<string, unknown> {
  return { __krate: key, state: state === undefined ? null : state };
}

function entryKey(state: unknown): string | undefined {
  if (state && typeof state === 'object' && typeof (state as any).__krate === 'string') {
    return (state as any).__krate;
  }
  return undefined;
}

/** The `state` passed to the navigation that created the current history entry. */
export function getHistoryState(): unknown {
  if (typeof history === 'undefined') return null;
  const s = history.state;
  if (entryKey(s) !== undefined) return (s as any).state;
  return s;
}

function snapshot(): NavigationState {
  const u = new URL(location.href);
  return { url: u.href, pathname: stripBase(u.pathname), search: u.search, hash: u.hash, status };
}

/** Current router state (pathname without base path, search, hash, status). */
export function getNavigationState(): NavigationState {
  if (typeof location === 'undefined') {
    return { url: '', pathname: '/', search: '', hash: '', status: 'idle' };
  }
  return snapshot();
}

function emit(): void {
  if (listeners.size === 0) return;
  const s = snapshot();
  for (const fn of Array.from(listeners)) {
    try {
      fn(s);
    } catch (err) {
      console.error(err);
    }
  }
}

function setStatus(next: 'idle' | 'loading'): void {
  if (status === next) return;
  status = next;
  emit();
}

/**
 * Subscribes to navigation state changes (URL changes from SPA navigations,
 * back/forward, hash changes, and loading status). Returns an unsubscribe
 * function. The listener is not called immediately; read the current value
 * with getNavigationState().
 */
export function subscribeNavigation(fn: (s: NavigationState) => void): () => void {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

function dispatch(name: string, detail: Record<string, unknown>, cancelable = false): boolean {
  try {
    const ev = new CustomEvent(name, { detail, cancelable });
    window.dispatchEvent(ev);
    return !ev.defaultPrevented;
  } catch {
    return true;
  }
}

// Analytics hook: fires once after every successful SPA navigation. A throw
// inside the hook is ignored.
function notifyNavigation(url: string): void {
  try {
    const hook = (globalThis as any).__krate_onNavigation;
    if (typeof hook === 'function') hook(url);
  } catch {
    /* ignore hook failures */
  }
}

function saveScroll(key: string): void {
  if (!key) return;
  scrollPositions.delete(key);
  if (scrollPositions.size >= SCROLL_HISTORY_MAX) {
    const oldest = scrollPositions.keys().next().value;
    if (oldest !== undefined) scrollPositions.delete(oldest);
  }
  scrollPositions.set(key, window.scrollY || 0);
}

// Prefetch cache

function prefetchTTL(): number {
  const ttl = runtimeConfig().prefetchTTL;
  return typeof ttl === 'number' && ttl >= 0 ? ttl : DEFAULT_PREFETCH_TTL;
}

function cachePut(key: string, doc: CachedDoc): void {
  prefetchCache.delete(key);
  while (prefetchCache.size >= PREFETCH_CACHE_MAX) {
    const oldest = prefetchCache.keys().next().value;
    if (oldest === undefined) break;
    prefetchCache.delete(oldest);
  }
  prefetchCache.set(key, doc);
}

/** Removes and returns a fresh cached document (hits are consumed). */
function cacheTake(key: string): CachedDoc | undefined {
  const doc = prefetchCache.get(key);
  if (!doc) return undefined;
  prefetchCache.delete(key);
  if (now() - doc.at > prefetchTTL()) return undefined;
  return doc;
}

/**
 * Drops prefetched documents: one URL, or every entry when called without an
 * argument. Server actions that revalidate data call this through the
 * `krate:revalidate` event.
 */
export function invalidatePrefetchCache(url?: string): void {
  if (url === undefined) {
    prefetchCache.clear();
    return;
  }
  prefetchCache.delete(cacheKey(url));
}

function isHTMLType(ct: string): boolean {
  return /text\/html|application\/xhtml\+xml/i.test(ct);
}

function looksLikeHTML(body: string): boolean {
  return /^\s*(<!doctype html|<html|<head|<body)/i.test(body);
}

function prefetchAsset(url: string): void {
  if (prefetchedAssets.has(url) || prefetchedAssets.size >= PREFETCHED_ASSETS_MAX) return;
  prefetchedAssets.add(url);
  const link = document.createElement('link');
  link.rel = 'prefetch';
  link.as = 'script';
  link.href = url;
  document.head.appendChild(link);
}

function prefetchPage(url: string): Promise<void> {
  const key = cacheKey(url);
  const cached = prefetchCache.get(key);
  if (cached && now() - cached.at <= prefetchTTL()) return Promise.resolve();
  const inflight = prefetchInflight.get(key);
  if (inflight) return inflight;
  const p = fetch(key, { headers: { Accept: 'text/html' }, credentials: 'same-origin' })
    .then(async (res) => {
      // Only cache successful, same-origin HTML documents.
      if (!res.ok) return;
      const ct = res.headers.get('content-type') || '';
      const finalUrl = res.url || key;
      if (new URL(finalUrl, location.href).origin !== location.origin) return;
      const html = await res.text();
      if (!(isHTMLType(ct) || (ct === '' && looksLikeHTML(html)))) return;
      cachePut(key, { html, url: finalUrl, status: res.status, at: now() });
      const loaded = new Set<string>();
      document.querySelectorAll('script[src]').forEach((s) => loaded.add((s as HTMLScriptElement).src));
      for (const asset of pageAssetURLs(html, finalUrl, loaded)) prefetchAsset(asset);
    })
    .catch(() => {
      // A failed prefetch is retried by the real navigation, which reports
      // errors through krate:navigate-error.
    })
    .finally(() => {
      prefetchInflight.delete(key);
    });
  prefetchInflight.set(key, p);
  return p;
}

// Link helpers

function isLocalLink(a: HTMLAnchorElement): boolean {
  if (a.hasAttribute('data-krate-external') || a.hasAttribute('download')) return false;
  const href = a.getAttribute('href');
  if (!href || href.startsWith('javascript:')) return false;
  let u: URL;
  try {
    u = new URL(href, location.href);
  } catch {
    return false;
  }
  if (u.origin !== location.origin) return false;
  const target = a.getAttribute('target');
  if (target && target !== '_self') return false;
  return true;
}

// Prefetch only links that opted in and did not opt out (prefetch={false}).
function shouldPrefetch(a: HTMLAnchorElement): boolean {
  const v = a.getAttribute('data-prefetch');
  return a.hasAttribute('data-prefetch') && v !== 'false' && v !== '0';
}

// Above-the-fold/viewport prefetch is deferred to idle time so it never
// competes with first paint; hover/focus keeps the 200ms intent debounce.
function idle(fn: () => void): void {
  const ric = (window as any).requestIdleCallback;
  if (typeof ric === 'function') ric(fn, { timeout: 1000 });
  else setTimeout(fn, 50);
}

function prefetchHoverHandler(e: Event): void {
  const t = e.target as Element | null;
  if (!t || typeof (t as Element).closest !== 'function') return;
  const a = t.closest('a[data-prefetch]') as HTMLAnchorElement | null;
  if (!a || !shouldPrefetch(a) || !isLocalLink(a)) return;
  const href = a.getAttribute('href');
  if (!href) return;
  const url = new URL(href, location.href).href;
  if (pathOf(url) === renderedPath) return;
  const existing = prefetchTimers.get(a);
  if (existing) clearTimeout(existing);
  const timer = setTimeout(() => {
    prefetchTimers.delete(a);
    void prefetchPage(url);
  }, PREFETCH_DELAY);
  prefetchTimers.set(a, timer);
}

function observePrefetchLinks(): void {
  if (!prefetchObserver) return;
  document.querySelectorAll('a[data-prefetch]').forEach((a) => {
    if (shouldPrefetch(a as HTMLAnchorElement)) prefetchObserver!.observe(a);
  });
}

function setupPrefetch(): void {
  document.addEventListener('mouseover', prefetchHoverHandler);
  document.addEventListener('focusin', prefetchHoverHandler);
  if (typeof IntersectionObserver !== 'undefined') {
    prefetchObserver = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (!entry.isIntersecting) continue;
          const a = entry.target as HTMLAnchorElement;
          const href = a.getAttribute('href');
          if (href && isLocalLink(a) && shouldPrefetch(a)) {
            const url = new URL(href, location.href).href;
            if (pathOf(url) !== renderedPath) idle(() => void prefetchPage(url));
          }
          prefetchObserver!.unobserve(entry.target);
        }
      },
      { rootMargin: '200px' }
    );
    observePrefetchLinks();
  }
}

/**
 * Inject Speculation Rules so the browser prefetches same-origin links ahead
 * of hover, where supported. Falls back to the JS prefetch path elsewhere.
 */
function setupSpeculationRules(): void {
  const supports = typeof HTMLScriptElement !== 'undefined' ? (HTMLScriptElement as any).supports : undefined;
  if (typeof supports !== 'function' || !supports('speculationrules')) return;
  try {
    const script = document.createElement('script');
    script.type = 'speculationrules';
    script.textContent = JSON.stringify({
      prefetch: [
        {
          where: {
            and: [
              { href_matches: '/*' },
              { not: { href_matches: '/__krate/*' } },
              { not: { href_matches: '/api/*' } },
            ],
          },
          eagerness: 'moderate',
        },
      ],
    });
    document.head.appendChild(script);
  } catch {
    /* ignore */
  }
}

// Visual transition

/** Whether native View Transitions should wrap the SPA mutation. */
function supportsViewTransitions(): boolean {
  if (runtimeConfig().viewTransitions === false) return false;
  if (typeof window !== 'undefined' && window.matchMedia) {
    try {
      if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return false;
    } catch {
      /* ignore */
    }
  }
  return typeof (document as any).startViewTransition === 'function';
}

function appRoot(doc: Document): HTMLElement {
  return (doc.getElementById('root') || doc.body) as HTMLElement;
}

// While a fetch is in flight: overlay the loading.tsx template (if the page
// has one) and, without View Transitions, fade the content out.
function beginTransition(): void {
  const tpl = document.querySelector('template[data-krate-loading]') as HTMLTemplateElement | null;
  if (tpl && tpl.content && loadingOverlay.length === 0) {
    // Non-destructive loading UI: overlay the template without wiping the
    // live DOM, so the swap still sees the old tree.
    const frag = tpl.content.cloneNode(true) as DocumentFragment;
    loadingOverlay = Array.from(frag.childNodes);
    document.body.appendChild(frag);
  }
  if (!supportsViewTransitions()) {
    const el = appRoot(document);
    el.style.transition = `opacity ${TRANSITION_MS}ms ease`;
    el.style.opacity = '0';
  }
}

function endTransition(): void {
  for (const n of loadingOverlay) {
    if (n.parentNode) n.parentNode.removeChild(n);
  }
  loadingOverlay = [];
  const el = appRoot(document);
  if (el.style.opacity === '0') el.style.opacity = '';
}

/**
 * Apply `data-view-transition="name"` to `view-transition-name`. Persistent
 * chrome (navbar/sidebar) keeps its name across mutations so it morphs
 * instead of cross-fading.
 */
function applyViewTransitionNames(root: ParentNode): void {
  root.querySelectorAll('[data-view-transition]').forEach((el) => {
    const name = (el as HTMLElement).getAttribute('data-view-transition');
    if (name) (el as HTMLElement).style.viewTransitionName = name;
  });
}

// Document reconciliation

/** Diff stylesheet <link>s by absolute URL (base-path and format safe). */
function reconcileStylesheets(doc: Document, baseUrl: string): void {
  const newStyles = doc.querySelectorAll('link[rel="stylesheet"]');
  const newHrefs = new Set<string>();
  newStyles.forEach((s) => {
    const href = s.getAttribute('href');
    if (href) newHrefs.add(new URL(href, baseUrl).href);
  });
  document.querySelectorAll('link[rel="stylesheet"]').forEach((l) => {
    const abs = (l as HTMLLinkElement).href;
    if (abs && !newHrefs.has(abs)) l.remove();
  });
  const existing = new Set<string>();
  document.querySelectorAll('link[rel="stylesheet"]').forEach((l) => existing.add((l as HTMLLinkElement).href));
  newStyles.forEach((s) => {
    const href = s.getAttribute('href');
    if (!href) return;
    const abs = new URL(href, baseUrl).href;
    if (!existing.has(abs)) {
      const link = document.createElement('link');
      link.rel = 'stylesheet';
      link.href = abs;
      document.head.appendChild(link);
    }
  });
}

// Document-level <head> tags that change per page and must follow a swap.
const HEAD_SYNC_SEL =
  'meta[name="description"], meta[name="robots"], meta[property^="og:"], meta[name^="twitter:"], link[rel="canonical"], script[type="application/ld+json"]';

function reconcileHeadMeta(doc: Document): void {
  document.head.querySelectorAll(HEAD_SYNC_SEL).forEach((el) => el.remove());
  doc.head.querySelectorAll(HEAD_SYNC_SEL).forEach((el) => {
    document.head.appendChild(document.importNode(el, true));
  });
}

function scriptsToLoad(doc: Document, baseUrl: string): string[] {
  const keep = new Set<string>();
  document.querySelectorAll('script[src]:not([data-krate-spa])').forEach((s) => keep.add((s as HTMLScriptElement).src));
  const out: string[] = [];
  doc.querySelectorAll('script[src]').forEach((s) => {
    const src = s.getAttribute('src');
    if (!src) return;
    const t = (s.getAttribute('type') || '').toLowerCase();
    if (t && t !== 'module' && t !== 'text/javascript' && t !== 'application/javascript') return;
    const abs = resolveSrc(src, baseUrl);
    if (!keep.has(abs) && !out.includes(abs)) out.push(abs);
  });
  return out;
}

function loadScript(src: string): Promise<boolean> {
  return new Promise((resolve) => {
    const s = document.createElement('script');
    s.src = src;
    // Only page scripts are disposable across navigations; the shared runtime
    // chunk stays in place.
    if (!RUNTIME_CHUNK_RE.test(src)) s.setAttribute('data-krate-spa', '');
    s.onload = () => resolve(true);
    s.onerror = () => resolve(false);
    document.body.appendChild(s);
  });
}

// Scroll, focus, announcements

function hashTarget(hash: string): Element | null {
  if (!hash || hash === '#') return null;
  let id = hash.slice(1);
  try {
    id = decodeURIComponent(id);
  } catch {
    /* keep raw */
  }
  return document.getElementById(id) || document.getElementsByName(id)[0] || null;
}

function scrollToHash(hash: string): boolean {
  const el = hashTarget(hash);
  if (!el) return false;
  try {
    el.scrollIntoView({ block: 'start' });
  } catch {
    /* ignore */
  }
  return true;
}

function applyScroll(url: string, req: NavRequest): void {
  if (req.kind === 'pop') {
    const saved = req.restoreKey ? scrollPositions.get(req.restoreKey) : undefined;
    if (saved !== undefined) {
      window.scrollTo(0, saved);
      return;
    }
  }
  const hash = new URL(url, location.href).hash;
  if (scrollToHash(hash)) return;
  if (req.scroll && req.kind !== 'refresh') window.scrollTo(0, 0);
}

let focusedRoot: HTMLElement | null = null;

/** Move focus to the content root after navigation (a11y). */
function focusContent(): void {
  const active = document.activeElement as HTMLElement | null;
  // Focus inside a persisted region (e.g. a search box in a layout) stays put.
  if (active && active !== document.body && active.isConnected && active !== focusedRoot) return;
  const root = appRoot(document);
  if (!root.hasAttribute('tabindex')) {
    root.setAttribute('tabindex', '-1');
    focusedRoot = root;
    root.addEventListener(
      'blur',
      () => {
        if (focusedRoot === root) {
          root.removeAttribute('tabindex');
          focusedRoot = null;
        }
      },
      { once: true }
    );
  }
  try {
    root.focus({ preventScroll: true });
  } catch {
    /* ignore */
  }
}

const ANNOUNCER_ID = '__krate-route-announcer';

function announce(): void {
  let el = document.getElementById(ANNOUNCER_ID);
  if (!el) {
    el = document.createElement('div');
    el.id = ANNOUNCER_ID;
    el.setAttribute('aria-live', 'assertive');
    el.setAttribute('aria-atomic', 'true');
    el.setAttribute('role', 'status');
    el.setAttribute(
      'style',
      'position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0'
    );
    document.body.appendChild(el);
  }
  const h1 = appRoot(document).querySelector('h1');
  const text = document.title || (h1 && h1.textContent ? h1.textContent.trim() : '') || stripBase(location.pathname);
  el.textContent = text;
}

/**
 * Marks links matching the current route. An exact match gets
 * aria-current="page" and data-krate-active="exact"; a link to an ancestor
 * segment ("/docs" while on "/docs/intro", but never "/docs-old") gets
 * data-krate-active="segment" so navigation sections can be styled.
 */
function updateActiveLinks(): void {
  const path = normPath(stripBase(location.pathname));
  document.querySelectorAll('a[data-krate-link]').forEach((a) => {
    const href = a.getAttribute('href');
    let target = '';
    try {
      target = href ? normPath(stripBase(new URL(href, location.href).pathname)) : '';
    } catch {
      target = '';
    }
    let active = '';
    if (target && target === path) active = 'exact';
    else if (target && target !== '/' && path.startsWith(target + '/')) active = 'segment';
    if (active === 'exact') a.setAttribute('aria-current', 'page');
    else a.removeAttribute('aria-current');
    if (active) a.setAttribute('data-krate-active', active);
    else a.removeAttribute('data-krate-active');
  });
}

// Navigation

interface LoadedDoc {
  html: string;
  url: string;
  status: number;
}

type LoadResult = { ok: true; doc: LoadedDoc } | { ok: false; hard: string } | { ok: false; aborted: true };

function fetchErrorPage(path: string): Promise<string | null> {
  const url = basePath() + path;
  if (errorPageCache.has(url)) return Promise.resolve(errorPageCache.get(url) ?? null);
  return fetch(url, { headers: { Accept: 'text/html' }, credentials: 'same-origin' })
    .then(async (res) => {
      const html = res.ok ? await res.text() : null;
      const out = html && looksLikeHTML(html) ? html : null;
      errorPageCache.set(url, out);
      return out;
    })
    .catch(() => null);
}

async function loadDocument(req: NavRequest, signal: AbortSignal): Promise<LoadResult> {
  const key = cacheKey(req.url);
  if (!req.bypassCache) {
    const inflight = prefetchInflight.get(key);
    if (inflight) await inflight;
    if (signal.aborted) return { ok: false, aborted: true };
    const hit = cacheTake(key);
    if (hit) return { ok: true, doc: { html: hit.html, url: hit.url, status: hit.status } };
  }
  let res: Response;
  try {
    res = await fetch(key, {
      headers: { Accept: 'text/html' },
      credentials: 'same-origin',
      signal,
      cache: req.bypassCache ? 'no-cache' : 'default',
    });
  } catch (err) {
    if ((err as any)?.name === 'AbortError' || signal.aborted) return { ok: false, aborted: true };
    // Network failure: apps can cancel the hard-reload fallback (e.g. to show
    // an offline banner) by calling preventDefault on the event.
    const proceed = dispatch('krate:navigate-error', { url: req.url, status: 0, error: err }, true);
    if (!proceed) return { ok: false, aborted: true };
    return { ok: false, hard: req.url };
  }
  const finalUrl = res.url || key;
  if (new URL(finalUrl, location.href).origin !== location.origin) {
    return { ok: false, hard: finalUrl };
  }
  const ct = res.headers.get('content-type') || '';
  let body = '';
  try {
    body = await res.text();
  } catch (err) {
    if ((err as any)?.name === 'AbortError' || signal.aborted) return { ok: false, aborted: true };
    body = '';
  }
  if (signal.aborted) return { ok: false, aborted: true };
  const html = isHTMLType(ct) || (ct === '' && looksLikeHTML(body));
  // Preserve the hash of the requested URL across redirects.
  const hash = new URL(req.url, location.href).hash;
  const target = hash && !new URL(finalUrl).hash ? finalUrl + hash : finalUrl;
  if (res.ok) {
    // A non-HTML response (file download, JSON, PDF) is not a page: let the
    // browser handle it.
    if (!html) return { ok: false, hard: target };
    return { ok: true, doc: { html: body, url: target, status: res.status } };
  }
  dispatch('krate:navigate-error', { url: req.url, status: res.status });
  // The server rendered an error document for this URL: show it in place.
  if (html && looksLikeHTML(body)) return { ok: true, doc: { html: body, url: target, status: res.status } };
  let fallback: string | null = null;
  if (res.status === 404 || res.status === 410) fallback = await fetchErrorPage('/404.html');
  else if (res.status >= 500) fallback = await fetchErrorPage('/500.html');
  if (signal.aborted) return { ok: false, aborted: true };
  if (fallback) return { ok: true, doc: { html: fallback, url: target, status: res.status } };
  // Other statuses (401, 403, 429, ...) without an HTML body: the browser
  // shows the real response.
  return { ok: false, hard: target };
}

async function runNavigation(req: NavRequest): Promise<void> {
  const from = location.href;
  if (req.kind !== 'pop') {
    const proceed = dispatch('krate:navigate-start', { url: req.url, from, kind: req.kind }, true);
    if (!proceed) return;
  } else {
    dispatch('krate:navigate-start', { url: req.url, from, kind: req.kind });
  }

  const seq = ++navSeq;
  if (pendingAbort) pendingAbort.abort();
  const ctrl = new AbortController();
  pendingAbort = ctrl;
  setStatus('loading');
  const hasCached = !req.bypassCache && prefetchCache.has(cacheKey(req.url));
  if (!hasCached) beginTransition();

  let result: LoadResult;
  try {
    result = await loadDocument(req, ctrl.signal);
  } finally {
    if (pendingAbort === ctrl) pendingAbort = undefined;
  }
  if (seq !== navSeq) return; // superseded by a newer navigation
  if (!result.ok) {
    endTransition();
    setStatus('idle');
    if ('hard' in result) hardNavigate(result.hard, req.kind === 'replace' || req.kind === 'refresh');
    return;
  }
  await commitDocument(result.doc, req, seq);
}

async function commitDocument(loaded: LoadedDoc, req: NavRequest, seq: number): Promise<void> {
  const doc = new DOMParser().parseFromString(loaded.html, 'text/html');
  const url = loaded.url;
  const newRoot = appRoot(doc);
  const oldRoot = appRoot(document);

  // Save the position of the entry we are leaving (pop saves in the handler).
  if (req.kind === 'push') saveScroll(currentKey);

  const newChain = readRouteChain(doc);
  const plan = planSwap(currentChain, newChain, location.href, url, doc);

  const mutate = () => {
    const title = doc.querySelector('title');
    if (title) document.title = title.textContent || '';
    const g = globalThis as any;
    if (plan.keep.length > 0 && typeof g.krateDisposeExcept === 'function') g.krateDisposeExcept(plan.keep);
    else if (typeof g.disposeAll === 'function') g.disposeAll();
    if (plan.region) {
      // Only the deepest boundary that differs is replaced; the kept layouts'
      // DOM (and its state, focus and scroll containers) stays in place.
      const { live, next } = plan.region;
      clearBetween(live[0], live[1]);
      const nodes = between(next[0], next[1]).map((n) => document.importNode(n, true));
      for (const n of nodes) live[1].parentNode!.insertBefore(n, live[1]);
      for (const sl of plan.slots) {
        clearBetween(sl.live[0], sl.live[1]);
        for (const n of between(sl.next[0], sl.next[1])) sl.live[1].parentNode!.insertBefore(document.importNode(n, true), sl.live[1]);
      }
      syncLoadingTemplate(oldRoot, newRoot);
    } else {
      const nodes = Array.from(newRoot.childNodes).map((n) => document.importNode(n, true));
      oldRoot.replaceChildren(...nodes);
    }
    currentChain = plan.chain;
    // Detached slot content of earlier intercepts belonged to the old page.
    interceptSnapshots.clear();
    reconcileStylesheets(doc, url);
    reconcileHeadMeta(doc);
    applyViewTransitionNames(document);
  };

  await runMutation(mutate);
  if (seq !== navSeq) return;
  endTransition();

  // History: the DOM now shows the new route, so the URL follows it before
  // any page script runs.
  if (req.kind === 'push') {
    currentKey = newKey();
    history.pushState(historyEntry(currentKey, req.state), '', url);
  } else if (req.kind === 'replace') {
    scrollPositions.delete(currentKey);
    currentKey = newKey();
    history.replaceState(historyEntry(currentKey, req.state), '', url);
  } else if (req.kind === 'refresh') {
    if (url !== location.href) history.replaceState(history.state, '', url);
  } else if (req.restoreKey) {
    currentKey = req.restoreKey;
  }
  renderedPath = pathOf(location.href);
  docPath = renderedPath;
  syncParams(doc);

  applyScroll(url, req);
  if (req.kind !== 'pop' && req.kind !== 'refresh') focusContent();
  if (req.kind !== 'refresh') announce();
  updateActiveLinks();
  dispatch('krate:navigate', { url: location.href, status: loaded.status });
  notifyNavigation(location.href);
  setStatus('idle');

  // Page scripts re-run against the new DOM; the hydration code of kept
  // layouts is skipped (krateEnterScope) because their bindings are live.
  document.querySelectorAll('script[data-krate-spa]').forEach((s) => s.remove());
  const srcs = scriptsToLoad(doc, url);
  keepScopes(plan.skip);
  const results = await Promise.all(srcs.map(loadScript));
  keepScopes([]);
  if (seq !== navSeq) return;
  if (results.some((ok) => !ok)) {
    // A page chunk failed to load: the route would stay half-mounted, so do a
    // full load of the document instead (loudly, through the error event).
    dispatch('krate:navigate-error', { url: location.href, status: loaded.status, error: 'script-load' });
    hardNavigate(location.href, true);
    return;
  }
  refreshPageBindings();
  dispatch('krate:navigate-end', { url: location.href, status: loaded.status });
}

/**
 * Runs the DOM mutation, inside a View Transition when supported. Resolves
 * once the mutation has been applied (the transition's update callback has
 * run), so callers can touch the new DOM, history and focus afterwards.
 */
function runMutation(mutate: () => void): Promise<void> {
  if (!supportsViewTransitions()) {
    mutate();
    return Promise.resolve();
  }
  let ran = false;
  const once = () => {
    if (ran) return;
    ran = true;
    mutate();
  };
  try {
    const vt = (document as any).startViewTransition(once);
    if (vt && vt.finished && typeof vt.finished.catch === 'function') vt.finished.catch(() => {});
    const done: Promise<unknown> | undefined = vt && vt.updateCallbackDone;
    if (done && typeof done.then === 'function') {
      return done.then(
        () => once(),
        () => once()
      );
    }
  } catch {
    /* fall through */
  }
  once();
  return Promise.resolve();
}

// Layout persistence

/** One layout/template boundary of a page (emitted by the compiler as #__krate_route). */
interface ChainEntry {
  id: string;
  /** "layout" or "template". */
  k: string;
  /** URL segments covered by the boundary's directory (-1: the rest). */
  d: number;
  /** Whether the layout can stay mounted across a soft navigation. */
  p: boolean;
  why?: string;
  f?: string;
  /** Parallel-route slots rendered by the layout. */
  s?: SlotInfo[];
}

/** One parallel-route slot of a layout (see the compiler's routeSlotInfo). */
interface SlotInfo {
  n: string;
  key: string;
  /** Route scope of the content currently in the slot. */
  scope: string;
  /** "page" (matched slot page), "default" (default.tsx) or "intercept". */
  src: string;
  /** URL patterns (no leading slash) the slot intercepts on soft navigation. */
  i?: string[];
}

let currentChain: ChainEntry[] = [];

function readRouteChain(doc: Document): ChainEntry[] {
  const el = doc.getElementById('__krate_route');
  if (!el) return [];
  try {
    const info = JSON.parse(el.textContent || '{}');
    return Array.isArray(info.chain) ? (info.chain as ChainEntry[]) : [];
  } catch {
    devWarn('unreadable #__krate_route route info; layouts will be remounted');
    return [];
  }
}

function segmentKey(url: string, depth: number): string {
  const segs = stripBase(new URL(url, location.href).pathname).split('/').filter(Boolean);
  return (depth < 0 ? segs : segs.slice(0, depth)).join('/');
}

/** Finds the <!--kl:ID--> ... <!--/kl:ID--> (or ks: slot) sibling pair under root. */
function findBoundary(root: Node, id: string, kind = 'kl:'): [Comment, Comment] | null {
  const doc = root.ownerDocument || (root as Document);
  const walker = doc.createTreeWalker(root, 128 /* NodeFilter.SHOW_COMMENT */);
  const open = kind + id;
  const close = '/' + kind + id;
  while (walker.nextNode()) {
    const c = walker.currentNode as Comment;
    if (c.nodeValue !== open) continue;
    for (let n = c.nextSibling; n; n = n.nextSibling) {
      if (n.nodeType === 8 && n.nodeValue === close) return [c, n as Comment];
    }
    return null;
  }
  return null;
}

function between(start: Node, end: Node): Node[] {
  const out: Node[] = [];
  for (let n = start.nextSibling; n && n !== end; n = n.nextSibling) out.push(n);
  return out;
}

function clearBetween(start: Node, end: Node): void {
  const dispose = (globalThis as any).disposeNode;
  for (const n of between(start, end)) {
    n.parentNode!.removeChild(n);
    if (typeof dispose === 'function') dispose(n);
  }
}

function syncLoadingTemplate(liveRoot: Element, nextRoot: Element): void {
  const sel = ':scope > template[data-krate-loading]';
  liveRoot.querySelectorAll(sel).forEach((t) => t.remove());
  nextRoot.querySelectorAll(sel).forEach((t) => liveRoot.appendChild(document.importNode(t, true)));
}

function keepScopes(ids: string[]): void {
  const fn = (globalThis as any).krateKeepScopes;
  if (typeof fn === 'function') fn(ids);
}

interface SwapPlan {
  /** Route scopes (layouts, kept slot content) that stay mounted. */
  keep: string[];
  /** Route scopes whose hydration code the new page script must skip. */
  skip: string[];
  /** Live and incoming marker pairs of the region to replace; null swaps #root. */
  region: { live: [Comment, Comment]; next: [Comment, Comment] } | null;
  /** Slots of kept layouts whose content is replaced. */
  slots: Array<{ live: [Comment, Comment]; next: [Comment, Comment] }>;
  /** Route chain describing the DOM after the swap. */
  chain: ChainEntry[];
}

/**
 * Compares the layout chains of the current and next documents. The longest
 * common prefix of persistable layouts (same file, same URL segments) stays
 * mounted, and only the children region of the deepest kept layout is
 * replaced. Templates always end the prefix, so they remount on every
 * navigation through them. A shared layout that cannot be kept is reported
 * in dev (never silently). Slots of kept layouts are swapped when the new
 * route has a matching slot page and otherwise keep their content (a soft
 * navigation never resets a slot to its default).
 */
function planSwap(oldChain: ChainEntry[], newChain: ChainEntry[], fromUrl: string, toUrl: string, doc: Document): SwapPlan {
  const full: SwapPlan = { keep: [], skip: [], region: null, slots: [], chain: newChain };
  let k = 0;
  while (k < oldChain.length && k < newChain.length) {
    const a = oldChain[k];
    const b = newChain[k];
    if (a.id !== b.id || a.k !== 'layout' || b.k !== 'layout') break;
    if (segmentKey(fromUrl, a.d) !== segmentKey(toUrl, b.d)) break;
    if (!a.p || !b.p) {
      devWarn(`layout ${b.f || b.id} is remounted on this navigation: ${b.why || a.why || 'not persistable'}`);
      break;
    }
    k++;
  }
  if (k === 0) return full;
  if (typeof (globalThis as any).krateDisposeExcept !== 'function') {
    devWarn('the runtime has no krateDisposeExcept; layouts are remounted');
    return full;
  }
  const id = newChain[k - 1].id;
  const live = findBoundary(appRoot(document), id);
  const next = findBoundary(appRoot(doc), id);
  if (!live || !next) {
    devWarn(`layout ${newChain[k - 1].f || id} boundary markers are missing; the whole page is swapped`);
    return full;
  }
  const keep = oldChain.slice(0, k).map((e) => e.id);
  const skip = keep.slice();
  const slots: SwapPlan['slots'] = [];
  const chain = newChain.slice();
  for (let j = 0; j < k; j++) {
    const oldSlots = oldChain[j].s || [];
    const merged: SlotInfo[] = [];
    for (const ns of newChain[j].s || []) {
      const os = oldSlots.find((x) => x.key === ns.key);
      const liveSlot = findBoundary(appRoot(document), ns.key, 'ks:');
      const nextSlot = findBoundary(appRoot(doc), ns.key, 'ks:');
      if (os && liveSlot && (ns.src !== 'page' || !nextSlot)) {
        // No matching slot page for the new URL: the slot keeps its content.
        keep.push(os.scope);
        skip.push(os.scope, ns.scope);
        merged.push({ ...ns, scope: os.scope, src: os.src });
        continue;
      }
      if (liveSlot && nextSlot) {
        slots.push({ live: liveSlot, next: nextSlot });
        merged.push(ns);
        continue;
      }
      devWarn(`slot @${ns.n} markers are missing; the whole page is swapped`);
      return full;
    }
    chain[j] = { ...newChain[j], s: merged };
  }
  return { keep, skip, region: { live, next }, slots, chain };
}

// Intercepting routes

interface InterceptHit {
  entry: ChainEntry;
  slot: SlotInfo;
  /** URL path without base path and surrounding slashes. */
  path: string;
}

/** Detached slot content an intercept replaced, restored on back. */
interface SlotSnapshot {
  key: string;
  nodes: Node[];
  slot: SlotInfo;
  /** pathname+search of the page the intercept was opened from. */
  path: string;
}

const interceptSnapshots = new Map<string, SlotSnapshot>();
/** pathname+search of the last full document (the children route). */
let docPath = '';

type JSSeg = { kind: number; text: string };

function parsePattern(pattern: string): JSSeg[] {
  const t = pattern.replace(/^\/+|\/+$/g, '');
  if (!t) return [];
  return t.split('/').map((p) => {
    if (p.startsWith('[[...') && p.endsWith(']]')) return { kind: 3, text: p.slice(5, -2) };
    if (p.startsWith('[...') && p.endsWith(']')) return { kind: 2, text: p.slice(4, -1) };
    if (p.startsWith('[') && p.endsWith(']')) return { kind: 1, text: p.slice(1, -1) };
    return { kind: 0, text: p };
  });
}

/** Mirrors the compiler's matchRoutePattern (static, [param], [...all], [[...opt]]). */
export function matchRoutePattern(path: string, pattern: string): Record<string, string> | null {
  const segs = parsePattern(pattern);
  const t = path.replace(/^\/+|\/+$/g, '');
  const parts = t ? t.split('/') : [];
  const params: Record<string, string> = {};
  for (let i = 0; i < segs.length; i++) {
    const s = segs[i];
    if (s.kind >= 2) {
      if (i !== segs.length - 1) return null;
      const rest = parts.slice(i);
      if (s.kind === 2 && rest.length === 0) return null;
      if (rest.some((r) => r === '')) return null;
      params[s.text] = rest.map(decodeSeg).join('/');
      return params;
    }
    if (i >= parts.length) return null;
    if (s.kind === 1) {
      if (!parts[i]) return null;
      params[s.text] = decodeSeg(parts[i]);
    } else if (decodeSeg(parts[i]) !== s.text) {
      return null;
    }
  }
  return parts.length === segs.length ? params : null;
}

function decodeSeg(s: string): string {
  try {
    return decodeURIComponent(s);
  } catch {
    return s;
  }
}

/** Negative when pattern a is more specific than b (static > dynamic > catch-all > optional). */
export function compareRouteSpecificity(a: string, b: string): number {
  const sa = parsePattern(a);
  const sb = parsePattern(b);
  for (let i = 0; i < sa.length && i < sb.length; i++) {
    if (sa[i].kind !== sb[i].kind) return sa[i].kind - sb[i].kind;
    if (sa[i].kind === 0 && sa[i].text !== sb[i].text) return sa[i].text < sb[i].text ? -1 : 1;
  }
  if (sa.length !== sb.length) return sb.length - sa.length;
  return a < b ? -1 : a > b ? 1 : 0;
}

function findIntercept(url: string): InterceptHit | null {
  const path = stripBase(new URL(url, location.href).pathname).replace(/^\/+|\/+$/g, '');
  let best: { hit: InterceptHit; pattern: string } | null = null;
  for (const entry of currentChain) {
    for (const slot of entry.s || []) {
      for (const pattern of slot.i || []) {
        if (!matchRoutePattern(path, pattern)) continue;
        if (!best || compareRouteSpecificity(pattern, best.pattern) < 0) {
          best = { hit: { entry, slot, path }, pattern };
        }
      }
    }
  }
  if (best && !findBoundary(appRoot(document), best.hit.slot.key, 'ks:')) return null;
  return best ? best.hit : null;
}

function setSlotInfo(key: string, next: SlotInfo): void {
  currentChain = currentChain.map((e) =>
    e.s && e.s.some((x) => x.key === key) ? { ...e, s: e.s.map((x) => (x.key === key ? next : x)) } : e
  );
}

function syncParams(doc: Document): void {
  for (const script of Array.from(doc.querySelectorAll('script:not([src])'))) {
    const m = /window\.__KRATE_PARAMS__\s*=\s*(\{[\s\S]*?\})\s*;?\s*$/.exec(script.textContent || '');
    if (!m) continue;
    try {
      (globalThis as any).__KRATE_PARAMS__ = JSON.parse(m[1]);
    } catch {
      /* ignore */
    }
    return;
  }
  delete (globalThis as any).__KRATE_PARAMS__;
}

function disposeScopes(ids: string[]): void {
  const fn = (globalThis as any).krateDisposeScopes;
  if (typeof fn === 'function') fn(ids);
}

/**
 * Soft navigation to a URL a mounted slot intercepts: the slot shows the
 * intercepting fragment while the URL becomes the target and the rest of
 * the page (including the children route) stays as it is. The slot's
 * previous content is kept detached (alive) and restored by going back.
 */
async function runIntercept(req: NavRequest, hit: InterceptHit): Promise<void> {
  const from = location.href;
  if (req.kind !== 'pop') {
    if (!dispatch('krate:navigate-start', { url: req.url, from, kind: req.kind, intercept: hit.slot.n }, true)) return;
  }
  const seq = ++navSeq;
  if (pendingAbort) pendingAbort.abort();
  const ctrl = new AbortController();
  pendingAbort = ctrl;
  setStatus('loading');
  const fragURL = location.origin + basePath() + '/' + INTERCEPT_PREFIX + '/' + hit.slot.key + (hit.path ? '/' + hit.path : '');
  let html = '';
  let ok = false;
  try {
    const res = await fetch(fragURL, {
      headers: { Accept: 'text/html' },
      credentials: 'same-origin',
      signal: ctrl.signal,
      cache: req.bypassCache ? 'no-cache' : 'default',
    });
    html = await res.text();
    ok = res.ok && (isHTMLType(res.headers.get('content-type') || '') || looksLikeHTML(html));
    if (!ok) dispatch('krate:navigate-error', { url: req.url, status: res.status, intercept: hit.slot.n });
  } catch (err) {
    if ((err as any)?.name === 'AbortError' || ctrl.signal.aborted) return;
    dispatch('krate:navigate-error', { url: req.url, status: 0, error: err, intercept: hit.slot.n });
  } finally {
    if (pendingAbort === ctrl) pendingAbort = undefined;
  }
  if (seq !== navSeq) return;
  const doc = ok ? new DOMParser().parseFromString(html, 'text/html') : null;
  const next = doc ? findBoundary(appRoot(doc), hit.slot.key, 'ks:') : null;
  const live = findBoundary(appRoot(document), hit.slot.key, 'ks:');
  let frag: { key?: string; scope?: string } | null = null;
  try {
    const el = doc && doc.getElementById('__krate_route');
    frag = el ? JSON.parse(el.textContent || '{}').frag || null : null;
  } catch {
    frag = null;
  }
  if (!doc || !next || !live || !frag || frag.key !== hit.slot.key || !frag.scope) {
    // The fragment is missing or malformed: show the real page instead,
    // loudly (the error event above, and a console warning).
    try {
      console.warn(`[krate router] intercepting route for ${req.url} (slot @${hit.slot.n}) could not be loaded; navigating to the page`);
    } catch {
      /* ignore */
    }
    setStatus('idle');
    return runNavigation({ ...req });
  }
  if (req.kind === 'push') saveScroll(currentKey);

  // Swap the slot content.
  if (hit.slot.src !== 'intercept') {
    interceptSnapshots.set(currentKey, { key: hit.slot.key, nodes: between(live[0], live[1]), slot: hit.slot, path: docPath });
    for (const n of between(live[0], live[1])) n.parentNode!.removeChild(n);
  } else {
    disposeScopes([hit.slot.scope]);
    clearBetween(live[0], live[1]);
  }
  for (const n of between(next[0], next[1])) live[1].parentNode!.insertBefore(document.importNode(n, true), live[1]);
  // A fragment only adds its stylesheets; the page keeps its own.
  const existing = new Set<string>();
  document.querySelectorAll('link[rel="stylesheet"]').forEach((l) => existing.add((l as HTMLLinkElement).href));
  doc.querySelectorAll('link[rel="stylesheet"][href]').forEach((l) => {
    const abs = new URL(l.getAttribute('href')!, fragURL).href;
    if (existing.has(abs)) return;
    const link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = abs;
    document.head.appendChild(link);
  });
  setSlotInfo(hit.slot.key, { ...hit.slot, scope: frag.scope, src: 'intercept' });

  const state = { __krate: '', state: req.state === undefined ? null : req.state, __kint: { key: hit.slot.key, base: docPath } };
  if (req.kind === 'push') {
    currentKey = newKey();
    state.__krate = currentKey;
    history.pushState(state, '', req.url);
  } else if (req.kind === 'replace') {
    currentKey = newKey();
    state.__krate = currentKey;
    history.replaceState(state, '', req.url);
  } else if (req.kind === 'pop' && req.restoreKey) {
    currentKey = req.restoreKey;
  }
  renderedPath = pathOf(location.href);
  syncParams(doc);
  updateActiveLinks();
  dispatch('krate:navigate', { url: location.href, status: 200, intercept: hit.slot.n });
  notifyNavigation(location.href);
  setStatus('idle');

  const srcs = scriptsToLoad(doc, fragURL);
  const results = await Promise.all(srcs.map(loadScript));
  if (seq !== navSeq) return;
  if (results.some((r) => !r)) {
    dispatch('krate:navigate-error', { url: location.href, status: 200, error: 'script-load', intercept: hit.slot.n });
    hardNavigate(location.href, true);
    return;
  }
  refreshPageBindings();
  dispatch('krate:navigate-end', { url: location.href, status: 200, intercept: hit.slot.n });
}

/** Back to the page an intercept was opened from: put the slot content back. */
function restoreSnapshot(snap: SlotSnapshot, key: string): boolean {
  const live = findBoundary(appRoot(document), snap.key, 'ks:');
  const current = currentChain.flatMap((e) => e.s || []).find((x) => x.key === snap.key);
  if (!live || !current || current.src !== 'intercept') return false;
  disposeScopes([current.scope]);
  clearBetween(live[0], live[1]);
  for (const n of snap.nodes) live[1].parentNode!.insertBefore(n, live[1]);
  setSlotInfo(snap.key, snap.slot);
  interceptSnapshots.delete(key);
  currentKey = key;
  renderedPath = pathOf(location.href);
  const saved = scrollPositions.get(key);
  if (saved !== undefined) window.scrollTo(0, saved);
  updateActiveLinks();
  dispatch('krate:navigate', { url: location.href, status: 200 });
  notifyNavigation(location.href);
  emit();
  return true;
}

function sameDocument(url: string): boolean {
  return pathOf(url) === renderedPath;
}

/** In-document navigation: only the hash (or nothing) changes. */
function hashNavigate(url: string, req: NavRequest): void {
  saveScroll(currentKey);
  if (url !== location.href || req.state !== undefined) {
    if (req.kind === 'replace') {
      history.replaceState(historyEntry(currentKey || newKey(), req.state), '', url);
    } else {
      currentKey = newKey();
      history.pushState(historyEntry(currentKey, req.state), '', url);
    }
  }
  const hash = new URL(url).hash;
  if (!scrollToHash(hash) && req.scroll && !hash) window.scrollTo(0, 0);
  emit();
}

function start(req: NavRequest): Promise<void> {
  let u: URL;
  try {
    u = new URL(req.url, location.href);
  } catch {
    hardNavigate(req.url);
    return Promise.resolve();
  }
  if (u.origin !== location.origin) {
    hardNavigate(u.href, req.kind === 'replace');
    return Promise.resolve();
  }
  req.url = u.href;
  if ((req.kind === 'push' || req.kind === 'replace') && sameDocument(u.href) && (u.hash || u.href === location.href)) {
    hashNavigate(u.href, req);
    return Promise.resolve();
  }
  if (req.kind === 'push' || req.kind === 'replace') {
    const hit = findIntercept(u.href);
    if (hit) return runIntercept(req, hit);
  }
  return runNavigation(req);
}

// Event handlers

function clickHandler(e: MouseEvent): void {
  if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  const t = e.target as Element | null;
  if (!t || typeof t.closest !== 'function') return;
  const a = t.closest('a[data-krate-link]') as HTMLAnchorElement | null;
  if (!a || !isLocalLink(a)) return;
  const href = a.getAttribute('href');
  if (!href) return;
  e.preventDefault();
  void start({
    url: new URL(href, location.href).href,
    kind: a.hasAttribute('data-krate-replace') ? 'replace' : 'push',
    scroll: a.getAttribute('data-krate-scroll') !== 'false',
  });
}

function submitHandler(e: SubmitEvent): void {
  if (e.defaultPrevented) return;
  const form = e.target as HTMLFormElement | null;
  if (!form || form.tagName !== 'FORM') return;
  if (form.hasAttribute('data-krate-reload')) return;
  const submitter = (e.submitter || null) as HTMLButtonElement | HTMLInputElement | null;
  const method = ((submitter && submitter.getAttribute('formmethod')) || form.getAttribute('method') || 'get').toLowerCase();
  if (method !== 'get') return;
  const target = (submitter && submitter.getAttribute('formtarget')) || form.getAttribute('target');
  if (target && target !== '_self') return;
  const actionAttr = (submitter && submitter.getAttribute('formaction')) || form.getAttribute('action') || '';
  let action: URL;
  try {
    action = new URL(actionAttr || location.href, location.href);
  } catch {
    return;
  }
  if (action.origin !== location.origin || action.protocol.indexOf('http') !== 0) return;
  let data: FormData;
  try {
    data = submitter ? new FormData(form, submitter) : new FormData(form);
  } catch {
    data = new FormData(form);
    if (submitter && submitter.name) data.append(submitter.name, submitter.value);
  }
  const params = new URLSearchParams();
  for (const [k, v] of data.entries()) {
    // A file input cannot be encoded into a GET query; leave it to the browser.
    if (typeof v !== 'string') return;
    params.append(k, v);
  }
  e.preventDefault();
  action.search = params.toString();
  action.hash = '';
  void start({ url: action.href, kind: 'push', scroll: true });
}

function popstateHandler(e: PopStateEvent): void {
  // The URL already changed; remember where the user was on the entry left.
  saveScroll(currentKey);
  const key = entryKey(e.state);
  const url = location.href;
  if (pathOf(url) === renderedPath) {
    // Hash-only history step within the rendered document.
    currentKey = key || currentKey;
    const saved = key ? scrollPositions.get(key) : undefined;
    if (saved !== undefined) window.scrollTo(0, saved);
    else scrollToHash(location.hash);
    emit();
    return;
  }
  // Back to the page an intercept was opened from.
  const snap = key ? interceptSnapshots.get(key) : undefined;
  if (snap && key && snap.path === pathOf(url) && snap.path === docPath && restoreSnapshot(snap, key)) return;
  // Back/forward to an intercepted entry while its base page is still shown.
  const kint = e.state && typeof e.state === 'object' ? (e.state as any).__kint : null;
  if (kint && kint.base === docPath) {
    const hit = findIntercept(url);
    if (hit && hit.slot.key === kint.key) {
      void runIntercept({ url, kind: 'pop', scroll: false, restoreKey: key }, hit);
      return;
    }
  }
  void runNavigation({ url, kind: 'pop', scroll: true, restoreKey: key });
}

function revalidateHandler(): void {
  invalidatePrefetchCache();
  void refresh();
}

function pagehideHandler(): void {
  saveScroll(currentKey);
  // Hand scroll restoration back to the browser for reloads and cross-document
  // history steps.
  try {
    history.scrollRestoration = 'auto';
  } catch {
    /* ignore */
  }
}

function pageshowHandler(): void {
  try {
    history.scrollRestoration = 'manual';
  } catch {
    /* ignore */
  }
}

/** Per-page bindings refreshed after every swap (and by reinitRouter). */
function refreshPageBindings(): void {
  observePrefetchLinks();
  updateActiveLinks();
  applyViewTransitionNames(document);
  // Mark the page scripts as disposable so they're removed and re-executed
  // against the fresh DOM on the next navigation.
  document.querySelectorAll('script[src]').forEach((s) => {
    if (!RUNTIME_CHUNK_RE.test((s as HTMLScriptElement).src)) s.setAttribute('data-krate-spa', '');
  });
}

// Public API

function refresh(): Promise<void> {
  if (!initialized || typeof location === 'undefined') {
    if (typeof location !== 'undefined') location.reload();
    return Promise.resolve();
  }
  if (renderedPath !== docPath) {
    // The URL shows an intercepted route: refresh the slot content.
    const hit = findIntercept(location.href);
    if (hit && hit.slot.src === 'intercept') {
      return runIntercept({ url: location.href, kind: 'refresh', scroll: false, bypassCache: true }, hit);
    }
  }
  return runNavigation({ url: location.href, kind: 'refresh', scroll: false, bypassCache: true });
}

function push(url: string, opts: NavigateOptions = {}): Promise<void> {
  if (!initialized) {
    if (typeof location !== 'undefined') location.assign(url);
    return Promise.resolve();
  }
  return start({ url, kind: 'push', scroll: opts.scroll !== false, state: opts.state });
}

function replace(url: string, opts: NavigateOptions = {}): Promise<void> {
  if (!initialized) {
    if (typeof location !== 'undefined') location.replace(url);
    return Promise.resolve();
  }
  return start({ url, kind: 'replace', scroll: opts.scroll !== false, state: opts.state });
}

/**
 * The router API. Every navigation method resolves once the new route's DOM
 * and URL are in place (page scripts may still be loading).
 *
 * - push(url, opts) / replace(url, opts): client navigation.
 * - back() / forward(): history traversal.
 * - refresh(): re-fetches the current route (bypassing the prefetch cache)
 *   and swaps it in place, keeping scroll position and history.
 * - prefetch(url): warms the document (and its page chunk) for a later visit.
 */
export const krateRouter = {
  push,
  replace,
  back(): void {
    if (typeof history !== 'undefined') history.back();
  },
  forward(): void {
    if (typeof history !== 'undefined') history.forward();
  },
  refresh,
  prefetch(url: string): Promise<void> {
    if (!initialized || typeof location === 'undefined') return Promise.resolve();
    let u: URL;
    try {
      u = new URL(url, location.href);
    } catch {
      return Promise.resolve();
    }
    if (u.origin !== location.origin) return Promise.resolve();
    return prefetchPage(u.href);
  },
  subscribe: subscribeNavigation,
  getState: getNavigationState,
  invalidate: invalidatePrefetchCache,
};

/**
 * Programmatic SPA navigation. Falls back to a full page load when the router
 * has not initialized (e.g. before hydration). Returns false when handled
 * client-side, true when a full navigation was triggered.
 */
export function navigate(url: string, opts: NavigateOptions = {}): boolean {
  if (initialized) {
    void (opts.replace ? replace(url, opts) : push(url, opts));
    return false;
  }
  if (typeof location !== 'undefined') {
    if (opts.replace) location.replace(url);
    else location.assign(url);
  }
  return true;
}

export function initRouter(): void {
  if (typeof document === 'undefined') return;
  if (initialized) {
    refreshPageBindings();
    return;
  }
  initialized = true;

  renderedPath = pathOf(location.href);
  currentChain = readRouteChain(document);
  docPath = renderedPath;
  currentKey = entryKey(history.state) || newKey();
  if (entryKey(history.state) === undefined) {
    try {
      history.replaceState(historyEntry(currentKey, history.state), '', location.href);
    } catch {
      /* ignore */
    }
  }
  try {
    history.scrollRestoration = 'manual';
  } catch {
    /* ignore */
  }

  document.addEventListener('click', clickHandler);
  document.addEventListener('submit', submitHandler as EventListener);
  window.addEventListener('popstate', popstateHandler);
  window.addEventListener('krate:revalidate', revalidateHandler);
  window.addEventListener('pagehide', pagehideHandler);
  window.addEventListener('pageshow', pageshowHandler);
  setupPrefetch();
  setupSpeculationRules();
  refreshPageBindings();
}

/**
 * Called at the end of every page hydration script. The router keeps its
 * listeners and caches; only the per-page bindings are refreshed.
 */
export function reinitRouter(): void {
  initRouter();
}

/** Tears the router down (tests and embedders that unmount Krate). */
export function destroyRouter(): void {
  if (!initialized) return;
  initialized = false;
  document.removeEventListener('click', clickHandler);
  document.removeEventListener('submit', submitHandler as EventListener);
  window.removeEventListener('popstate', popstateHandler);
  window.removeEventListener('krate:revalidate', revalidateHandler);
  window.removeEventListener('pagehide', pagehideHandler);
  window.removeEventListener('pageshow', pageshowHandler);
  document.removeEventListener('mouseover', prefetchHoverHandler);
  document.removeEventListener('focusin', prefetchHoverHandler);
  for (const timer of prefetchTimers.values()) clearTimeout(timer);
  prefetchTimers = new Map();
  if (prefetchObserver) {
    prefetchObserver.disconnect();
    prefetchObserver = null;
  }
  if (pendingAbort) pendingAbort.abort();
  pendingAbort = undefined;
  navSeq++;
  prefetchCache.clear();
  prefetchInflight.clear();
  scrollPositions.clear();
  errorPageCache.clear();
  listeners.clear();
  status = 'idle';
  loadingOverlay = [];
  currentChain = [];
  interceptSnapshots.clear();
  docPath = '';
}
