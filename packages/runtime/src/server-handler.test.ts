// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { createKrateHandler, matchMetaRoute, type HandlerConfig, type KrateServerData } from "./server-handler.js";
import { jsx, ServerErrorBoundary } from "./server.js";
import { headers, redirect, permanentRedirect } from "./action-context.js";

const config: HandlerConfig = {
  basePath: "",
  trustProxy: true,
  maxBodyBytes: 1 << 20,
  maxAPIBodyBytes: 1 << 20,
  redirects: [
    { source: "/old", destination: "/new", permanent: true },
    { source: "/moved", destination: "/new", permanent: true, statusCode: 308 },
    { source: "/store/:slug((?!compare$)[^/]+)", destination: "/templates/:slug", statusCode: 308 },
  ],
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
    apiRoutes: ["hello", "items/[id]", "items/[id]/download", "scope", "who"],
    actions: [{ id: "act1", export: "add", file: "acts" }, { id: "act2", export: "oauth", file: "acts" }],
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
    "items/[id]/download": { GET: (_r: Request, ctx: any) => Response.json({ download: ctx.params.id }) },
    who: { GET: (r: Request) => Response.json({ url: r.headers.get("x-dd-url"), dropped: r.headers.get("x-drop") }) },
    scope: {
      GET: () => {
        const store = (globalThis as Record<symbol, any>)[Symbol.for("krate.action.store")].getStore();
        store.cookies.set("flow", "1", { maxAge: 900 });
        throw Object.assign(new Error("r"), { __krateRedirect: "https://provider.example/auth", __krateRedirectExternal: true });
      },
    },
  },
  actions: {
    acts: {
      add: (a: number, b: number) => ({ sum: a + b }),
      oauth: () => {
        throw Object.assign(new Error("r"), { __krateRedirect: "https://provider.example/auth", __krateRedirectExternal: true });
      },
    },
  },
  middleware: {
    middleware: (req: Request) => {
      if (new URL(req.url).pathname === "/blocked") return new Response("no", { status: 403 });
      if (new URL(req.url).pathname === "/api/who") {
        return new Response(null, {
          headers: { "x-middleware-next": "1", "x-middleware-override-headers": "x-dd-url", "x-middleware-request-x-dd-url": "/api/who?a=1" },
        });
      }
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
    expect((await get("/moved")).status).toBe(308);
    const re = await get("/store/some-template");
    expect([re.status, re.headers.get("location")]).toEqual([308, "/templates/some-template"]);
  });

  it("accepts Next's [{ key, value }] header rules, the last repeated key winning", async () => {
    const arrayConfig: HandlerConfig = {
      ...config,
      headers: [{ source: "/new", headers: [{ key: "X-Rule", value: "first" }, { key: "X-Other", value: "o" }, { key: "x-rule", value: "last" }] }],
    };
    const h = createKrateHandler({ target: "node", data: data({ config: arrayConfig }), modules });
    const r = await h(new Request("http://localhost/new"));
    expect(r.headers.get("x-rule")).toBe("last");
    expect(r.headers.get("x-other")).toBe("o");
  });

  it("middleware request header overrides reach the route and stay off the response", async () => {
    const r = await get("/api/who", { headers: { "x-drop": "secret" } });
    expect(await r.json()).toEqual({ url: "/api/who?a=1", dropped: null });
    expect(r.headers.get("x-middleware-override-headers")).toBeNull();
    expect(r.headers.get("x-middleware-request-x-dd-url")).toBeNull();
  });

  it("routes API requests with 404, 405 and params", async () => {
    expect(await (await get("/api/items/7")).json()).toEqual({ id: "7" });
    // A static file inside a dynamic directory (issue: orders/[id]/download.ts 404ed).
    expect(await (await get("/api/items/7/download")).json()).toEqual({ download: "7" });
    expect((await get("/api/items/7/other")).status).toBe(404);
    expect((await get("/api/none")).status).toBe(404);
    const r = await get("/api/hello", { method: "DELETE" });
    expect(r.status).toBe(405);
    expect(r.headers.get("allow")).toBe("GET, POST, HEAD, OPTIONS");
    expect((await get("/api/hello", { method: "POST", body: "abc" })).status).toBe(201);
  });

  it("route handlers get the action scope, and external redirect signals are followed", async () => {
    const r = await get("/api/scope");
    expect(r.status).toBe(307);
    expect(r.headers.get("location")).toBe("https://provider.example/auth");
    expect(r.headers.get("set-cookie")).toContain("flow=1");
    const headers = { "content-type": "application/json", "x-krate-action": "fetch", "sec-fetch-site": "same-origin" };
    const act = await (await get("/__krate/action/act2", { method: "POST", headers, body: '{"args":[]}' })).json();
    expect(act).toMatchObject({ ok: true, redirect: "https://provider.example/auth", external: true });
  });

  it("runs server actions and rejects cross-site posts", async () => {
    const headers = { "content-type": "application/json", "x-krate-action": "fetch", "sec-fetch-site": "same-origin" };
    const ok = await get("/__krate/action/act1", { method: "POST", headers, body: '{"args":[2,3]}' });
    expect(await ok.json()).toEqual({ ok: true, result: { sum: 5 } });
    const bad = await get("/__krate/action/act1", { method: "POST", headers: { ...headers, "sec-fetch-site": "cross-site" }, body: "{}" });
    expect(bad.status).toBe(403);
  });
});

