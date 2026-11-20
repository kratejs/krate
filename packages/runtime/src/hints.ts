// React DOM's resource hints (preload, preinit, preconnect, prefetchDNS,
// preloadModule, preinitModule). React emits <link>/<script> tags into the
// document head for them. In the browser they add the tag once, and nothing
// when the document already has it (the server HTML put it there). While the
// server renders, they record the tag on the render scope and the renderer
// puts the recorded tags into the <head> of the response (withCacheScope).
// When the build renders an island outside any request, they record the tag
// in the set the build installs as globalThis.__krate_island_hints, and the
// build puts it into the head of the page.
// This module must stay free of imports: island bundles inline it.

export interface HintOptions {
  as?: string;
  crossOrigin?: string;
  integrity?: string;
  type?: string;
  media?: string;
  nonce?: string;
  fetchPriority?: string;
  referrerPolicy?: string;
  imageSrcSet?: string;
  imageSizes?: string;
  precedence?: string;
}

type Attrs = Array<[string, string]>;

function optAttrs(opts: HintOptions | undefined, names: Array<keyof HintOptions>): Attrs {
  const out: Attrs = [];
  if (!opts) return out;
  for (const name of names) {
    const v: any = opts[name];
    if (v === undefined || v === null || v === false) continue;
    // crossOrigin: "" is a valid value (anonymous); the others need text.
    if (v === '' && name !== 'crossOrigin') continue;
    const attr =
      name === 'crossOrigin' ? 'crossorigin' :
      name === 'fetchPriority' ? 'fetchpriority' :
      name === 'referrerPolicy' ? 'referrerpolicy' :
      name === 'imageSrcSet' ? 'imagesrcset' :
      name === 'imageSizes' ? 'imagesizes' : name;
    if (name === 'crossOrigin') {
      // As React: "use-credentials" or anonymous (""), so react-dom in a
      // real-React island finds the tag by the same selector.
      out.push([attr, v === 'use-credentials' ? v : '']);
      continue;
    }
    out.push([attr, v === true ? '' : String(v)]);
  }
  return out;
}

function escapeAttr(v: string): string {
  return v.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

interface RenderScopeLike {
  hints?: Set<string>;
}

// The scope of the render that is running on the server, if any. The cache
// runtime keeps it on globalThis, so every copy of this module sees it.
function serverScope(): RenderScopeLike | undefined {
  const rt = (globalThis as any)[Symbol.for('krate.cache.runtime')];
  const store = rt && rt.renderStore;
  return store && typeof store.getStore === 'function' ? store.getStore() : undefined;
}

// serverHints is where a hint called outside a browser goes: the running
// render's scope, else the build's island collector, else nowhere.
function serverHints(): Set<string> | undefined {
  const scope = serverScope();
  if (scope) return (scope.hints ??= new Set());
  const c = (globalThis as any).__krate_island_hints;
  return c instanceof Set ? c : undefined;
}

function absURL(href: string): string {
  try {
    return new URL(href, document.baseURI).href;
  } catch {
    return href;
  }
}

// present reports whether the document already has the tag: the same rel (or
// script type) and the same URL, however it is spelled. A preload also needs
// the same `as`, as in React.
function present(tag: 'link' | 'script', attrs: Attrs): boolean {
  const get = (name: string) => {
    for (const [k, v] of attrs) if (k === name) return v;
  };
  if (tag === 'script') {
    const src = absURL(get('src') || '');
    const module = get('type') === 'module';
    for (const el of Array.from(document.querySelectorAll('script[src]')) as HTMLScriptElement[]) {
      if (el.src === src && (el.type === 'module') === module) return true;
    }
    return false;
  }
  const rel = get('rel') || '';
  const href = absURL(get('href') || '');
  const as = get('as');
  for (const el of Array.from(document.querySelectorAll('link[href]')) as HTMLLinkElement[]) {
    if (el.href !== href) continue;
    if ((el.getAttribute('rel') || '').toLowerCase() !== rel) continue;
    if (rel === 'preload' && (el.getAttribute('as') || '') !== (as || '')) continue;
    return true;
  }
  return false;
}

function tagText(tag: 'link' | 'script', attrs: Attrs): string {
  const text = attrs.map(([k, v]) => (v === '' && k !== 'crossorigin' ? ' ' + k : ' ' + k + '="' + escapeAttr(v) + '"')).join('');
  return tag === 'link' ? '<link' + text + '>' : '<script' + text + '></script>';
}

// emit adds one head tag: to the document in a browser, to the render scope
// (or the build's collector) on the server. A tag that is already there is
// not added twice.
function emit(tag: 'link' | 'script', attrs: Attrs): void {
  if (typeof document === 'undefined') {
    serverHints()?.add(tagText(tag, attrs));
    return;
  }
  if (present(tag, attrs)) return;
  const el = document.createElement(tag);
  for (const [k, v] of attrs) el.setAttribute(k, v);
  document.head.appendChild(el);
}

function link(rel: string, href: string, opts: HintOptions | undefined, names: Array<keyof HintOptions>): void {
  if (!href || typeof href !== 'string') return;
  emit('link', [['rel', rel], ['href', href], ...optAttrs(opts, names)]);
}

/**
 * Records head tags the build collected for a component that the server does
 * not render (a real-React client component). Server only; the browser
 * already has them in the document.
 */
export function krateHintTags(tags: readonly string[]): void {
  if (typeof document !== 'undefined') return;
  const hints = serverHints();
  if (hints) for (const t of tags) hints.add(t);
}

export function krateHintPreload(href: string, opts?: HintOptions): void {
  link('preload', href, opts, ['as', 'crossOrigin', 'integrity', 'type', 'media', 'nonce', 'fetchPriority', 'referrerPolicy', 'imageSrcSet', 'imageSizes']);
}

export function krateHintPreloadModule(href: string, opts?: HintOptions): void {
  link('modulepreload', href, opts, ['as', 'crossOrigin', 'integrity', 'nonce']);
}

export function krateHintPreconnect(href: string, opts?: HintOptions): void {
  link('preconnect', href, opts, ['crossOrigin']);
}

export function krateHintPrefetchDNS(href: string): void {
  link('dns-prefetch', href, undefined, []);
}

export function krateHintPreinit(href: string, opts?: HintOptions): void {
  if (!href || typeof href !== 'string') return;
  if (opts && opts.as === 'script') {
    emit('script', [['src', href], ['async', ''], ...optAttrs(opts, ['crossOrigin', 'integrity', 'nonce', 'fetchPriority'])]);
    return;
  }
  // as: "style" (the default use) loads the stylesheet.
  link('stylesheet', href, opts, ['crossOrigin', 'integrity', 'media', 'nonce', 'fetchPriority']);
}

export function krateHintPreinitModule(href: string, opts?: HintOptions): void {
  if (!href || typeof href !== 'string') return;
  emit('script', [['type', 'module'], ['src', href], ['async', ''], ...optAttrs(opts, ['crossOrigin', 'integrity', 'nonce'])]);
}

// The react-dom exports a server module can use. flushSync(fn) is fn() here.
export const preload = krateHintPreload;
export const preinit = krateHintPreinit;
export const preconnect = krateHintPreconnect;
export const prefetchDNS = krateHintPrefetchDNS;
export const preloadModule = krateHintPreloadModule;
export const preinitModule = krateHintPreinitModule;
export function flushSync<T>(fn: () => T): T {
  return fn();
}

export default { preload, preinit, preconnect, prefetchDNS, preloadModule, preinitModule, flushSync };
