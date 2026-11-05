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

describe("action cookies", () => {
  const h = createKrateHandler({
    target: "node",
    data: data({ hasMiddleware: false, actions: [{ id: "setc", export: "setc", file: "acts" }, { id: "flash", export: "flash", file: "acts" }] }),
    modules: {
      ...modules,
      actions: {
        acts: {
          setc: async () => {
            const { cookies } = await import("./action-context.js");
            cookies().set("none", "1", { sameSite: "None" });
            cookies().set("plain", "2");
            cookies().set("optout", "3", { secure: false });
            return 1;
          },
          flash: () => ({ error: "bad" }),
        },
      },
    },
  });
  const post = (id: string, headers: Record<string, string>) =>
    h(new Request("http://localhost/__krate/action/" + id, { method: "POST", headers: { "sec-fetch-site": "same-origin", ...headers }, body: "{}" }));
  const cookiesOf = (r: Response) => (r.headers as any).getSetCookie() as string[];

  it("marks SameSite=None cookies Secure and defaults Secure on https", async () => {
    const insecure = cookiesOf(await post("setc", { "x-krate-action": "fetch", "content-type": "application/json" }));
    expect(insecure.find((c) => c.startsWith("none="))).toContain("; Secure");
    expect(insecure.find((c) => c.startsWith("plain="))).not.toContain("Secure");

    const https = cookiesOf(await post("setc", { "x-krate-action": "fetch", "content-type": "application/json", "x-forwarded-proto": "https" }));
    expect(https.find((c) => c.startsWith("plain="))).toContain("; Secure");
    expect(https.find((c) => c.startsWith("optout="))).not.toContain("Secure");
  });

  it("marks the flash cookie Secure on https", async () => {
    const r = await post("flash", { "content-type": "application/x-www-form-urlencoded", "x-forwarded-proto": "https", referer: "http://localhost/form" });
    const flash = cookiesOf(r).find((c) => c.startsWith("krate_flash="));
    expect(flash).toContain("; Secure");
  });
});

describe("rewrites to a slashless page path", () => {
  const h = createKrateHandler({
    target: "node",
    data: data({
      hasMiddleware: true,
      templates: [{ pattern: "feedback", html: "<p>feedback form</p>" }],
      config: { ...config, rewrites: [{ source: "/contact", destination: "/feedback" }] },
    } as any),
    modules: {
      ...modules,
      middleware: {
        middleware: (req: Request) => {
          if (new URL(req.url).pathname === "/go") return new Response(null, { headers: { "x-middleware-rewrite": "/feedback" } });
          return undefined;
        },
        config: { matcher: ["/go"] },
      },
    },
    static: {
      exists: (p: string) => p === "/feedback/",
      isAsset: () => false,
      serve: async (_r: Request, p: string) => (p === "/feedback/" ? new Response("<p>feedback form</p>", { headers: { "content-type": "text/html" } }) : null),
    },
  });
  const get = (path: string) => h(new Request("http://localhost" + path, { redirect: "manual" }));

  it("serves a middleware rewrite in place (no 301)", async () => {
    const r = await get("/go");
    expect(r.status).toBe(200);
    expect(await r.text()).toContain("feedback form");
  });

  it("serves a config rewrite in place (no 301)", async () => {
    const r = await get("/contact");
    expect(r.status).toBe(200);
    expect(await r.text()).toContain("feedback form");
  });
});