describe("static files", () => {
  // A headers rule's Cache-Control used to be replaced with no-cache.
  it("a headers rule sets Cache-Control on a public file", async () => {
    const staticLayer = {
      exists: (p: string) => p === "/brand/logo.png" || p === "/other.png",
      isAsset: () => true,
      serve: async () => new Response("png", { headers: { "content-type": "image/png" } }),
    };
    const cfg = { ...config, headers: [{ source: "/brand/*.png", headers: { "Cache-Control": "public, max-age=86400" } }] };
    const h = createKrateHandler({ target: "node", data: data({ config: cfg, hasMiddleware: false }), modules, static: staticLayer });
    const ruled = await h(new Request("http://localhost/brand/logo.png"));
    expect(ruled.status).toBe(200);
    expect(ruled.headers.get("cache-control")).toBe("public, max-age=86400");
    expect((await h(new Request("http://localhost/other.png"))).headers.get("cache-control")).toBe("no-cache");
  });
});

describe("API maxDuration", () => {
  const sleep = (ms: number, signal: AbortSignal) =>
    new Promise<boolean>((resolve) => {
      const t = setTimeout(() => resolve(false), ms);
      signal.addEventListener("abort", () => (clearTimeout(t), resolve(true)), { once: true });
    });
  let sawAbort = false;
  const mods = {
    ...modules,
    api: {
      ...modules.api,
      slow: { GET: async (r: Request) => ((sawAbort = await sleep(2000, r.signal)), Response.json({ late: true })) },
      ok: { GET: async (r: Request) => (await sleep(50, r.signal), Response.json({ done: true })) },
    },
  };
  const h = createKrateHandler({
    target: "node",
    data: data({ hasMiddleware: false, apiRoutes: ["slow", "ok"], apiMaxDuration: { slow: 0.1, ok: 5 } }),
    modules: mods,
  });
  const get = (path: string) => h(new Request("http://localhost" + path));

  it("answers 504 and aborts request.signal at the deadline", async () => {
    const err = vi.spyOn(console, "error").mockImplementation(() => {});
    const r = await get("/api/slow");
    expect(r.status).toBe(504);
    expect(await r.json()).toEqual({ error: "Gateway Timeout" });
    expect(sawAbort).toBe(true);
    err.mockRestore();
  });

  it("lets a handler that finishes inside its budget through", async () => {
    const r = await get("/api/ok");
    expect(r.status).toBe(200);
    expect(await r.json()).toEqual({ done: true });
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
});

describe("trailingSlash redirect", () => {
  const seen: string[] = [];
  const mwModules = {
    ...modules,
    middleware: {
      middleware: (req: Request) => {
        seen.push(new URL(req.url).pathname);
        return new Response("mw", { status: 200 });
      },
      config: { matcher: ["/mw/:path*"] },
    },
  };
  const mk = (trailingSlash: "always" | "never", basePath = "") =>
    createKrateHandler({ target: "node", data: data({ config: { ...config, trailingSlash, basePath } }), modules: mwModules });
  const req = (h: ReturnType<typeof createKrateHandler>, path: string, method = "GET") =>
    h(new Request("http://localhost" + path, { method, redirect: "manual" }));

  it("never: answers 308 before middleware for pages, API routes and any method", async () => {
    const h = mk("never");
    seen.length = 0;
    for (const [path, method, loc] of [
      ["/mw/a/", "GET", "/mw/a"],
      ["/live/?x=1", "GET", "/live?x=1"],
      ["/api/hello/", "POST", "/api/hello"],
      ["/a.svg/", "GET", "/a.svg"],
      ["//evil.example/", "GET", "/evil.example"],
    ]) {
      const r = await req(h, path, method);
      expect([r.status, r.headers.get("location")], path).toEqual([308, loc]);
    }
    expect(seen).toEqual([]);
    expect((await req(h, "/mw/a")).status).toBe(200);
    expect(seen).toEqual(["/mw/a"]);
    expect((await req(h, "/404/")).status).toBe(404);
  });

  it("always: keeps its slash redirect behind middleware", async () => {
    seen.length = 0;
    expect((await req(mk("always"), "/mw/a")).status).toBe(200);
    expect(seen).toEqual(["/mw/a"]);
  });

  it("keeps the base path in the Location", async () => {
    const r = await req(mk("never", "/docs"), "/docs/live/");
    expect([r.status, r.headers.get("location")]).toEqual([308, "/docs/live"]);
  });
});

describe("signed flash cookies", () => {
  const shell =
    "<html><head><title>t</title></head><body><!--suspense:page-->baked<!--/suspense:page--></body></html>";
  const mk = (target: "node" | "vercel" | "cloudflare", flashSecret?: string) =>
    createKrateHandler({
      target,
      flashSecret,
      data: data({
        hasMiddleware: false,
        pages: [{ route: "/form", mode: "ssr", bundlePath: "form" }],
        allPages: [{ route: "/form", mode: "ssr" }],
        shells: { "/form": shell },
        actions: [{ id: "act9", export: "bad", file: "acts" }],
      }),
      modules: {
        pages: {
          form: {
            default: () => jsx("main", { children: jsx("form", { action: "/__krate/action/act9", method: "post", children: jsx("button", { children: "go" }) }) }),
          },
        },
        actions: { acts: { bad: () => ({ error: "Name is required" }) } },
      },
    });
  const page = (h: ReturnType<typeof mk>, cookie: string) => h(new Request("http://localhost/form/", { headers: { cookie } }));

  it("ignores a cookie planted without a valid signature", async () => {
    const h = mk("node", "a-shared-flash-secret-for-this-test");
    const planted = Buffer.from(JSON.stringify({ a: "act9", r: { error: "Your account was suspended" } })).toString("base64url");
    for (const cookie of [planted, planted + ".", planted + ".AAAA", "x." + planted]) {
      const body = await (await page(h, "krate_flash=" + cookie)).text();
      expect(body, cookie).not.toContain("suspended");
      expect(body, cookie).not.toContain("krate-flash");
    }
  });
});

describe("forwarded headers per platform", () => {
  const mk = (target: "node" | "vercel" | "netlify" | "cloudflare", trustProxy?: boolean | null) => {
    const base = data({ hasMiddleware: false });
    return createKrateHandler({
      target,
      data: { ...base, config: { ...config, trustProxy: trustProxy === undefined ? null : trustProxy }, apiRoutes: ["echo"], actions: [{ id: "act1", export: "add", file: "acts" }] },
      modules: {
        api: {
          echo: {
            GET: (r: Request) =>
              Response.json({
                url: r.url,
                xff: r.headers.get("x-forwarded-for"),
                realIp: r.headers.get("x-real-ip"),
                forwarded: r.headers.get("forwarded"),
                host: r.headers.get("x-forwarded-host"),
                proto: r.headers.get("x-forwarded-proto"),
              }),
          },
        },
        actions: { acts: { add: (a: number, b: number) => ({ sum: a + b }) } },
      },
    });
  };
  const echo = async (h: ReturnType<typeof mk>, headers: Record<string, string>) =>
    (await h(new Request("http://app.internal/api/echo", { headers }))).json() as Promise<Record<string, string | null>>;
  // What a hostile client adds on top of whatever the platform sets.
  const spoof = { "x-forwarded-host": "evil.example", "x-forwarded-proto": "https", "x-forwarded-for": "6.6.6.6, 7.7.7.7", "x-real-ip": "6.6.6.6", forwarded: "for=6.6.6.6;host=evil.example" };

  it("vercel: a client-written forwarded chain never becomes the client address", async () => {
    const out = await echo(mk("vercel"), { ...spoof, "x-forwarded-host": "shop.example", "x-real-ip": "203.0.113.9" });
    expect(out.xff).toBe("203.0.113.9");
    expect(out.forwarded).toBeNull();
  });

  it("the server-action origin check is not fooled by a spoofed X-Forwarded-Host", async () => {
    const post = (h: ReturnType<typeof mk>) =>
      h(
        new Request("http://app.internal/__krate/action/act1", {
          method: "POST",
          headers: { origin: "https://evil.example", "x-forwarded-host": "evil.example", "x-krate-action": "fetch", "content-type": "application/json" },
          body: '{"args":[1,2]}',
        }),
      );
    for (const target of ["netlify", "cloudflare"] as const) {
      expect((await post(mk(target))).status, target + " unset").toBe(403);
      expect((await post(mk(target, false))).status, target + " false").toBe(403);
    }
    expect((await post(mk("vercel", false))).status).toBe(403);
    // Behind a proxy of yours that sets the header, it is the real host.
    expect((await post(mk("cloudflare", true))).status).toBe(200);
  });
});

describe("middleware exports", () => {
  const run = (middleware: any) =>
    createKrateHandler({ target: "node", data: data(), modules: { ...modules, middleware } })(new Request("http://localhost/live"));

  it("fails closed when the module has no handler", async () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    expect((await run({ other: 1 })).status).toBe(503);
    expect((await run(undefined)).status).toBe(503);
    error.mockRestore();
  });
});

describe("per-request metadata routes", () => {
  const metaRoutes = [
    { path: "/robots.txt", key: "__krate_meta/robots" },
    { prefix: "/sitemap/", suffix: ".xml", key: "__krate_meta/sitemap/[id]" },
  ];

  it("maps public URLs to API route keys", () => {
    expect(matchMetaRoute(metaRoutes, "/robots.txt")).toBe("__krate_meta/robots");
    expect(matchMetaRoute(metaRoutes, "/sitemap/3.xml")).toBe("__krate_meta/sitemap/3");
    for (const p of ["/sitemap/.xml", "/sitemap/a/b.xml", "/robots.txt/x", "/other"]) expect(matchMetaRoute(metaRoutes, p)).toBe("");
    expect(matchMetaRoute(undefined, "/robots.txt")).toBe("");
  });

  it("serves them through the API handler after middleware", async () => {
    const handler = createKrateHandler({
      target: "node",
      data: data({ metaRoutes, apiRoutes: ["__krate_meta/robots", "__krate_meta/sitemap/[id]"], hasMiddleware: false }),
      modules: {
        api: {
          "__krate_meta/robots": { GET: (r: Request) => new Response("User-agent: *\n# " + new URL(r.url).pathname, { headers: { "content-type": "text/plain" } }) },
          "__krate_meta/sitemap/[id]": { GET: (_r: Request, ctx: any) => new Response("sitemap " + ctx.params.id) },
        },
      },
    });
    const robots = await handler(new Request("http://localhost/robots.txt"));
    expect(robots.status).toBe(200);
    expect(await robots.text()).toContain("User-agent: *");
    expect(await (await handler(new Request("http://localhost/sitemap/7.xml"))).text()).toBe("sitemap 7");
    expect((await handler(new Request("http://localhost/robots.txt", { method: "POST" }))).status).not.toBe(200);
  });
});

describe("Cache-Control on request-time pages", () => {
  const shell = "<html><head><title>t</title></head><body><!--suspense:page-->baked<!--/suspense:page--></body></html>";
  const mk = (target: "node" | "vercel" | "cloudflare") =>
    createKrateHandler({
      target,
      data: data({
        hasMiddleware: true,
        pages: [
          { route: "/cached", mode: "ssr", bundlePath: "page" },
          { route: "/plain", mode: "ssr", bundlePath: "page" },
        ],
        allPages: [{ route: "/cached", mode: "ssr" }, { route: "/plain", mode: "ssr" }],
        shells: { "/cached": shell, "/plain": shell },
      }),
      modules: {
        pages: { page: { default: () => jsx("main", { children: "ssr" }) } },
        middleware: {
          middleware: (req: Request) => {
            const headers: Record<string, string> = { "x-middleware-next": "1" };
            if (new URL(req.url).pathname.startsWith("/cached")) headers["cache-control"] = "public, s-maxage=60";
            return new Response(null, { headers });
          },
        },
      },
    });

  it("lets middleware set it and defaults to no-store", async () => {
    for (const target of ["node", "cloudflare", "vercel"] as const) {
      const h = mk(target);
      const cached = await h(new Request("http://localhost/cached/"));
      expect(cached.headers.get("cache-control"), target).toBe("public, s-maxage=60");
      expect(await cached.text()).toContain("ssr");
      expect((await h(new Request("http://localhost/plain/"))).headers.get("cache-control"), target).toBe("no-store");
    }
  });
});

describe("error boundary under a request-time page", () => {
  const shell = "<html><head><title>t</title></head><body><!--suspense:page-->baked<!--/suspense:page--></body></html>";
  const boom = async () => {
    throw new Error("db down");
  };
  const h = createKrateHandler({
    target: "node",
    data: data({
      hasMiddleware: false,
      pages: [{ route: "/boom", mode: "ssr", bundlePath: "boom" }],
      allPages: [{ route: "/boom", mode: "ssr" }],
      shells: { "/boom": shell },
    }),
    modules: {
      pages: {
        boom: {
          default: () =>
            jsx("div", {
              children: jsx(ServerErrorBoundary, { fallback: (p: any) => jsx("p", { children: "failed " + p.error.digest }), children: jsx(boom, {}) }),
            }),
        },
      },
    },
  });

  it("answers 500 with no-store and the fallback in place", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const r = await h(new Request("http://localhost/boom/"));
    const body = await r.text();
    expect(r.status).toBe(500);
    expect(r.headers.get("cache-control")).toBe("no-store");
    expect(body).toMatch(/<p>failed [0-9a-f]{10}<\/p>/);
    expect(body).not.toContain("db down");
  });

  it("keeps no-store when middleware asked for public caching", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const cached = createKrateHandler({
      target: "cloudflare",
      data: data({
        hasMiddleware: true,
        pages: [{ route: "/boom", mode: "ssr", bundlePath: "boom" }],
        allPages: [{ route: "/boom", mode: "ssr" }],
        shells: { "/boom": shell },
      }),
      modules: {
        pages: {
          boom: {
            default: () =>
              jsx("div", { children: jsx(ServerErrorBoundary, { fallback: () => jsx("p", { children: "failed" }), children: jsx(boom, {}) }) }),
          },
        },
        middleware: { middleware: () => new Response(null, { headers: { "x-middleware-next": "1", "cache-control": "public, s-maxage=60" } }) },
      },
    });
    const r = await cached(new Request("http://localhost/boom/"));
    expect(r.status).toBe(500);
    expect(r.headers.get("cache-control")).toBe("no-store");
  });
});

