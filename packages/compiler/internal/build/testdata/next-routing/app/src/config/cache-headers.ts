/**
 * Cache-Control for the static files in packages/web/public (Next sends `public, max-age=0` for them by default,
 * i.e. a revalidation round trip per load, painful for the multi-MB ORT wasm and model files).
 *
 * None of these filenames are content-hashed (`/ort/*` must match the onnxruntime-web version of each build,
 * `/models/*` keeps a fixed name), so no `immutable`: a day fresh plus a week of stale-while-revalidate means a changed
 * file reaches users within ~a day while repeat visits skip the network. `/_next/static/*` is hashed and already
 * immutable (Next); `/api/*` is deliberately not matched here and keeps its own `no-store`.
 *
 * Exception: `/preview/<hash>.wasm` (the preview key-derivation module, docs/PREVIEW_WASM.md) IS content-hashed by
 * scripts/gen-preview-cipher.mjs, a fresh name every build and rotation, so it is cached immutably for a year.
 */
export interface CacheRule {
  source: string;
  headers: { key: string; value: string }[];
}

const rule = (source: string, value: string): CacheRule => ({ source, headers: [{ key: 'Cache-Control', value }] });

/** File types served from public/brand. */
export const BRAND_FILE_TYPES = 'svg|png|webp|jpg|jpeg|gif|ico';

export function buildCacheRules(): CacheRule[] {
  const swr = 'public, max-age=86400, stale-while-revalidate=604800';
  return [
    rule('/ort/:path*', swr),
    rule('/models/:path*', swr),
    rule('/dd-pause.js', 'public, max-age=3600'),
    // Image files only, so a page under /brand could never be marked publicly cacheable.
    rule(`/brand/:file([^/]+\\.(?:${BRAND_FILE_TYPES}))`, 'public, max-age=86400'),
    rule('/help/:path*', 'public, max-age=86400'),
    rule('/preview/:file([0-9a-f]{16}\\.wasm)', 'public, max-age=31536000, immutable'),
    // The Krate pages' assets (packages/static-site, copied to public/_k). Their scripts and stylesheets carry a content hash in the
    // name; the font does not. The pages themselves (_k/**/index.html) are never requested by URL: the proxy answers the page paths.
    rule('/_k/fonts/:path*', swr),
    rule('/_k/chunks/:path*', 'public, max-age=31536000, immutable'),
    rule('/_k/:file(styles\\.[0-9a-z]+\\.css)', 'public, max-age=31536000, immutable'),
    rule('/_k/:path*/:file(index\\.[0-9a-z]+\\.js)', 'public, max-age=31536000, immutable'),
  ];
}
