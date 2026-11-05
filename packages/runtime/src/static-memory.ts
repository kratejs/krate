// A StaticLayer over prerendered HTML pages carried in the server bundle. The
// platforms whose CDN would otherwise serve static pages before the function
// (Vercel, Netlify) route page requests to the function when the project has
// middleware; the function then answers prerendered pages from here, after the
// middleware ran. Assets are never held here: they keep going to the CDN.

import type { StaticLayer } from "./server-handler.js";
import { baseName } from "./handler-routing.js";

function decode(p: string): string {
  try {
    return decodeURIComponent(p);
  } catch {
    return p;
  }
}

/** pages maps dist-relative slash paths ("about/index.html") to their HTML. */
export function createMemoryStaticLayer(pages: Record<string, string>): StaticLayer {
  const find = (urlPath: string): { key: string; redirect: boolean } | null => {
    const rel = decode(urlPath).replace(/^\/+/, "");
    if (rel === "") return pages["index.html"] !== undefined ? { key: "index.html", redirect: false } : null;
    if (rel.endsWith("/")) {
      const k = rel + "index.html";
      return pages[k] !== undefined ? { key: k, redirect: false } : null;
    }
    if (pages[rel] !== undefined) return { key: rel, redirect: false };
    const k = rel + "/index.html";
    return pages[k] !== undefined ? { key: k, redirect: true } : null;
  };
  return {
    exists: (urlPath) => find(urlPath) !== null,
    isAsset: () => false,
    async serve(request, urlPath) {
      const hit = find(urlPath);
      if (!hit) return null;
      if (hit.redirect) {
        const u = new URL(request.url);
        return new Response(null, { status: 301, headers: { Location: baseName(urlPath) + "/" + u.search } });
      }
      return new Response(request.method === "HEAD" ? null : pages[hit.key], {
        status: 200,
        headers: { "Content-Type": "text/html; charset=utf-8" },
      });
    },
  };
}