describe("config route rules", () => {
  const rules: HandlerConfig = {
    ...config,
    redirects: [
      { source: "/store/plans", destination: "/store", statusCode: 308 },
      { source: "/store", destination: "/templates", statusCode: 308, has: [{ type: "query", key: "vehicle" }] },
      { source: "/store/:slug((?!compare$)[^/]+)", destination: "/templates/:slug", statusCode: 308 },
      { source: "/api/old/:rest*", destination: "/api/:rest*", statusCode: 307 },
      { source: "/legacy", destination: "/new", permanent: true },
    ],
    rewrites: [{ source: "/hi", destination: "/api/hello", has: [{ type: "header", key: "x-go" }] }],
    headers: [
      { source: "/:path*", headers: { "X-All": "1", "Cache-Control": "public, max-age=60" } },
      { source: "/m", headers: { "X-Mobile": "1" }, has: [{ type: "header", key: "x-device", value: "ios" }] },
    ],
  };
  const deny = {
    ...modules,
    middleware: { middleware: () => new Response("maintenance", { status: 503 }), config: { matcher: ["/:path*"] } },
  };
  const handler = createKrateHandler({ target: "node", data: data({ config: rules, apiRoutes: ["hello"], hashedAssets: [] }), modules: deny });
  const get = (path: string, init?: RequestInit) => handler(new Request("http://localhost" + path, init));

  it("redirects like Next: status, query passthrough, regex params", async () => {
    const cases: Array<[string, number, string]> = [
      ["/store?vehicle=x", 308, "/templates?vehicle=x"],
      ["/store/some-slug", 308, "/templates/some-slug"],
      ["/store/plans?x=1", 308, "/store?x=1"],
      ["/legacy?a=1", 301, "/new?a=1"],
    ];
    for (const [path, status, loc] of cases) {
      const r = await get(path);
      expect([path, r.status, r.headers.get("location")]).toEqual([path, status, loc]);
    }
  });

  it("leaves unmatched requests to middleware, and redirects run before it and on /api", async () => {
    expect((await get("/store/compare")).status).toBe(503);
    expect((await get("/store")).status).toBe(503);
    const r = await get("/api/old/hello?k=v");
    expect([r.status, r.headers.get("location")]).toEqual([307, "/api/hello?k=v"]);
  });

  it("answers a rewrite onto an API route from the API handler", async () => {
    const open = createKrateHandler({
      target: "node",
      data: data({
        config: { ...rules, redirects: [], rewrites: [{ source: "/hi", destination: "/api/hello" }, { source: "/items-alias/:id", destination: "/api/items/:id" }] },
        apiRoutes: ["hello", "items/[id]"],
        hasMiddleware: false,
        hashedAssets: [],
      }),
      modules,
    });
    const hi = await open(new Request("http://localhost/hi"));
    expect([hi.status, await hi.json()]).toEqual([200, { ok: true }]);
    const item = await open(new Request("http://localhost/items-alias/7"));
    expect(item.status).toBe(200);
    expect(await item.text()).toContain("7");
  });

  it("applies header rules last so they beat a handler's Cache-Control, with has", async () => {
    const r = await get("/m", { headers: { "x-device": "ios" } });
    expect(r.headers.get("x-all")).toBe("1");
    expect(r.headers.get("x-mobile")).toBe("1");
    expect(r.headers.get("cache-control")).toBe("public, max-age=60");
    expect((await get("/m")).headers.get("x-mobile")).toBeNull();
    const redirect = await get("/store/x");
    expect(redirect.headers.get("x-all")).toBe("1");
  });
});

