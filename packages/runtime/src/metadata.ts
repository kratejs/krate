// The Next.js Metadata API: `export const metadata` and
// `export async function generateMetadata({ params, searchParams }, parent)` in
// pages and layouts, merged from the root layout down to the page and rendered
// as <head> tags.
//
// The same code runs at build time (once per page, once per generateStaticParams
// entry, in the embedded JS engine or Node) and per request in the SSR renderer
// and the portable handler, so a page gets the same tags either way.
//
// Supported Metadata fields: title (string, { default, template, absolute }),
// description, applicationName, authors, generator, keywords, referrer, creator,
// publisher, robots (string or object, with googleBot), alternates (canonical,
// languages, media, types), openGraph, twitter, icons, manifest, themeColor,
// viewport (string), verification, appleWebApp, formatDetection, category,
// itunes, appLinks, archives, assets, bookmarks and other. metadataBase resolves
// relative URLs. Anything else is ignored.

import { httpFallbackStatus, redirectTarget, thenableRecord } from './request-context.js';

export interface TemplateString {
  default?: string;
  template?: string;
  absolute?: string;
}

export interface OGImage {
  url: string | URL;
  secureUrl?: string | URL;
  alt?: string;
  type?: string;
  width?: string | number;
  height?: string | number;
}

export interface IconDescriptor {
  url: string | URL;
  type?: string;
  sizes?: string;
  color?: string;
  rel?: string;
  media?: string;
  fetchPriority?: 'high' | 'low' | 'auto';
}

type Icon = string | URL | IconDescriptor;

export interface Metadata {
  metadataBase?: string | URL | null;
  title?: string | TemplateString | null;
  description?: string | null;
  applicationName?: string | null;
  authors?: Array<{ name?: string; url?: string | URL }> | { name?: string; url?: string | URL } | null;
  generator?: string | null;
  keywords?: string | string[] | null;
  referrer?: string | null;
  creator?: string | null;
  publisher?: string | null;
  robots?: string | RobotsObject | null;
  alternates?: {
    canonical?: string | URL | null;
    languages?: Record<string, string | URL | null>;
    media?: Record<string, string | URL | null>;
    types?: Record<string, string | URL | null>;
  } | null;
  openGraph?: OpenGraph | null;
  twitter?: Twitter | null;
  icons?: Icon | Icon[] | { icon?: Icon | Icon[]; shortcut?: Icon | Icon[]; apple?: Icon | Icon[]; other?: IconDescriptor | IconDescriptor[] } | null;
  manifest?: string | URL | null;
  themeColor?: string | Array<{ media?: string; color: string }> | null;
  viewport?: string | Viewport | null;
  colorScheme?: string | null;
  verification?: { google?: string | string[]; yahoo?: string | string[]; yandex?: string | string[]; me?: string | string[]; other?: Record<string, string | string[]> } | null;
  appleWebApp?: { capable?: boolean; title?: string; statusBarStyle?: string; startupImage?: string | string[] } | boolean | null;
  formatDetection?: { telephone?: boolean; date?: boolean; address?: boolean; email?: boolean; url?: boolean } | null;
  category?: string | null;
  itunes?: { appId: string; appArgument?: string } | null;
  appLinks?: Record<string, unknown> | null;
  archives?: string | string[] | null;
  assets?: string | string[] | null;
  bookmarks?: string | string[] | null;
  other?: Record<string, string | number | Array<string | number>> | null;
}

export interface RobotsObject {
  index?: boolean;
  follow?: boolean;
  noarchive?: boolean;
  nosnippet?: boolean;
  noimageindex?: boolean;
  nocache?: boolean;
  notranslate?: boolean;
  maxSnippet?: number;
  maxImagePreview?: 'none' | 'standard' | 'large';
  maxVideoPreview?: number | string;
  googleBot?: string | RobotsObject;
}

