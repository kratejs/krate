// @vitest-environment node
import { describe, expect, it } from "vitest";
import { createKrateHandler, type HandlerConfig, type KrateServerData } from "./server-handler.js";
import { jsx } from "./server.js";

const config: HandlerConfig = {
  basePath: "",
  trustProxy: true,
  maxBodyBytes: 1 << 20,
  maxAPIBodyBytes: 1 << 20,
  redirects: [{ source: "/old", destination: "/new", permanent: true }],
  rewrites: [],
  headers: [{ source: "/new", headers: { "X-Rule": "yes" } }],
  security: { nosniff: true, frameOptions: "DENY", referrerPolicy: "no-referrer", permissionsPolicy: "", hsts: "", csp: "" },
  cors: { enabled: false, origins: [], methods: [], headers: [], credentials: false, maxAge: 0 },
  ssr: { timeout: 2000, maxCacheSize: 16, ppr: false, pprRevalidate: 60 },
};

function data(extra: Partial<KrateServerData> = {}): KrateServerData {
  return {
    version: 1,
    config,
    pages: [{ route: "/live", mode: "isr", revalidate: 30, bundlePath: "live" }],
    regions: {},
    allPages: [{ route: "/live", mode: "isr" }, { route: "/static", mode: "ssg" }],
    shells: { "/live": "<html><head><title>t</title></head><body><!--suspense:page-->baked<!--/suspense:page--></body></html>" },
    templates: [],
    staticOnlyRoutes: [],
    hashedAssets: [],
    html404: "<h1>custom 404</h1>",
    html500: "",
    apiRoutes: ["hello", "items/[id]"],
    actions: [{ id: "act1", export: "add", file: "acts" }],
    hasMiddleware: true,
    ...extra,
  };
}

let renders = 0;
const modules = {
  pages: { live: { default: () => jsx("main", { children: "render " + ++renders }) } },
  api: {
    hello: { GET: () => Response.json({ ok: true }), POST: async (r: Request) => new Response(await r.text(), { status: 201 }) },
    "items/[id]": { GET: (_r: Request, ctx: any) => Response.json(ctx.params) },
  },
  actions: { acts: { add: (a: number, b: number) => ({ sum: a + b }) } },
  middleware: {
    middleware: (req: Request) => {
      if (new URL(req.url).pathname === "/blocked") return new Response("no", { status: 403 });
      return new Response(null, { headers: { "x-middleware-next": "1", "x-mw": "1" } });
    },
    config: { matcher: ["/blocked", "/api/:path*", "/live"] },
  },
};

describe("createKrateHandler", () => {
  const handler = createKrateHandler({ target: "node", data: data(), modules });
  const get = (path: string, init?: RequestInit) => handler(new Request("http://localhost" + path, init));

  it("applies config redirects, header rules and security headers", async () => {
    const r = await get("/old");
    expect(r.status).toBe(301);
    expect(r.headers.get("location")).toBe("/new");
    expect(r.headers.get("x-frame-options")).toBe("DENY");
    expect(r.headers.get("permissions-policy")).toBeNull();
    expect((await get("/new")).headers.get("x-rule")).toBe("yes");
  });

  it("serves ISR pages from the cache with the CDN policy", async () => {
    const a = await get("/live/");
    const b = await get("/live/");
    expect(a.headers.get("cache-control")).toBe("public, s-maxage=30, stale-while-revalidate=60");
    expect(a.headers.get("x-krate-cache")).toBe("MISS");
    expect(b.headers.get("x-krate-cache")).toBe("HIT");
    expect(await b.text()).toContain("render 1");
    expect(renders).toBe(1);
  });

  it("runs middleware and answers its responses", async () => {
    expect((await get("/blocked")).status).toBe(403);
    expect((await get("/api/hello")).headers.get("x-mw")).toBe("1");
  });

  it("routes API requests with 404, 405 and params", async () => {
    expect(await (await get("/api/items/7")).json()).toEqual({ id: "7" });
    expect((await get("/api/none")).status).toBe(404);
    const r = await get("/api/hello", { method: "DELETE" });
    expect(r.status).toBe(405);
    expect(r.headers.get("allow")).toBe("GET, POST, HEAD, OPTIONS");
    expect((await get("/api/hello", { method: "POST", body: "abc" })).status).toBe(201);
  });

  it("runs server actions and rejects cross-site posts", async () => {
    const headers = { "content-type": "application/json", "x-krate-action": "fetch", "sec-fetch-site": "same-origin" };
    const ok = await get("/__krate/action/act1", { method: "POST", headers, body: '{"args":[2,3]}' });
    expect(await ok.json()).toEqual({ ok: true, result: { sum: 5 } });
    const bad = await get("/__krate/action/act1", { method: "POST", headers: { ...headers, "sec-fetch-site": "cross-site" }, body: "{}" });
    expect(bad.status).toBe(403);
  });

  it("answers unknown pages with the custom 404 and hides internals", async () => {
    const r = await get("/nope/");
    expect(r.status).toBe(404);
    expect(await r.text()).toContain("custom 404");
    expect((await get("/.krate/x")).status).toBe(404);
  });

  it("reports revalidation honestly on multi-instance targets", async () => {
    let seen: any;
    const h = createKrateHandler({
      target: "vercel",
      data: data({ actions: [{ id: "rv", export: "rv", file: "acts" }] }),
      modules: {
        ...modules,
        actions: { acts: { rv: async () => { const { revalidatePath } = await import("./cache.js"); revalidatePath("/live"); return 1; } } },
      },
    });
    const res = await h(new Request("http://localhost/__krate/action/rv", { method: "POST", headers: { "x-krate-action": "fetch", "content-type": "application/json", "sec-fetch-site": "same-origin" }, body: '{"args":[]}' }));
    seen = await res.json();
    expect(seen.revalidated).toBe(true);
    expect(seen.revalidation.paths).toEqual([{ path: "/live", type: "page" }]);
  });
});