describe("page headers()", () => {
  const shell =
    "<html><head><title>t</title></head><body><!--suspense:page-->baked<!--/suspense:page--></body></html>";
  it("includes the request host, like route handlers", async () => {
    const h = createKrateHandler({
      target: "node",
      data: data({
        hasMiddleware: false,
        pages: [{ route: "/who", mode: "ssr", bundlePath: "who" }],
        allPages: [{ route: "/who", mode: "ssr" }],
        shells: { "/who": shell },
      }),
      modules: { pages: { who: { default: () => jsx("p", { children: "host=" + headers().get("host") }) } } },
    });
    const body = await (await h(new Request("http://example.test:8080/who/", { headers: { accept: "text/html" } }))).text();
    expect(body).toContain("host=example.test:8080");
  });
});

describe("page redirect statuses", () => {
  const shell =
    "<html><head><title>t</title></head><body><!--suspense:page-->baked<!--/suspense:page--></body></html>";
  const mk = () =>
    createKrateHandler({
      target: "node",
      data: data({
        hasMiddleware: false,
        pages: [
          { route: "/temp", mode: "ssr", bundlePath: "temp" },
          { route: "/perm", mode: "ssr", bundlePath: "perm" },
        ],
        allPages: [{ route: "/temp", mode: "ssr" }, { route: "/perm", mode: "ssr" }],
        shells: { "/temp": shell, "/perm": shell },
      }),
      modules: {
        pages: {
          temp: { default: () => redirect("/login") },
          perm: { default: () => permanentRedirect("/new") },
        },
      },
    });
  const get = (h: ReturnType<typeof mk>, path: string) => h(new Request("http://localhost" + path, { redirect: "manual" }));

  it("redirect() answers 307 and permanentRedirect() 308", async () => {
    const h = mk();
    const t = await get(h, "/temp/");
    expect([t.status, t.headers.get("location")]).toEqual([307, "/login"]);
    const p = await get(h, "/perm/");
    expect([p.status, p.headers.get("location")]).toEqual([308, "/new"]);
  });
});