export interface OpenGraph {
  type?: string;
  url?: string | URL;
  title?: string | TemplateString;
  description?: string;
  siteName?: string;
  locale?: string;
  alternateLocale?: string | string[];
  determiner?: string;
  images?: string | URL | OGImage | Array<string | URL | OGImage>;
  videos?: Array<{ url: string | URL; width?: number; height?: number; type?: string }>;
  audio?: Array<{ url: string | URL; type?: string }>;
  publishedTime?: string;
  modifiedTime?: string;
  expirationTime?: string;
  authors?: string | string[];
  section?: string;
  tags?: string | string[];
  emails?: string | string[];
  phoneNumbers?: string | string[];
  firstName?: string;
  lastName?: string;
  username?: string;
  gender?: string;
  [key: string]: unknown;
}

export interface Twitter {
  card?: 'summary' | 'summary_large_image' | 'app' | 'player';
  site?: string;
  siteId?: string;
  creator?: string;
  creatorId?: string;
  title?: string | TemplateString;
  description?: string;
  images?: string | URL | OGImage | Array<string | URL | OGImage>;
  [key: string]: unknown;
}

export interface Viewport {
  width?: string | number;
  height?: string | number;
  initialScale?: number;
  minimumScale?: number;
  maximumScale?: number;
  userScalable?: boolean;
  viewportFit?: 'auto' | 'cover' | 'contain';
  interactiveWidget?: 'resizes-visual' | 'resizes-content' | 'overlays-content';
  themeColor?: string | Array<{ media?: string; color: string }>;
  colorScheme?: string;
}

/** The resolved viewport of the parent segments. */
export type ResolvingViewport = Promise<Viewport>;

export interface MetadataProps {
  params?: unknown;
  searchParams?: unknown;
}

/** The resolved metadata of the parent segments, as generateMetadata's second argument. */
export type ResolvingMetadata = Promise<Metadata>;

export interface MetadataModule {
  metadata?: Metadata | null;
  /** A static `export const viewport`, merged like metadata. */
  viewport?: Viewport | null;
  generateMetadata?: (props: MetadataProps, parent: ResolvingMetadata) => Metadata | null | undefined | Promise<Metadata | null | undefined>;
}

/** One layer of the chain, root layout first, the page last. */
export interface MetadataSegment {
  /** The module that exports metadata or generateMetadata. */
  mod?: MetadataModule | null;
  /** Name for log lines ("src/pages/blog/_layout.tsx"). */
  file?: string;
  /** Metadata a file convention contributes (opengraph-image, icon, ...), under the segment's own export. */
  files?: Metadata;
}

export interface ResolvedMetadata {
  metadata: Metadata;
  /** The title templates a segment declared for openGraph.title and twitter.title. */
  templates: { openGraph?: string; twitter?: string };
  /** Errors thrown by generateMetadata functions; the page renders without that segment's metadata. */
  errors: Array<{ file: string; error: unknown }>;
}

function isThenable(v: unknown): boolean {
  return !!v && typeof (v as { then?: unknown }).then === 'function';
}

const MERGE_KEYS = [
  'title', 'description', 'applicationName', 'authors', 'generator', 'keywords', 'referrer', 'creator', 'publisher',
  'robots', 'alternates', 'openGraph', 'twitter', 'icons', 'manifest', 'themeColor', 'viewport', 'colorScheme', 'verification',
  'appleWebApp', 'formatDetection', 'category', 'itunes', 'appLinks', 'archives', 'assets', 'bookmarks', 'other',
] as const;

interface Merged {
  metadata: Metadata;
  /** Title templates, per field: openGraph and twitter titles have their own. */
  titleTemplate?: string;
  openGraphTemplate?: string;
  twitterTemplate?: string;
  base?: string;
}

