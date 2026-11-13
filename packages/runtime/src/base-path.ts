// The site base path, shared by the router, actions and link rendering. The
// compiler writes the config into the page head (see site-config.ts); server
// bundles set it on globalThis so server-rendered links get the same prefix.
import { siteConfig } from './site-config.js';

/** The configured base path as "" or "/prefix" (no trailing slash). */
export function basePath(): string {
  const cfg = siteConfig();
  const bp = (cfg && cfg.basePath) || '';
  if (!bp || bp === '/') return '';
  return '/' + bp.replace(/^\/+|\/+$/g, '');
}

/**
 * Prefixes a root-relative URL with the base path. Relative URLs, fragments,
 * queries, absolute URLs and protocol-relative URLs are returned unchanged.
 */
export function withBase(url: string): string {
  const bp = basePath();
  return bp && url.charCodeAt(0) === 47 && url.charCodeAt(1) !== 47 ? bp + url : url;
}