describe("route precedence", () => {
  const shell = "<html><head><title>t</title></head><body><!--suspense:page-->baked<!--/suspense:page--></body></html>";
  const files = new Set(["/about/", "/shop/", "/team/staff/"]);
  const staticLayer = {
    exists: (p: string) => files.has(p),
    isAsset: () => false,
    serve: async (_req: Request, p: string) => new Response("static " + p, { headers: { "content-type": "text/html" } }),
  };
  const mods = {
    pages: {
      slug: { default: () => jsx("main", { children: "dynamic-slug" }) },
      filter: { default: () => jsx("main", { children: "dynamic-filter" }) },
      staff: { default: () => jsx("main", { children: "dynamic-staff" }) },
    },
    api: {},
    actions: {},
  };
  const h = createKrateHandler({
    target: "node",
    data: data({
      hasMiddleware: false,
      apiRoutes: [],
      actions: [],
      pages: [
        { route: "/[slug]", mode: "ssr", bundlePath: "slug" },
        { route: "/shop/[[...filter]]", mode: "ssr", bundlePath: "filter" },
        { route: "/team/staff", mode: "ssr", bundlePath: "staff" },
      ],
      allPages: [
        { route: "/[slug]", mode: "ssr" },
        { route: "/about", mode: "ssg" },
        { route: "/shop", mode: "ssg" },
        { route: "/shop/[[...filter]]", mode: "ssr" },
        { route: "/team/[slug]", mode: "ssg" },
        { route: "/team/staff", mode: "ssr" },
      ],
      shells: { "/[slug]": shell, "/shop/[[...filter]]": shell, "/team/staff": shell },
    }),
    modules: mods,
    static: staticLayer,
  });
  const body = async (p: string) => (await h(new Request("http://localhost" + p))).text();

  it("a prerendered static page beats a request-time [param] beside it", async () => {
    expect(await body("/about/")).toBe("static /about/");
    expect(await body("/other/")).toContain("dynamic-slug");
  });

  it("a prerendered index beats a request-time optional catch-all", async () => {
    expect(await body("/shop/")).toBe("static /shop/");
    expect(await body("/shop/a/b/")).toContain("dynamic-filter");
  });

  it("a request-time page at a static path beats a prerendered [param]", async () => {
    expect(await body("/team/staff/")).toContain("dynamic-staff");
  });
});
