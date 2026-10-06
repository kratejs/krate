let initialized = false;
let pendingAbort: AbortController | undefined;
let routerCleanup: (() => void) | null = null;
// Monotonic navigation sequence: a slower, superseded fetch must never apply
// its HTML over a newer navigation.
let navSeq = 0;

/** Cache for prefetched page HTML keyed by URL (bounded LRU). */
const prefetchCache = new Map<string, string>();
const PREFETCH_CACHE_MAX = 30;

/** Saved scroll positions for back/forward navigation restoration (bounded). */
const scrollPositions = new Map<string, number>();
const SCROLL_HISTORY_MAX = 50;

/**
 * The shared runtime chunk is loaded once and must persist across SPA
 * navigations. Page-specific hydration scripts, in contrast, must be
 * re-executed on every visit (against the freshly reconciled DOM), so they are
 * removed before loading a new page and re-appended.
 */
const RUNTIME_CHUNK_RE = /\/chunks\/runtime\.[^/]+\.js$/;

/** Site config injected by the compiler (`window.__KRATE_CFG__`). */
interface KrateRuntimeConfig {
  basePath?: string;
  viewTransitions?: boolean | 'auto';
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

// Set during initRouter so the exported `navigate()` can drive the SPA router.
let navigateImpl: ((url: string, opts?: NavigateOptions) => void) | null = null;

export interface NavigateOptions {
  replace?: boolean;
  scroll?: boolean;
  /** State stored on the new history entry. */
  state?: unknown;
}

/**
 * Programmatic SPA navigation. Falls back to a full page load when the router
 * has not initialized (e.g. before hydration). Returns false when handled
 * client-side, true when a full navigation was triggered.
 */
export function navigate(url: string, opts: NavigateOptions = {}): boolean {
  if (navigateImpl) {
    navigateImpl(url, opts);
    return false;
  }
  if (typeof location !== 'undefined') location.href = url;
  return true;
}

export function initRouter(): void {
  if (typeof document === 'undefined') return;

  // Fix 4.4: Guard against multiple initRouter calls
  if (initialized) return;
  initialized = true;

  // Reconcile from the app root so navigation between DIFFERENT shells (the
  // marketing layout's <main> and the docs layout's `.docs-page`) swaps the
  // whole shell instead of reconciling mismatched tree shapes (which dropped
  // the docs navbar/sidebar while still loading its CSS). Within one shell the
  // diff preserves unchanged nodes (sidebar/TOC) by key, so docs↔docs stays
  // cheap.
  const CONTENT_SEL = '#root, main, .docs-content';
  const TRANSITION_MS = 150;
  const PREFETCH_DELAY = 200;
  let notFoundHTML: string | null = null;
  let errorHTML: string | null = null;
  let prefetchTimers = new Map<Element, ReturnType<typeof setTimeout>>();
  let prefetchObserver: IntersectionObserver | null = null;

  function findContentRoot(doc: Document): Element | null {
    for (const sel of CONTENT_SEL.split(', ')) {
      const el = doc.querySelector(sel.trim());
      if (el) return el;
    }
    return doc.body;
  }

  function isLocalLink(a: HTMLAnchorElement): boolean {
    if (a.hasAttribute('data-krate-external')) return false;
    const href = a.getAttribute('href');
    if (!href) return false;
    if (href.startsWith('#') || href.startsWith('javascript:')) return false;
    if (a.origin !== location.origin) return false;
    if (a.target === '_blank') return false;
    return true;
  }

  function prefetchPage(url: string): void {
    if (prefetchCache.has(url)) return;
    fetch(url, { headers: { Accept: 'text/html' } })
      .then((res) => {
        if (res.ok) return res.text();
        return null;
      })
      .then((html) => {
        if (html) {
          // Bounded LRU: evict the oldest entry when the cache is full.
          if (prefetchCache.size >= PREFETCH_CACHE_MAX) {
            const oldest = prefetchCache.keys().next().value;
            if (oldest !== undefined) prefetchCache.delete(oldest);
          }
          prefetchCache.set(url, html);
        }
      })
      .catch(() => {});
  }

  function setupPrefetch(): void {
    // Hover/focus prefetching
    document.addEventListener('mouseover', prefetchHoverHandler);
    document.addEventListener('focusin', prefetchHoverHandler);

    // Viewport prefetching via IntersectionObserver
    if (typeof IntersectionObserver !== 'undefined') {
      prefetchObserver = new IntersectionObserver(
        (entries) => {
          for (const entry of entries) {
            if (entry.isIntersecting) {
              const a = entry.target as HTMLAnchorElement;
              const href = a.getAttribute('href');
              if (href && isLocalLink(a)) {
                const url = new URL(href, location.origin).href;
                prefetchPage(url);
              }
              prefetchObserver!.unobserve(entry.target);
            }
          }
        },
        { rootMargin: '200px' }
      );
      observePrefetchLinks();
    }
  }

  function observePrefetchLinks(): void {
    if (!prefetchObserver) return;
    document.querySelectorAll('a[data-prefetch]').forEach((a) => {
      prefetchObserver!.observe(a);
    });
  }

  function cleanupPrefetch(): void {
    document.removeEventListener('mouseover', prefetchHoverHandler);
    document.removeEventListener('focusin', prefetchHoverHandler);
    for (const timer of prefetchTimers.values()) {
      clearTimeout(timer);
    }
    prefetchTimers.clear();
    if (prefetchObserver) {
      prefetchObserver.disconnect();
      prefetchObserver = null;
    }
  }

  function prefetchHoverHandler(e: Event): void {
    const a = (e.target as HTMLElement).closest('a[data-prefetch]') as HTMLAnchorElement | null;
    if (!a || !isLocalLink(a)) return;
    const href = a.getAttribute('href');
    if (!href) return;

    const url = new URL(href, location.origin).href;
    if (prefetchCache.has(url)) return;

    // Debounce: start prefetch after a short delay
    const existing = prefetchTimers.get(a);
    if (existing) clearTimeout(existing);

    const timer = setTimeout(() => {
      prefetchTimers.delete(a);
      prefetchPage(url);
    }, PREFETCH_DELAY);
    prefetchTimers.set(a, timer);
  }

  // Begin the visual transition (fade out + optional loading overlay) while a
  // page fetch is in flight, so navigation doesn't feel artificially delayed.
  let loadingOverlay: Node | null = null;
  function beginTransition(): void {
    if (supportsViewTransitions()) return; // native View Transition handles it
    const oldContent = findContentRoot(document);
    const loadingTemplate = document.querySelector('template[data-krate-loading]') as HTMLTemplateElement | null;
    if (loadingTemplate && loadingTemplate.content && oldContent) {
      // Non-destructive loading UI: overlay the template without wiping the
      // live DOM, so reconciliation still preserves node state.
      loadingOverlay = loadingTemplate.content.cloneNode(true);
      document.body.appendChild(loadingOverlay);
    }
    if (oldContent) {
      (oldContent as HTMLElement).style.transition = `opacity ${TRANSITION_MS}ms ease`;
      (oldContent as HTMLElement).style.opacity = '0';
    }
  }

  function endTransition(): void {
    if (supportsViewTransitions()) return; // opacity was never touched
    if (loadingOverlay && loadingOverlay.parentNode) {
      loadingOverlay.parentNode.removeChild(loadingOverlay);
    }
    loadingOverlay = null;
    const oldContent = findContentRoot(document);
    if (oldContent) {
      (oldContent as HTMLElement).style.opacity = '1';
    }
  }

  // Analytics hook: fires once after every successful SPA navigation
  // (click, prefetch-cache, back/forward, and error-page replacements all flow
  // through swapContent/replacePage). A throw inside the hook is ignored.
  function notifyNavigation(url: string): void {
    try {
      const hook = (globalThis as any).__krate_onNavigation;
      if (typeof hook === 'function') hook(url);
    } catch {
      /* ignore hook failures */
    }
  }

  function dispatchNavEvent(name: string, url: string): void {
    try {
      window.dispatchEvent(new CustomEvent(name, { detail: { url } }));
    } catch {
      /* ignore */
    }
  }

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

  /** Diff stylesheet <link>s by absolute URL (base-path and format safe). */
  function reconcileStylesheets(doc: Document): void {
    const newStyles = doc.querySelectorAll('link[rel="stylesheet"]');
    const newHrefs = new Set<string>();
    newStyles.forEach((s) => {
      const href = s.getAttribute('href');
      if (href) newHrefs.add(new URL(href, location.origin).href);
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
      const abs = new URL(href, location.origin).href;
      if (!existing.has(abs)) {
        const link = document.createElement('link');
        link.rel = 'stylesheet';
        link.href = abs;
        document.head.appendChild(link);
      }
    });
  }

  /**
   * Apply `data-view-transition="name"` to `view-transition-name`. Persistent
   * chrome (navbar/sidebar) keeps its name across mutations so it morphs
   * instead of cross-fading. Runs at init and after every swap.
   */
  function applyViewTransitionNames(root: ParentNode): void {
    root.querySelectorAll('[data-view-transition]').forEach((el) => {
      const name = (el as HTMLElement).getAttribute('data-view-transition');
      if (name) (el as HTMLElement).style.viewTransitionName = name;
    });
  }

  /** Move focus to the content root after navigation (a11y). */
  function focusContent(): void {
    const root = findContentRoot(document) as HTMLElement | null;
    if (!root) return;
    if (!root.hasAttribute('tabindex')) root.setAttribute('tabindex', '-1');
    try {
      root.focus({ preventScroll: true });
    } catch {
      try {
        root.focus();
      } catch {
        /* ignore */
      }
    }
  }

  /**
   * Inject Speculation Rules so the browser prefetches same-origin links ahead
   * of hover, where supported. Falls back to the JS prefetch path elsewhere.
   */
  function setupSpeculationRules(): void {
    const supports = (HTMLScriptElement as any).supports;
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

  // The app shell lives in `#root` on every page (see generateHTML), so it is
  // the one stable swap target. Heuristic per-page content roots made different
  // shells/page shapes reconcile incorrectly (dropping the navbar); swapping
  // the whole shell is always correct and the new page's own scripts hydrate it.
  function appRoot(doc: Document): HTMLElement {
    return (doc.getElementById('root') || doc.body) as HTMLElement;
  }

  // Document-level <head> tags that change per page and must follow a swap.
  const HEAD_SYNC_SEL =
    'meta[name="description"], meta[property^="og:"], meta[name^="twitter:"], link[rel="canonical"], script[type="application/ld+json"]';

  function reconcileHeadMeta(doc: Document): void {
    document.head.querySelectorAll(HEAD_SYNC_SEL).forEach((el) => el.remove());
    doc.head.querySelectorAll(HEAD_SYNC_SEL).forEach((el) => {
      document.head.appendChild(document.importNode(el, true));
    });
  }

  function swapContent(
    html: string,
    url: string,
    scripts: HTMLScriptElement[],
    opts: { replace?: boolean; scroll?: boolean; restoreScroll?: boolean } = {}
  ): void {
    const { replace = false, scroll = true, restoreScroll = false } = opts;
    const parser = new DOMParser();
    const doc = parser.parseFromString(html, 'text/html');

    const newContent = appRoot(doc);
    const oldContent = appRoot(document);
    if (!newContent || !oldContent) {
      location.href = url;
      return;
    }

    dispatchNavEvent('krate:navigate-start', url);

    // The single synchronous DOM mutation point. Wrapped in a View Transition
    // when the browser supports it, so the SPA morph is animated natively.
    const runMutation = () => {
      const newTitle = doc.querySelector('title');
      if (newTitle) document.title = newTitle.textContent || '';

      // Dispose all active effects before replacing DOM
      if (typeof (globalThis as any).disposeAll === 'function') {
        (globalThis as any).disposeAll();
      }

      // Replace the entire app shell. This is deterministic for ANY page pair
      // (docs↔docs, marketing↔docs, different layouts): no structural mismatch
      // can drop chrome. The new page's scripts re-hydrate below, and View
      // Transitions keep the visual transition smooth.
      const nodes = Array.from(newContent.childNodes).map((n) => document.importNode(n, true));
      oldContent.replaceChildren(...nodes);

      reconcileStylesheets(doc);
      reconcileHeadMeta(doc);
      applyViewTransitionNames(document);
    };

    if (supportsViewTransitions()) {
      try {
        const vt = (document as any).startViewTransition(runMutation);
        if (vt && vt.finished && typeof vt.finished.catch === 'function') {
          vt.finished.catch(() => {});
        }
      } catch {
        runMutation();
      }
    } else {
      runMutation();
    }

    // Load new scripts, then rehydrate
    loadScripts(scripts, url).then(() => {
      try {
        rehydrate();
        observePrefetchLinks();
      } catch (err) {
        // Hydration threw (e.g. a page-side error or a compiler emission bug).
        // Rather than leave the route half-mounted against a live SPA DOM,
        // fall back to a full page load of the SSR HTML. The optional global
        // hook lets embedders be notified; a throw inside the hook is ignored.
        try {
          const hook = (globalThis as any).__krate_onHydrationError;
          if (typeof hook === 'function') hook(err);
        } catch {
          /* ignore hook failures */
        }
        location.href = url;
      }
    });

    endTransition();

    // History: replace keeps the current entry; push appends one (unless we
    // are restoring from a back/forward navigation).
    const prevUrl = location.pathname + location.search;
    if (replace) {
      history.replaceState({}, '', url);
    } else {
      if (!restoreScroll) {
        if (scrollPositions.size >= SCROLL_HISTORY_MAX) {
          const oldest = scrollPositions.keys().next().value;
          if (oldest !== undefined) scrollPositions.delete(oldest);
        }
        scrollPositions.set(prevUrl, window.scrollY);
      }
      history.pushState({}, '', url);
    }

    applyScroll(url, { restoreScroll, scroll });
    if (!restoreScroll) focusContent();
    updateActiveLinks();

    window.dispatchEvent(new CustomEvent('krate:navigate', { detail: { url } }));
    dispatchNavEvent('krate:navigate-end', url);
    notifyNavigation(url);
  }

  function handleHashScroll(url: string): boolean {
    const hashIdx = url.indexOf('#');
    if (hashIdx < 0 || hashIdx === url.length - 1) return false;
    const hash = url.slice(hashIdx);
    try {
      const el = document.querySelector(hash);
      if (el) {
        el.scrollIntoView({ behavior: 'smooth', block: 'start' });
        return true;
      }
    } catch {
      return false;
    }
    return false;
  }

  function applyScroll(url: string, opts: { restoreScroll?: boolean; scroll?: boolean }): void {
    if (opts.restoreScroll) {
      const saved = scrollPositions.get(url);
      if (saved !== undefined) {
        window.scrollTo(0, saved);
        return;
      }
    }
    if (handleHashScroll(url)) return;
    if (opts.scroll) window.scrollTo(0, 0);
  }

  // Mark the link matching the current route with aria-current="page".
  function updateActiveLinks(): void {
    const path = stripBase(location.pathname);
    document.querySelectorAll('a[data-krate-link]').forEach((a) => {
      const href = a.getAttribute('href');
      if (!href) {
        a.removeAttribute('aria-current');
        return;
      }
      let target = href;
      try {
        target = stripBase(new URL(href, location.origin).pathname);
      } catch {
        return;
      }
      const active =
        target === path ||
        (path === '/' && (target === '' || target === '/')) ||
        (target !== '/' && target.length > 1 && path.startsWith(target));
      if (active) a.setAttribute('aria-current', 'page');
      else a.removeAttribute('aria-current');
    });
  }

  function loadScripts(scripts: HTMLScriptElement[], baseUrl: string): Promise<void> {
    if (scripts.length === 0) return Promise.resolve();
    // Load all scripts in parallel (no serialization latency); hydration runs
    // once every script is loaded.
    return Promise.all(
      scripts.map((script) => {
        const src = script.getAttribute('src');
        return src ? loadScript(src, baseUrl) : Promise.resolve();
      })
    ).then(() => undefined);
  }

  function loadScript(src: string, baseUrl: string): Promise<void> {
    return new Promise((resolve) => {
      const s = document.createElement('script');
      const resolved = resolveSrc(src, baseUrl);
      s.src = resolved;
      // Only page scripts are disposable across navigations — the shared
      // runtime chunk stays in place.
      if (!RUNTIME_CHUNK_RE.test(resolved)) s.setAttribute('data-krate-spa', '');
      s.onload = () => resolve();
      s.onerror = () => resolve();
      document.body.appendChild(s);
    });
  }

  // Resolve a script URL against the navigated-to page (not the current one),
  // so relative srcs like "index.a1b2c3.js" load from the new page's directory.
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

  function rehydrate(): void {
    if (typeof (globalThis as any).__krate_hydrate === 'function') {
      (globalThis as any).__krate_hydrate();
    }
  }

  function replacePage(html: string, url: string): void {
    // Dispose all active effects before full page replacement
    if (typeof (globalThis as any).disposeAll === 'function') {
      (globalThis as any).disposeAll();
    }
    history.pushState({}, '', url);
    notifyNavigation(url);
    document.open();
    document.write(html);
    document.close();
  }

  function fetchNotFoundPage(): Promise<string | null> {
    if (notFoundHTML !== null) return Promise.resolve(notFoundHTML);
    return fetch('/404.html', { headers: { Accept: 'text/html' } })
      .then((res) => {
        if (!res.ok) return null;
        return res.text();
      })
      .then((html) => {
        notFoundHTML = html;
        return html;
      })
      .catch(() => null);
  }

  function fetchErrorPage(): Promise<string | null> {
    if (errorHTML !== null) return Promise.resolve(errorHTML);
    return fetch('/500.html', { headers: { Accept: 'text/html' } })
      .then((res) => {
        if (!res.ok) return null;
        return res.text();
      })
      .then((html) => {
        errorHTML = html;
        return html;
      })
      .catch(() => null);
  }

  function extractScripts(doc: Document, baseUrl: string): HTMLScriptElement[] {
    // Remove previously SPA-loaded page scripts. They must re-execute on every
    // visit (the reconciled DOM is new), and they'd otherwise accumulate.
    document.querySelectorAll('script[data-krate-spa]').forEach((s) => s.remove());

    const scripts: HTMLScriptElement[] = [];
    const existingSrcs = new Set<string>();
    document.querySelectorAll('script[src]').forEach((s) => {
      existingSrcs.add((s as HTMLScriptElement).src);
    });
    doc.querySelectorAll('script[src]').forEach((s) => {
      const src = (s as HTMLScriptElement).getAttribute('src');
      if (src && !existingSrcs.has(resolveSrc(src, baseUrl))) {
        scripts.push(s as HTMLScriptElement);
      }
    });
    return scripts;
  }

  function fetchAndSwap(
    url: string,
    opts: { replace?: boolean; scroll?: boolean; restoreScroll?: boolean } = {}
  ): void {
    const seq = ++navSeq;

    // Check prefetch cache first — already-loaded content swaps instantly.
    const cached = prefetchCache.get(url);
    if (cached) {
      const parser = new DOMParser();
      const doc = parser.parseFromString(cached, 'text/html');
      swapContent(cached, url, extractScripts(doc, url), opts);
      return;
    }

    // Begin the fade/loading transition while the fetch is in flight.
    beginTransition();

    // Fix 4.6: Abort any in-flight navigation
    if (pendingAbort) {
      pendingAbort.abort();
    }
    pendingAbort = new AbortController();
    const signal = pendingAbort.signal;

    fetch(url, {
      headers: { Accept: 'text/html' },
      signal,
    })
      .then((res) => {
        if (seq !== navSeq) return; // superseded by a newer navigation
        if (!res.ok) {
          const page = res.status >= 500 ? fetchErrorPage() : fetchNotFoundPage();
          return page.then((html) => {
            if (seq !== navSeq) return;
            if (html) {
              // Error pages contain full layout — full page replacement
              replacePage(html, url);
            } else {
              location.href = url;
            }
          });
        }
        return res.text().then((html) => {
          if (seq !== navSeq) return; // superseded while reading the body
          const parser = new DOMParser();
          const doc = parser.parseFromString(html, 'text/html');
          swapContent(html, url, extractScripts(doc, url), opts);
        });
      })
      .catch((err) => {
        // Ignore aborted requests; otherwise abort the transition so the page
        // fades back in.
        if (err?.name !== 'AbortError') {
          endTransition();
          location.href = url;
        }
      })
      .finally(() => {
        if (pendingAbort && !signal.aborted) {
          pendingAbort = undefined;
        }
      });
  }

  const clickHandler = (e: MouseEvent) => {
    // Never intercept modified clicks (cmd/ctrl/shift/alt or middle button) —
    // they should open in a new tab or trigger browser behavior.
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    const a = (e.target as HTMLElement).closest('a[data-krate-link]') as HTMLAnchorElement | null;
    if (!a || !isLocalLink(a)) return;

    e.preventDefault();
    const href = a.getAttribute('href');
    if (!href) return;
    const url = new URL(href, location.origin).href;
    const replace = a.hasAttribute('data-krate-replace');
    const scroll = a.getAttribute('data-krate-scroll') !== 'false';

    // Same-URL navigation (e.g. hash change): just update history + scroll.
    if (href === location.pathname + location.search + location.hash) {
      if (replace) history.replaceState({}, '', href);
      applyScroll(url, { scroll });
      return;
    }
    fetchAndSwap(url, { replace, scroll });
  };

  const popstateHandler = () => {
    fetchAndSwap(location.href, { restoreScroll: true });
  };

  document.addEventListener('click', clickHandler);
  window.addEventListener('popstate', popstateHandler);

  setupPrefetch();
  setupSpeculationRules();
  updateActiveLinks();
  applyViewTransitionNames(document);

  // Expose the SPA navigation to `navigate()` (and any embedded UI).
  navigateImpl = (url: string, opts: NavigateOptions = {}) => {
    let sameOrigin = true;
    try {
      sameOrigin = new URL(url, location.href).origin === location.origin;
    } catch {
      sameOrigin = false;
    }
    if (!sameOrigin) {
      location.href = url;
      return;
    }
    fetchAndSwap(new URL(url, location.href).href, opts);
  };

  // Mark the initially-loaded page scripts as disposable so they're removed and
  // re-executed against the fresh DOM when the user navigates back to this
  // page. The shared runtime chunk is the exception and persists.
  document.querySelectorAll('script[src]').forEach((s) => {
    if (!RUNTIME_CHUNK_RE.test((s as HTMLScriptElement).src)) {
      s.setAttribute('data-krate-spa', '');
    }
  });

  routerCleanup = () => {
    document.removeEventListener('click', clickHandler);
    window.removeEventListener('popstate', popstateHandler);
    cleanupPrefetch();
    navigateImpl = null;
  };
}

/** Dispose old effects, re-register router listeners for new page. */
export function reinitRouter(): void {
  if (routerCleanup) {
    routerCleanup();
    routerCleanup = null;
  }
  initialized = false;
  initRouter();
}