function mergeSegment(acc: Merged, next: Metadata): Merged {
  const out: Merged = { ...acc, metadata: { ...acc.metadata } };
  if (next.metadataBase !== undefined) {
    out.base = next.metadataBase ? String(next.metadataBase) : undefined;
    out.metadata.metadataBase = next.metadataBase;
  }
  for (const key of MERGE_KEYS) {
    if (!(key in next)) continue;
    const value = (next as Record<string, unknown>)[key];
    if (key === 'title') {
      const t = value as Metadata['title'];
      if (t === null || t === undefined) {
        out.metadata.title = t;
      } else if (typeof t === 'string') {
        out.metadata.title = out.titleTemplate ? out.titleTemplate.replace('%s', t) : t;
      } else {
        if (t.template !== undefined) out.titleTemplate = t.template;
        if (t.absolute !== undefined) out.metadata.title = t.absolute;
        else if (t.default !== undefined) out.metadata.title = t.default;
      }
      continue;
    }
    if (key === 'openGraph' || key === 'twitter') {
      const t = value && typeof value === 'object' ? (value as { title?: unknown }).title : undefined;
      if (t && typeof t === 'object' && (t as TemplateString).template !== undefined) {
        if (key === 'openGraph') out.openGraphTemplate = (t as TemplateString).template;
        else out.twitterTemplate = (t as TemplateString).template;
      }
    }
    (out.metadata as Record<string, unknown>)[key] = value;
  }
  return out;
}

/**
 * Resolves the chain: each segment's metadata (an export or the result of
 * generateMetadata, which gets { params, searchParams } and a promise of the
 * parents' merged metadata), merged root first. A deeper segment replaces a
 * field wholesale; a title string takes the nearest ancestor's template. A
 * generateMetadata that throws is logged and skipped, so one failing lookup
 * never costs the page.
 */
export async function resolveMetadata(segments: MetadataSegment[], rawProps: MetadataProps): Promise<ResolvedMetadata> {
  // params and searchParams read both ways (props.params.slug and
  // (await props.params).slug), like a page's.
  const props: MetadataProps = {
    params: isThenable(rawProps.params) ? rawProps.params : thenableRecord((rawProps.params as Record<string, string>) || {}),
    searchParams: isThenable(rawProps.searchParams) ? rawProps.searchParams : thenableRecord((rawProps.searchParams as Record<string, string>) || {}),
  };
  let acc: Merged = { metadata: {} };
  const errors: ResolvedMetadata['errors'] = [];
  for (const seg of segments) {
    const mod = seg.mod;
    let own: Metadata | null | undefined;
    if (mod && typeof mod.generateMetadata === 'function') {
      const parent = Promise.resolve({ ...acc.metadata });
      try {
        own = await mod.generateMetadata(props, parent);
      } catch (error) {
        // notFound(), forbidden(), unauthorized() and redirect() in
        // generateMetadata answer the request; any other error costs only the
        // segment's metadata.
        if (httpFallbackStatus(error) || redirectTarget(error)) throw error;
        errors.push({ file: seg.file || '', error });
        own = undefined;
      }
    } else if (mod && mod.metadata && typeof mod.metadata === 'object') {
      own = mod.metadata;
    }
    if (own) acc = mergeSegment(acc, own);
    if (mod && mod.viewport && typeof mod.viewport === 'object') {
      const { themeColor, colorScheme, ...rest } = mod.viewport;
      acc = mergeSegment(acc, { viewport: rest, ...(themeColor !== undefined ? { themeColor } : {}), ...(colorScheme !== undefined ? { colorScheme } : {}) });
    }
    if (seg.files) acc = mergeFiles(acc, fillParams(seg.files, rawProps.params), own);
  }
  return { metadata: acc.metadata, templates: { openGraph: acc.openGraphTemplate, twitter: acc.twitterTemplate }, errors };
}

