// A StaticLayer backed by a platform asset fetcher (Cloudflare's env.ASSETS).
// Used when the Worker runs first and falls through to the uploaded assets.

import type { StaticLayer } from "./server-handler.js";
import { extName } from "./handler-routing.js";

export interface AssetFetcher {
  fetch(request: Request): Promise<Response>;
}

export function createAssetsStaticLayer(assets: AssetFetcher): StaticLayer {
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
      void urlPath;
      const res = await assets.fetch(request);
      return res.status === 404 ? null : res;
    },
  };
}
