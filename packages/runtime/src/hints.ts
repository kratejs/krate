// React DOM's resource hints (preload, preinit, preconnect, prefetchDNS,
// preloadModule, preinitModule). React emits <link>/<script> tags into the
// document head for them; on the client they add the tag once.

interface HintOptions {
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
}

function headHas(selector: string): boolean {
  return !!document.head.querySelector(selector);
}

function addLink(rel: string, href: string, opts?: HintOptions): void {
  if (typeof document === 'undefined' || !href) return;
  const sel = 'link[rel="' + rel + '"][href="' + (window.CSS && CSS.escape ? CSS.escape(href) : href) + '"]';
  if (headHas(sel)) return;
  const link = document.createElement('link');
  link.rel = rel;
  link.href = href;
  if (opts) {
    if (opts.as) link.as = opts.as;
    if (opts.crossOrigin !== undefined) link.crossOrigin = opts.crossOrigin;
    if (opts.integrity) link.integrity = opts.integrity;
    if (opts.type) link.type = opts.type;
    if (opts.media) link.media = opts.media;
    if (opts.nonce) link.nonce = opts.nonce;
    if (opts.fetchPriority) link.setAttribute('fetchpriority', opts.fetchPriority);
    if (opts.referrerPolicy) link.referrerPolicy = opts.referrerPolicy;
    if (opts.imageSrcSet) link.setAttribute('imagesrcset', opts.imageSrcSet);
    if (opts.imageSizes) link.setAttribute('imagesizes', opts.imageSizes);
  }
  document.head.appendChild(link);
}

export function krateHintPreload(href: string, opts?: HintOptions): void {
  addLink('preload', href, opts);
}

export function krateHintPreloadModule(href: string, opts?: HintOptions): void {
  addLink('modulepreload', href, opts);
}

export function krateHintPreconnect(href: string, opts?: HintOptions): void {
  addLink('preconnect', href, opts);
}

export function krateHintPrefetchDNS(href: string): void {
  addLink('dns-prefetch', href);
}

export function krateHintPreinit(href: string, opts?: HintOptions): void {
  if (typeof document === 'undefined' || !href) return;
  if (opts && opts.as === 'script') {
    if (headHas('script[src="' + href + '"]')) return;
    const s = document.createElement('script');
    s.src = href;
    s.async = true;
    if (opts.crossOrigin !== undefined) s.crossOrigin = opts.crossOrigin;
    if (opts.integrity) s.integrity = opts.integrity;
    if (opts.nonce) s.nonce = opts.nonce;
    document.head.appendChild(s);
    return;
  }
  // as: "style" (the default use): load the stylesheet.
  if (headHas('link[rel="stylesheet"][href="' + href + '"]')) return;
  const link = document.createElement('link');
  link.rel = 'stylesheet';
  link.href = href;
  if (opts && opts.crossOrigin !== undefined) link.crossOrigin = opts.crossOrigin;
  if (opts && opts.integrity) link.integrity = opts.integrity;
  if (opts && opts.media) link.media = opts.media;
  document.head.appendChild(link);
}

export function krateHintPreinitModule(href: string, opts?: HintOptions): void {
  if (typeof document === 'undefined' || !href) return;
  if (headHas('script[type="module"][src="' + href + '"]')) return;
  const s = document.createElement('script');
  s.type = 'module';
  s.src = href;
  if (opts && opts.crossOrigin !== undefined) s.crossOrigin = opts.crossOrigin;
  if (opts && opts.integrity) s.integrity = opts.integrity;
  document.head.appendChild(s);
}