// Fills [param], [...param] and [[...param]] placeholders in the URLs of a file
// convention's metadata with the request's params (an image route in a dynamic
// segment is built once per param set, at a URL named after it).
function fillParams(files: Metadata, params: unknown): Metadata {
  const values = (params && typeof params === 'object' ? params : {}) as Record<string, string>;
  const fill = (url: string): string =>
    url.replace(/\/?\[\[?(?:\.\.\.)?([^\]]+)\]\]?/g, (_m, name: string) => (values[name] ? '/' + values[name] : ''));
  const fixImages = (images: unknown): unknown =>
    Array.isArray(images) ? images.map((i) => (i && typeof i === 'object' && typeof (i as { url?: unknown }).url === 'string' ? { ...i, url: fill((i as { url: string }).url) } : i)) : images;
  const out: Metadata = { ...files };
  if (files.openGraph) out.openGraph = { ...files.openGraph, images: fixImages(files.openGraph.images) as OpenGraph['images'] };
  if (files.twitter) out.twitter = { ...files.twitter, images: fixImages(files.twitter.images) as Twitter['images'] };
  return out;
}

// A file convention fills the fields the segment's own metadata left out.
function mergeFiles(acc: Merged, files: Metadata, own: Metadata | null | undefined): Merged {
  const out: Merged = { ...acc, metadata: { ...acc.metadata } };
  if (files.openGraph && files.openGraph.images !== undefined && !(own && own.openGraph && own.openGraph.images !== undefined)) {
    out.metadata.openGraph = { ...(out.metadata.openGraph || {}), images: files.openGraph.images };
  }
  if (files.twitter && files.twitter.images !== undefined && !(own && own.twitter && own.twitter.images !== undefined)) {
    out.metadata.twitter = { card: 'summary_large_image', ...(out.metadata.twitter || {}), images: files.twitter.images };
  }
  if (files.icons !== undefined && !(own && own.icons !== undefined)) out.metadata.icons = files.icons;
  if (files.manifest !== undefined && !(own && own.manifest !== undefined)) out.metadata.manifest = files.manifest;
  return out;
}

// Rendering

