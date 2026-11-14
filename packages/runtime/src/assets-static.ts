// A StaticLayer backed by a platform asset fetcher (Cloudflare's env.ASSETS).
// Used when the Worker runs first and falls through to the uploaded assets.

import type { StaticLayer } from "./server-handler.js";
import { extName } from "./handler-routing.js";

export interface AssetFetcher {
  fetch(request: Request): Promise<Response>;
}

export interface AssetsStaticOptions {
  /**
   * Ask for a folder's index.html file by name. The generated wrangler config
   * sets assets.html_handling to "none" for trailingSlash "ignore", so the
   * platform no longer maps /x and /x/ to x/index.html or redirects between
   * them; the layer does that mapping itself and both URLs serve the page.
   */
  directoryIndex?: boolean;
}

export function createAssetsStaticLayer(assets: AssetFetcher, options: AssetsStaticOptions = {}): StaticLayer {
  const probe = async (url: string, path: string): Promise<boolean> => {
    const u = new URL(url);
    u.pathname = path;
    u.search = "";
    const res = await assets.fetch(new Request(u.toString(), { method: "HEAD", redirect: "manual" }));
    return res.status !== 404;
  };
  let origin = "https://assets.invalid";
  return {
    async exists(urlPath) {
      const rel = urlPath.replace(/^\/+|\/+$/g, "");
      return (await probe(origin + "/", rel === "" ? "/index.html" : "/" + rel)) || (rel !== "" && (await probe(origin + "/", "/" + rel + "/index.html")));
    },
    async isAsset(urlPath) {
      const ext = extName(urlPath).toLowerCase();
      if (ext === "" || ext === ".html") return false;
      return probe(origin + "/", urlPath);
    },
    async serve(request, urlPath) {
      origin = new URL(request.url).origin;
      if (options.directoryIndex && extName(urlPath) === "") {
        const rel = urlPath.replace(/^\/+|\/+$/g, "");
        const u = new URL(request.url);
        u.pathname = rel === "" ? "/index.html" : "/" + rel + "/index.html";
        const indexed = await assets.fetch(new Request(u.toString(), request));
        // A file without an extension (and no folder of that name) is still served as itself.
        if (indexed.status !== 404) return indexed;
      }
      const res = await assets.fetch(request);
      return res.status === 404 ? null : res;
    },
  };
}
