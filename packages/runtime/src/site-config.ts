// The site runtime config (`__KRATE_CFG__`). The compiler writes it into the
// page head as a JSON data block rather than an inline script, so a strict
// `script-src 'self'` policy needs no allowance for it. Server bundles set the
// global directly. The block is only present when something differs from the
// defaults (a base path, View Transitions off, dev mode).

/** Site config injected by the compiler. */
export interface KrateRuntimeConfig {
  basePath?: string;
  viewTransitions?: boolean | 'auto';
  /** Milliseconds a prefetched document stays fresh (default 30000). */
  prefetchTTL?: number;
  /** Set by `krate dev`: enables router dev warnings. */
  dev?: boolean;
}

/** Id of the JSON data block the compiler writes. */
export const CFG_BLOCK_ID = '__krate_cfg__';

/** The site config, read once from the head's JSON block when the global is not set. */
export function siteConfig(): KrateRuntimeConfig {
  const g = globalThis as { __KRATE_CFG__?: KrateRuntimeConfig; document?: Document };
  if (g.__KRATE_CFG__) return g.__KRATE_CFG__;
  const doc = g.document;
  const el = doc && typeof doc.getElementById === 'function' ? doc.getElementById(CFG_BLOCK_ID) : null;
  if (el) {
    try {
      const parsed = JSON.parse(el.textContent || '{}') as KrateRuntimeConfig;
      if (parsed && typeof parsed === 'object') {
        g.__KRATE_CFG__ = parsed;
        return parsed;
      }
    } catch {
      // a malformed block falls back to the defaults
    }
  }
  return {};
}