function esc(value: unknown): string {
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/"/g, '&quot;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

function escText(value: unknown): string {
  return String(value).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

function toArray<T>(v: T | T[] | null | undefined): T[] {
  if (v === null || v === undefined) return [];
  return Array.isArray(v) ? v : [v];
}

/** Resolves a URL field against metadataBase (its path is a prefix). Absolute URLs stay as they are. */
function resolveBaseJoin(value: string | URL | null | undefined, base: string | undefined): string {
  if (value === null || value === undefined || value === '') return '';
  const s = String(value);
  if (!base || /^[a-z][a-z0-9+.-]*:/i.test(s) || s.startsWith('//')) return s;
  try {
    const b = new URL(base);
    // metadataBase's path is a prefix: "/docs" + "/a" = "/docs/a".
    const prefix = b.pathname.replace(/\/$/, '');
    const path = s.startsWith('/') ? s : '/' + s;
    return b.origin + prefix + path;
  } catch {
    return s;
  }
}

function meta(attr: 'name' | 'property' | 'http-equiv', key: string, content: unknown): string {
  if (content === undefined || content === null || content === '') return '';
  return `<meta ${attr}="${esc(key)}" content="${esc(content)}">`;
}

function link(attrs: Record<string, string | undefined>): string {
  const parts = Object.entries(attrs)
    .filter(([, v]) => v !== undefined && v !== '')
    .map(([k, v]) => `${k}="${esc(v)}"`);
  return `<link ${parts.join(' ')}>`;
}

function viewportString(v: Viewport): string {
  const parts: string[] = [];
  const add = (k: string, val: unknown) => {
    if (val !== undefined && val !== null && val !== '') parts.push(`${k}=${val}`);
  };
  add('width', v.width);
  add('height', v.height);
  add('initial-scale', v.initialScale);
  add('minimum-scale', v.minimumScale);
  add('maximum-scale', v.maximumScale);
  if (v.userScalable !== undefined) add('user-scalable', v.userScalable ? 'yes' : 'no');
  add('viewport-fit', v.viewportFit);
  add('interactive-widget', v.interactiveWidget);
  return parts.join(', ');
}

function robotsString(r: string | RobotsObject): string {
  if (typeof r === 'string') return r;
  const out: string[] = [];
  out.push(r.index === false ? 'noindex' : 'index');
  out.push(r.follow === false ? 'nofollow' : 'follow');
  if (r.noarchive) out.push('noarchive');
  if (r.nosnippet) out.push('nosnippet');
  if (r.noimageindex) out.push('noimageindex');
  if (r.nocache) out.push('nocache');
  if (r.notranslate) out.push('notranslate');
  if (r.maxSnippet !== undefined) out.push(`max-snippet:${r.maxSnippet}`);
  if (r.maxImagePreview !== undefined) out.push(`max-image-preview:${r.maxImagePreview}`);
  if (r.maxVideoPreview !== undefined) out.push(`max-video-preview:${r.maxVideoPreview}`);
  return out.join(', ');
}

function imageList(images: OpenGraph['images'], base: string | undefined): OGImage[] {
  return toArray(images).map((img) => {
    const o: OGImage = typeof img === 'string' || img instanceof URL ? { url: img } : { ...img };
    o.url = resolveBaseJoin(o.url, base);
    if (o.secureUrl) o.secureUrl = resolveBaseJoin(o.secureUrl, base);
    return o;
  });
}

function titleOf(t: string | TemplateString | undefined | null, template: string | undefined): string {
  if (t === undefined || t === null) return '';
  if (typeof t === 'string') return template ? template.replace('%s', t) : t;
  return t.absolute !== undefined ? t.absolute : t.default || '';
}

/** The <head> tags for resolved metadata, one per line. */
export function renderMetadataTags(md: Metadata, templates: { openGraph?: string; twitter?: string } = {}): string {
  const base = md.metadataBase ? String(md.metadataBase) : undefined;
  const tags: string[] = [];
  const push = (t: string) => {
    if (t) tags.push(t);
  };

  if (typeof md.title === 'string' && md.title !== '') push(`<title>${escText(md.title)}</title>`);
  push(meta('name', 'description', md.description));
  push(meta('name', 'application-name', md.applicationName));
  for (const a of toArray(md.authors)) {
    if (a.name) push(meta('name', 'author', a.name));
    if (a.url) push(link({ rel: 'author', href: resolveBaseJoin(a.url, base) }));
  }
  push(meta('name', 'generator', md.generator));
  if (md.keywords) push(meta('name', 'keywords', toArray(md.keywords).join(',')));
  push(meta('name', 'referrer', md.referrer));
  if (md.themeColor) {
    if (typeof md.themeColor === 'string') push(meta('name', 'theme-color', md.themeColor));
    else
      for (const t of md.themeColor)
        push(`<meta name="theme-color" content="${esc(t.color)}"${t.media ? ` media="${esc(t.media)}"` : ''}>`);
  }
  push(meta('name', 'color-scheme', md.colorScheme));
  if (md.viewport) push(meta('name', 'viewport', typeof md.viewport === 'string' ? md.viewport : viewportString(md.viewport)));
  push(meta('name', 'creator', md.creator));
  push(meta('name', 'publisher', md.publisher));
  if (md.robots) {
    push(meta('name', 'robots', robotsString(md.robots)));
    if (typeof md.robots === 'object' && md.robots.googleBot) push(meta('name', 'googlebot', robotsString(md.robots.googleBot)));
  }
  if (md.manifest) push(link({ rel: 'manifest', href: resolveBaseJoin(md.manifest, base) }));
  if (md.alternates) {
    const a = md.alternates;
    if (a.canonical) push(link({ rel: 'canonical', href: resolveBaseJoin(a.canonical, base) }));
    for (const [lang, url] of Object.entries(a.languages || {})) if (url) push(link({ rel: 'alternate', hreflang: lang, href: resolveBaseJoin(url, base) }));
    for (const [media, url] of Object.entries(a.media || {})) if (url) push(link({ rel: 'alternate', media, href: resolveBaseJoin(url, base) }));
    for (const [type, url] of Object.entries(a.types || {})) if (url) push(link({ rel: 'alternate', type, href: resolveBaseJoin(url, base) }));
  }
  for (const [rel, urls] of [['archives', md.archives], ['assets', md.assets], ['bookmarks', md.bookmarks]] as const)
    for (const u of toArray(urls as string | string[] | undefined | null)) push(link({ rel: rel === 'archives' ? 'archives' : rel === 'assets' ? 'assets' : 'bookmarks', href: resolveBaseJoin(u, base) }));
  push(meta('name', 'category', md.category));
  if (md.itunes) push(meta('name', 'apple-itunes-app', `app-id=${md.itunes.appId}${md.itunes.appArgument ? `, app-argument=${md.itunes.appArgument}` : ''}`));

  if (md.icons) renderIcons(md.icons, base, push);
  if (md.verification) {
    const v = md.verification;
    for (const x of toArray(v.google)) push(meta('name', 'google-site-verification', x));
    for (const x of toArray(v.yahoo)) push(meta('name', 'y_key', x));
    for (const x of toArray(v.yandex)) push(meta('name', 'yandex-verification', x));
    for (const x of toArray(v.me)) push(meta('name', 'me', x));
    for (const [k, vals] of Object.entries(v.other || {})) for (const x of toArray(vals)) push(meta('name', k, x));
  }
  if (md.appleWebApp) {
    const w = md.appleWebApp === true ? { capable: true } : md.appleWebApp;
    if (w) {
      if (w.capable !== false) push(meta('name', 'mobile-web-app-capable', 'yes'));
      push(meta('name', 'apple-mobile-web-app-title', (w as { title?: string }).title));
      push(meta('name', 'apple-mobile-web-app-status-bar-style', (w as { statusBarStyle?: string }).statusBarStyle));
      for (const img of toArray((w as { startupImage?: string | string[] }).startupImage)) push(link({ rel: 'apple-touch-startup-image', href: resolveBaseJoin(img, base) }));
    }
  }
  if (md.formatDetection) {
    const f = md.formatDetection;
    const parts = (['telephone', 'date', 'address', 'email', 'url'] as const).filter((k) => f[k] === false).map((k) => `${k}=no`);
    if (parts.length) push(meta('name', 'format-detection', parts.join(', ')));
  }
  if (md.openGraph) renderOpenGraph(md.openGraph, base, templates.openGraph, push);
  if (md.twitter) renderTwitter(md.twitter, base, templates.twitter, push);
  if (md.appLinks) {
    for (const [platform, entries] of Object.entries(md.appLinks))
      for (const entry of toArray(entries as Record<string, unknown> | Array<Record<string, unknown>>))
        for (const [k, v] of Object.entries(entry || {})) push(meta('property', `al:${platform}:${k}`, v));
  }
  for (const [k, vals] of Object.entries(md.other || {})) for (const x of toArray(vals)) push(meta('name', k, x));
  return tags.join('\n');
}

function renderIcons(icons: NonNullable<Metadata['icons']>, base: string | undefined, push: (t: string) => void): void {
  const asDesc = (i: Icon, rel: string): IconDescriptor => (typeof i === 'string' || i instanceof URL ? { url: i, rel } : { rel, ...i });
  const emit = (list: Icon | Icon[] | undefined, rel: string) => {
    for (const i of toArray(list)) {
      const d = asDesc(i, rel);
      push(link({ rel: d.rel || rel, href: resolveBaseJoin(d.url, base), type: d.type, sizes: d.sizes, media: d.media, color: d.color, fetchpriority: d.fetchPriority }));
    }
  };
  if (typeof icons === 'string' || icons instanceof URL || Array.isArray(icons)) {
    emit(icons as Icon | Icon[], 'icon');
    return;
  }
  const o = icons as { icon?: Icon | Icon[]; shortcut?: Icon | Icon[]; apple?: Icon | Icon[]; other?: IconDescriptor | IconDescriptor[] };
  if ('url' in (icons as object) && !('icon' in o)) {
    emit(icons as IconDescriptor, 'icon');
    return;
  }
  emit(o.icon, 'icon');
  emit(o.shortcut, 'shortcut icon');
  emit(o.apple, 'apple-touch-icon');
  emit(o.other, 'icon');
}

function renderOpenGraph(og: OpenGraph, base: string | undefined, template: string | undefined, push: (t: string) => void): void {
  push(meta('property', 'og:title', titleOf(og.title as string | TemplateString | undefined, template)));
  push(meta('property', 'og:description', og.description));
  push(meta('property', 'og:url', resolveBaseJoin(og.url, base)));
  push(meta('property', 'og:site_name', og.siteName));
  push(meta('property', 'og:locale', og.locale));
  for (const l of toArray(og.alternateLocale)) push(meta('property', 'og:locale:alternate', l));
  push(meta('property', 'og:determiner', og.determiner));
  for (const img of imageList(og.images, base)) {
    push(meta('property', 'og:image', img.url));
    push(meta('property', 'og:image:secure_url', img.secureUrl));
    push(meta('property', 'og:image:type', img.type));
    push(meta('property', 'og:image:width', img.width));
    push(meta('property', 'og:image:height', img.height));
    push(meta('property', 'og:image:alt', img.alt));
  }
  for (const v of toArray(og.videos)) {
    push(meta('property', 'og:video', resolveBaseJoin(v.url, base)));
    push(meta('property', 'og:video:type', v.type));
    push(meta('property', 'og:video:width', v.width));
    push(meta('property', 'og:video:height', v.height));
  }
  for (const a of toArray(og.audio)) {
    push(meta('property', 'og:audio', resolveBaseJoin(a.url, base)));
    push(meta('property', 'og:audio:type', a.type));
  }
  push(meta('property', 'og:type', og.type));
  if (og.type === 'article') {
    push(meta('property', 'article:published_time', og.publishedTime));
    push(meta('property', 'article:modified_time', og.modifiedTime));
    push(meta('property', 'article:expiration_time', og.expirationTime));
    for (const a of toArray(og.authors)) push(meta('property', 'article:author', a));
    push(meta('property', 'article:section', og.section));
    for (const t of toArray(og.tags)) push(meta('property', 'article:tag', t));
  }
  if (og.type === 'profile') {
    push(meta('property', 'profile:first_name', og.firstName));
    push(meta('property', 'profile:last_name', og.lastName));
    push(meta('property', 'profile:username', og.username));
    push(meta('property', 'profile:gender', og.gender));
  }
}

function renderTwitter(tw: Twitter, base: string | undefined, template: string | undefined, push: (t: string) => void): void {
  const images = imageList(tw.images, base);
  const card = tw.card || (images.length > 0 ? 'summary_large_image' : 'summary');
  push(meta('name', 'twitter:card', card));
  push(meta('name', 'twitter:site', tw.site));
  push(meta('name', 'twitter:site:id', tw.siteId));
  push(meta('name', 'twitter:creator', tw.creator));
  push(meta('name', 'twitter:creator:id', tw.creatorId));
  push(meta('name', 'twitter:title', titleOf(tw.title as string | TemplateString | undefined, template)));
  push(meta('name', 'twitter:description', tw.description));
  for (const img of images) {
    push(meta('name', 'twitter:image', img.url));
    push(meta('name', 'twitter:image:alt', img.alt));
  }
}

/**
 * The head markup of a chain for these props, or "" when the chain exports no
 * metadata. Errors from generateMetadata are logged and the segment skipped.
 */
export async function metadataHeadTags(segments: MetadataSegment[], props: MetadataProps, log?: (msg: string, err: unknown) => void): Promise<string> {
  if (!segments.some((s) => s.mod || s.files)) return '';
  const { metadata, templates, errors } = await resolveMetadata(segments, props);
  for (const e of errors) (log || defaultLog)(`generateMetadata${e.file ? ` in ${e.file}` : ''} failed; the page renders without its metadata`, e.error);
  return renderMetadataTags(metadata, templates);
}

function defaultLog(msg: string, err: unknown): void {
  console.error(`[krate] ${msg}:`, err);
}
