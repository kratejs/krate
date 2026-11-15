// React DOM's resource hints (preload, preinit, preconnect, prefetchDNS,
// preloadModule, preinitModule). React emits <link>/<script> tags into the
// document head for them. In the browser they add the tag once. While the
// server renders, they record the tag on the render scope and the renderer
// puts the recorded tags into the <head> of the response (withCacheScope).
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

function cssEscape(v: string): string {
  return (window as any).CSS && CSS.escape ? CSS.escape(v) : v.replace(/["\\]/g, '\\$&');
}

// emit adds one head tag: to the document in a browser, to the render scope
// on the server. A tag that is already there is not added twice.
function emit(tag: 'link' | 'script', attrs: Attrs, key: string): void {
  if (typeof document === 'undefined') {
    const scope = serverScope();
    if (!scope) return;
    const text =
      tag === 'link'
        ? '<link' + attrs.map(([k, v]) => (v === '' && k !== 'crossorigin' ? ' ' + k : ' ' + k + '="' + escapeAttr(v) + '"')).join('') + '>'
        : '<script' + attrs.map(([k, v]) => (v === '' && k !== 'crossorigin' ? ' ' + k : ' ' + k + '="' + escapeAttr(v) + '"')).join('') + '></script>';
    (scope.hints ??= new Set()).add(text);
    return;
  }
  if (document.head.querySelector(key)) return;
  const el = document.createElement(tag);
  for (const [k, v] of attrs) el.setAttribute(k, v);
  document.head.appendChild(el);
}

function linkKey(rel: string, href: string): string {
  return 'link[rel="' + rel + '"][href="' + cssEscape(href) + '"]';
}

function link(rel: string, href: string, opts: HintOptions | undefined, names: Array<keyof HintOptions>): void {
  if (!href || typeof href !== 'string') return;
  emit('link', [['rel', rel], ['href', href], ...optAttrs(opts, names)], typeof document === 'undefined' ? '' : linkKey(rel, href));
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
    emit('script', [['src', href], ['async', ''], ...optAttrs(opts, ['crossOrigin', 'integrity', 'nonce', 'fetchPriority'])], 'script[src="' + (typeof document === 'undefined' ? '' : cssEscape(href)) + '"]');
    return;
  }
  // as: "style" (the default use) loads the stylesheet.
  link('stylesheet', href, opts, ['crossOrigin', 'integrity', 'media', 'nonce', 'fetchPriority']);
}

export function krateHintPreinitModule(href: string, opts?: HintOptions): void {
  if (!href || typeof href !== 'string') return;
  emit('script', [['type', 'module'], ['src', href], ['async', ''], ...optAttrs(opts, ['crossOrigin', 'integrity', 'nonce'])], 'script[type="module"][src="' + (typeof document === 'undefined' ? '' : cssEscape(href)) + '"]');
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
