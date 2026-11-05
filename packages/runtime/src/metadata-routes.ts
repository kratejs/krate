// Metadata route files: sitemap.ts, robots.ts and manifest.ts return plain data
// (the shapes of Next.js's MetadataRoute); Krate renders them to sitemap.xml,
// robots.txt and manifest.webmanifest at build time.

export interface SitemapEntry {
  url: string;
  lastModified?: string | Date;
  changeFrequency?: 'always' | 'hourly' | 'daily' | 'weekly' | 'monthly' | 'yearly' | 'never';
  priority?: number;
  alternates?: { languages?: Record<string, string> };
  images?: string[];
  videos?: Array<{ title: string; thumbnail_loc: string; description: string; content_loc?: string; player_loc?: string }>;
}

export interface RobotsRule {
  userAgent?: string | string[];
  allow?: string | string[];
  disallow?: string | string[];
  crawlDelay?: number;
}

export interface RobotsFile {
  rules: RobotsRule | RobotsRule[];
  sitemap?: string | string[];
  host?: string;
}

export interface ManifestFile {
  name?: string;
  short_name?: string;
  description?: string;
  start_url?: string;
  display?: 'fullscreen' | 'standalone' | 'minimal-ui' | 'browser';
  background_color?: string;
  theme_color?: string;
  orientation?: string;
  scope?: string;
  lang?: string;
  dir?: string;
  id?: string;
  categories?: string[];
  icons?: Array<{ src: string; sizes?: string; type?: string; purpose?: string }>;
  screenshots?: Array<{ src: string; sizes?: string; type?: string; form_factor?: string; label?: string }>;
  shortcuts?: Array<{ name: string; url: string; short_name?: string; description?: string; icons?: ManifestFile['icons'] }>;
  [key: string]: unknown;
}

// eslint-disable-next-line @typescript-eslint/no-namespace
export namespace MetadataRoute {
  export type Sitemap = SitemapEntry[];
  export type Robots = RobotsFile;
  export type Manifest = ManifestFile;
}

function esc(s: unknown): string {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&apos;');
}

function dateString(d: string | Date): string {
  return d instanceof Date ? d.toISOString() : String(d);
}

/** The XML of a sitemap, with the xhtml, image and video namespaces only when used. */
export function renderSitemapXML(entries: SitemapEntry[]): string {
  const hasAlt = entries.some((e) => e.alternates && e.alternates.languages);
  const hasImg = entries.some((e) => e.images && e.images.length);
  const hasVideo = entries.some((e) => e.videos && e.videos.length);
  let xml = '<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"';
  if (hasImg) xml += ' xmlns:image="http://www.google.com/schemas/sitemap-image/1.1"';
  if (hasVideo) xml += ' xmlns:video="http://www.google.com/schemas/sitemap-video/1.1"';
  if (hasAlt) xml += ' xmlns:xhtml="http://www.w3.org/1999/xhtml"';
  xml += '>\n';
  for (const e of entries) {
    if (!e || typeof e.url !== 'string' || e.url === '') throw new TypeError('sitemap entries need a url');
    xml += `  <url>\n    <loc>${esc(e.url)}</loc>\n`;
    for (const [lang, href] of Object.entries((e.alternates && e.alternates.languages) || {})) {
      xml += `    <xhtml:link rel="alternate" hreflang="${esc(lang)}" href="${esc(href)}" />\n`;
    }
    if (e.lastModified) xml += `    <lastmod>${esc(dateString(e.lastModified))}</lastmod>\n`;
    if (e.changeFrequency) xml += `    <changefreq>${esc(e.changeFrequency)}</changefreq>\n`;
    if (e.priority !== undefined) xml += `    <priority>${esc(e.priority)}</priority>\n`;
    for (const img of e.images || []) xml += `    <image:image>\n      <image:loc>${esc(img)}</image:loc>\n    </image:image>\n`;
    for (const v of e.videos || []) {
      xml += `    <video:video>\n      <video:title>${esc(v.title)}</video:title>\n      <video:thumbnail_loc>${esc(v.thumbnail_loc)}</video:thumbnail_loc>\n      <video:description>${esc(v.description)}</video:description>\n`;
      if (v.content_loc) xml += `      <video:content_loc>${esc(v.content_loc)}</video:content_loc>\n`;
      if (v.player_loc) xml += `      <video:player_loc>${esc(v.player_loc)}</video:player_loc>\n`;
      xml += '    </video:video>\n';
    }
    xml += '  </url>\n';
  }
  return xml + '</urlset>\n';
}

function list(v: string | string[] | undefined): string[] {
  return v === undefined ? [] : Array.isArray(v) ? v : [v];
}

/** The text of a robots.txt. */
export function renderRobotsTXT(robots: RobotsFile): string {
  let out = '';
  for (const rule of Array.isArray(robots.rules) ? robots.rules : [robots.rules]) {
    for (const ua of list(rule.userAgent).length ? list(rule.userAgent) : ['*']) out += `User-Agent: ${ua}\n`;
    for (const a of list(rule.allow)) out += `Allow: ${a}\n`;
    for (const d of list(rule.disallow)) out += `Disallow: ${d}\n`;
    if (rule.crawlDelay !== undefined) out += `Crawl-delay: ${rule.crawlDelay}\n`;
    out += '\n';
  }
  for (const s of list(robots.sitemap)) out += `Sitemap: ${s}\n`;
  if (robots.host) out += `Host: ${robots.host}\n`;
  return out;
}

/** The JSON of a web app manifest. */
export function renderManifestJSON(manifest: ManifestFile): string {
  return JSON.stringify(manifest, null, 2) + '\n';
}
