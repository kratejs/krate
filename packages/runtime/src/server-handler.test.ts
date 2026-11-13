// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
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