describe("page region title swap", () => {
  it("keeps the page markers intact when the title length changes", async () => {
    const h = createKrateHandler({
      target: "node",
      data: data({
        pages: [{ route: "/titled", mode: "ssr", bundlePath: "titled" }],
        allPages: [{ route: "/titled", mode: "ssr" }],
        shells: { "/titled": "<html><head><title>t</title></head><body><!--suspense:page-->baked<!--/suspense:page--></body></html>" },
        hasMiddleware: false,
      }),
      modules: {
        ...modules,
        pages: {
          titled: {
            default: () => jsx("div", { children: [jsx("title", { children: "A much longer title" }), jsx("h1", { children: "hello" })] }),
          },
        },
      },
    });
    const r = await h(new Request("http://localhost/titled/"));
    const body = await r.text();
    expect(body).toContain("<title>A much longer title</title></head><body><!--suspense:page--><div><h1>hello</h1></div><!--/suspense:page--></body>");
    expect(body.match(/<title/g)?.length).toBe(1);
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
  const submit = (h: ReturnType<typeof mk>) =>
    h(
      new Request("http://localhost/__krate/action/act9", {
        method: "POST",
        headers: { "sec-fetch-site": "same-origin", "content-type": "application/x-www-form-urlencoded", referer: "http://localhost/form/" },
        body: "x=1",
      }),
    );
  const flashOf = (r: Response) => ((r.headers as any).getSetCookie() as string[]).find((c) => c.startsWith("krate_flash="));
  const page = (h: ReturnType<typeof mk>, cookie: string) => h(new Request("http://localhost/form/", { headers: { cookie } }));

  it("signs the cookie and shows the notice when it comes back", async () => {
    const h = mk("node", "a-shared-flash-secret-for-this-test");
    const set = flashOf(await submit(h));
    expect(set).toBeDefined();
    const value = set!.split(";")[0].slice("krate_flash=".length);
    expect(value).toMatch(/^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]{43}$/);
    const body = await (await page(h, "krate_flash=" + value)).text();
    expect(body).toContain("krate-flash");
    expect(body).toContain("Name is required");
  });

  it("ignores a cookie planted without a valid signature", async () => {
    const h = mk("node", "a-shared-flash-secret-for-this-test");
    const planted = Buffer.from(JSON.stringify({ a: "act9", r: { error: "Your account was suspended" } })).toString("base64url");
    for (const cookie of [planted, planted + ".", planted + ".AAAA", "x." + planted]) {
      const body = await (await page(h, "krate_flash=" + cookie)).text();
      expect(body, cookie).not.toContain("suspended");
      expect(body, cookie).not.toContain("krate-flash");
    }
  });

  it("serverless instances that share the secret accept each other's cookies, others do not", async () => {
    const a = mk("vercel", "a-shared-flash-secret-for-this-test");
    const b = mk("vercel", "a-shared-flash-secret-for-this-test");
    const stranger = mk("vercel", "some-other-deployments-secret-value");
    const value = flashOf(await submit(a))!.split(";")[0].slice("krate_flash=".length);
    expect(await (await page(b, "krate_flash=" + value)).text()).toContain("Name is required");
    expect(await (await page(stranger, "krate_flash=" + value)).text()).not.toContain("Name is required");
  });

  it("the node target without a secret signs with a per-process key", async () => {
    const saved = process.env.KRATE_FLASH_SECRET;
    delete process.env.KRATE_FLASH_SECRET;
    try {
      const one = mk("node");
      const other = mk("node");
      const value = flashOf(await submit(one))!.split(";")[0].slice("krate_flash=".length);
      expect(value).toContain(".");
      expect(await (await page(one, "krate_flash=" + value)).text()).toContain("Name is required");
      expect(await (await page(other, "krate_flash=" + value)).text()).not.toContain("Name is required");
    } finally {
      if (saved !== undefined) process.env.KRATE_FLASH_SECRET = saved;
    }
  });

  it("a serverless target without a secret carries no flash and says why", async () => {
    const saved = process.env.KRATE_FLASH_SECRET;
    delete process.env.KRATE_FLASH_SECRET;
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    try {
      const h = mk("cloudflare");
      const r = await submit(h);
      expect(r.status).toBe(303);
      expect(flashOf(r)).toBeUndefined();
      expect(warn).toHaveBeenCalledWith(expect.stringContaining("KRATE_FLASH_SECRET"));
      // A cookie from anywhere is not read either.
      const planted = Buffer.from(JSON.stringify({ a: "act9", r: { error: "planted" } })).toString("base64url");
      expect(await (await page(h, "krate_flash=" + planted)).text()).not.toContain("planted");
    } finally {
      warn.mockRestore();
      if (saved !== undefined) process.env.KRATE_FLASH_SECRET = saved;
    }
  });

  it("reads the key from KRATE_FLASH_SECRET", async () => {
    const saved = process.env.KRATE_FLASH_SECRET;
    process.env.KRATE_FLASH_SECRET = "env-flash-secret-env-flash-secret";
    try {
      const h = mk("vercel");
      const value = flashOf(await submit(h))!.split(";")[0].slice("krate_flash=".length);
      expect(await (await page(mk("vercel"), "krate_flash=" + value)).text()).toContain("Name is required");
    } finally {
      if (saved === undefined) delete process.env.KRATE_FLASH_SECRET;
      else process.env.KRATE_FLASH_SECRET = saved;
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

  it("vercel: uses the host, protocol and client address Vercel sets", async () => {
    const out = await echo(mk("vercel"), { "x-forwarded-host": "shop.example", "x-forwarded-proto": "https", "x-forwarded-for": "203.0.113.9", "x-real-ip": "203.0.113.9", forwarded: "for=6.6.6.6" });
    expect(out.url).toBe("https://shop.example/api/echo");
    expect(out.xff).toBe("203.0.113.9");
    expect(out.realIp).toBe("203.0.113.9");
    expect(out.forwarded).toBeNull();
  });

  it("vercel: a client-written forwarded chain never becomes the client address", async () => {
    const out = await echo(mk("vercel"), { ...spoof, "x-forwarded-host": "shop.example", "x-real-ip": "203.0.113.9" });
    expect(out.xff).toBe("203.0.113.9");
    expect(out.forwarded).toBeNull();
  });

  it("vercel with server.trustProxy: false trusts no forwarded header", async () => {
    const out = await echo(mk("vercel", false), { "x-forwarded-host": "shop.example", "x-forwarded-proto": "https", "x-forwarded-for": "203.0.113.9", "x-real-ip": "203.0.113.9" });
    expect(out.url).toBe("http://app.internal/api/echo");
    expect(out.host).toBe("app.internal");
    expect(out.proto).toBe("http");
    expect(out.xff).toBeNull();
    expect(out.realIp).toBeNull();
  });

  for (const [target, ipHeader] of [["netlify", "x-nf-client-connection-ip"], ["cloudflare", "cf-connecting-ip"]] as const) {
    it(`${target}: host and protocol come from the request URL, the address from ${ipHeader}`, async () => {
      const out = await echo(mk(target), { ...spoof, [ipHeader]: "203.0.113.9" });
      expect(out.url).toBe("http://app.internal/api/echo");
      expect(out.host).toBe("app.internal");
      expect(out.proto).toBe("http");
      expect(out.xff).toBe("203.0.113.9");
      expect(out.realIp).toBe("203.0.113.9");
      expect(out.forwarded).toBeNull();
    });

    it(`${target}: without the platform address header there is no client address`, async () => {
      const out = await echo(mk(target), spoof);
      expect(out.xff).toBeNull();
      expect(out.realIp).toBeNull();
    });

    it(`${target} with server.trustProxy: true trusts the proxy of yours in front`, async () => {
      const out = await echo(mk(target, true), { ...spoof, [ipHeader]: "203.0.113.9" });
      expect(out.url).toBe("https://evil.example/api/echo");
      expect(out.xff).toBe("6.6.6.6, 7.7.7.7");
    });

    it(`${target} with server.trustProxy: false ignores everything forwarded, the platform address included`, async () => {
      const out = await echo(mk(target, false), { ...spoof, [ipHeader]: "203.0.113.9" });
      expect(out.url).toBe("http://app.internal/api/echo");
      expect(out.xff).toBeNull();
      expect(out.realIp).toBeNull();
    });
  }

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

  it("node: forwarded headers are read only with trustProxy: true", async () => {
    const headers = { "x-forwarded-host": "shop.example", "x-forwarded-proto": "https" };
    expect((await echo(mk("node"), headers)).url).toBe("http://app.internal/api/echo");
    expect((await echo(mk("node", true), headers)).url).toBe("https://shop.example/api/echo");
  });
});

// Serverless targets run many instances. Passing a cache adapter to
// createKrateHandler (or setting cache.adapter in krate.config) shares the ISR
// and data caches between them, and revalidateTag on one reaches the others.
describe("createKrateHandler with a shared cache adapter", () => {
  it("serves one instance's page from another and revalidates for both", async () => {
    const { MemoryAdapter } = await import("./cache-adapter.js");
    const { revalidateTag } = await import("./cache.js");
    let renders = 0;
    const shared = new MemoryAdapter();
    const make = () =>
      createKrateHandler({
        target: "vercel",
        data: data({
          apiRoutes: ["bump"],
          pages: [{ route: "/tagged", mode: "isr", revalidate: 300, bundlePath: "tagged" }],
          allPages: [{ route: "/tagged", mode: "isr" }],
          shells: { "/tagged": "<html><head></head><body><!--suspense:page-->x<!--/suspense:page--></body></html>" },
          hasMiddleware: false,
        }),
        modules: {
          pages: {
            tagged: {
              default: async () => {
                // A tagged read: revalidateTag("posts") must drop this page everywhere.
                const { recordCacheRead } = await import("./cache-scope.js");
                recordCacheRead(["posts"], undefined);
                return jsx("main", { children: "render " + ++renders });
              },
            },
          },
          api: { bump: { POST: () => (revalidateTag("posts"), Response.json({ ok: true })) } },
          actions: {},
        } as any,
        cache: shared,
      });
    const a = make();
    const b = make();
    const get = (h: typeof a, path: string, init?: RequestInit) => h(new Request("http://localhost" + path, init));

    const first = await get(a, "/tagged/");
    expect(first.headers.get("x-krate-cache")).toBe("MISS");
    const other = await get(b, "/tagged/");
    expect(other.headers.get("x-krate-cache")).toBe("HIT");
    expect(await other.text()).toContain("render 1");
    expect(renders).toBe(1);

    const bump = await get(a, "/api/bump", { method: "POST" });
    expect(bump.status).toBe(200);
    const after = await get(b, "/tagged/");
    expect(after.headers.get("x-krate-cache")).toBe("MISS");
    expect(await after.text()).toContain("render 2");
    expect(renders).toBe(2);
  });
});
