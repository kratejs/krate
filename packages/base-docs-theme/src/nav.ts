/** A link the site navigation can mark as the current page. */
export interface ActiveNavLink {
  href: string;
  external?: boolean;
  /**
   * `exact` marks the link only on its own page; `prefix` (the default for
   * every link but "/") also marks it on the pages below it.
   */
  match?: "exact" | "prefix";
}

export interface ActiveNav {
  /** Index of the marked link. */
  index: number;
  /** True when the link is the page itself, false when the page is below it. */
  exact: boolean;
}

/** Strips query, hash, the site base path and trailing slashes. */
export function normalizeNavPath(path: string, basePath?: string): string {
  let p = path.split("#")[0].split("?")[0] || "/";
  const base = basePath && basePath !== "/" ? "/" + basePath.replace(/^\/+|\/+$/g, "") : "";
  if (base && (p === base || p.startsWith(base + "/"))) p = p.slice(base.length) || "/";
  if (p.length > 1) p = p.replace(/\/+$/, "") || "/";
  return p.charAt(0) === "/" ? p : "/" + p;
}

/**
 * Picks the link that marks the current page: the link to the page itself, or
 * else the most specific link whose path contains it ("/docs/" on
 * "/docs/start/"). Only one link is ever marked. External links and links
 * that are not site paths are skipped.
 */
export function activeNavLink(links: ActiveNavLink[], path: string, basePath?: string): ActiveNav | null {
  const current = normalizeNavPath(path, basePath);
  let best: ActiveNav | null = null;
  let bestLen = -1;
  for (let i = 0; i < links.length; i++) {
    const link = links[i];
    if (link.external || !link.href || link.href.charAt(0) !== "/" || link.href.indexOf("//") === 0) continue;
    const href = normalizeNavPath(link.href, basePath);
    if (href === current) return { index: i, exact: true };
    if (link.match === "exact" || href === "/") continue;
    if (current.indexOf(href + "/") === 0 && href.length > bestLen) {
      best = { index: i, exact: false };
      bestLen = href.length;
    }
  }
  return best;
}