describe("middleware without a matcher", () => {
  const seen: string[] = [];
  const mw = {
    middleware: (req: Request) => {
      const p = new URL(req.url).pathname;
      seen.push(p);
      if (req.headers.get("x-auth") !== "yes") return new Response("denied", { status: 401 });
      return new Response(null, { headers: { "x-middleware-next": "1" } });
    },
  };
  const staticLayer = {
    exists: (p: string) => p === "/doc.pdf",
    isAsset: (p: string) => p === "/doc.pdf",
    serve: async (_r: Request, p: string) => (p === "/doc.pdf" ? new Response("pdf") : null),
  };
  const h = createKrateHandler({
    target: "node",
    data: data({ apiRoutes: ["files/[name]"] }),
    modules: { ...modules, api: { "files/[name]": { GET: () => Response.json({ secret: "TOP" }) } }, middleware: mw },
    static: staticLayer,
  });
  const get = (path: string) => h(new Request("http://localhost" + path));

  it("does not skip dynamic API routes that end in a static extension", async () => {
    for (const ext of ["pdf", "txt", "xml", "zip", "js", "css"]) {
      const r = await get("/api/files/report." + ext);
      expect(r.status, ext).toBe(401);
    }
  });

  it("still skips a real static file", async () => {
    seen.length = 0;
    const r = await get("/doc.pdf");
    expect(r.status).toBe(200);
    expect(seen).not.toContain("/doc.pdf");
  });

  it("runs middleware for a static-looking page path that is not a file", async () => {
    expect((await get("/missing.pdf")).status).toBe(401);
  });
});

describe("canonical slash redirect", () => {
  const h = createKrateHandler({
    target: "node",
    data: data({ hasMiddleware: false, templates: [{ pattern: "plain/[id]", html: "<p>[id]</p>" }] }),
    modules,
  });
  const get = (path: string) => h(new Request("http://localhost" + path, { redirect: "manual" }));

  it("never emits a Location that can parse as an absolute URL", async () => {
    for (const seg of ["ftp:3232235777", "https:3232235777", "http:1"]) {
      const r = await get("/plain/" + seg);
      expect(r.status, seg).toBe(301);
      const loc = r.headers.get("location")!;
      expect(loc, seg).toBe("./" + seg + "/");
      expect(new URL(loc, "http://localhost/plain/" + seg).origin, seg).toBe("http://localhost");
    }
  });

  it("keeps the query on an ordinary segment", async () => {
    const r = await get("/plain/7?a=1");
    expect(r.headers.get("location")).toBe("./7/?a=1");
  });
});
