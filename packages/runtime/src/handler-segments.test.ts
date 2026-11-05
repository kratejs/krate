// @vitest-environment node
import { describe, expect, it } from "vitest";
import { fillServerError, lookupSegmentPage, newServerError, SEGMENT_KIND_OF_STATUS, type SegmentEntry } from "./handler-segments.js";
import { createKrateHandler, type HandlerConfig, type KrateServerData } from "./server-handler.js";
import { jsx } from "./server.js";

describe("lookupSegmentPage (port of build/segments.go)", () => {
  it("URL walk", () => {
    const entries: SegmentEntry[] = [
      { key: "", dir: "", notFound: "404.html", error: "500.html" },
      { key: "blog", dir: "blog", notFound: "blog/404.html" },
      { key: "blog/new", dir: "blog/new", notFound: "blog/new/404.html" },
      { key: "blog/[slug]", dir: "blog/[slug]", notFound: "blog/[slug]/404.html" },
      { key: "docs/[...rest]", dir: "docs/[...rest]", notFound: "docs/[...rest]/404.html" },
      { key: "(shop)", dir: "", group: true, notFound: "(shop)/404.html" },
      { key: "(shop)/cart", dir: "cart", group: true, notFound: "(shop)/cart/404.html" },
    ];
    const cases: Array<[string, "notFound" | "error", string, string?]> = [
      ["/", "notFound", "404.html"],
      ["/nope", "notFound", "404.html"],
      ["/blog/", "notFound", "blog/404.html"],
      ["/blog", "notFound", "blog/404.html"],
      ["/blog/new/x/y", "notFound", "blog/new/404.html"],
      ["/blog/hello", "notFound", "blog/[slug]/404.html", "hello"],
      ["/blog/hello/deeper/still", "notFound", "blog/[slug]/404.html", "hello"],
      ["/docs", "notFound", "404.html"],
      ["/docs/a/b/c", "notFound", "docs/[...rest]/404.html"],
      ["/cart/x", "notFound", "404.html"],
      ["/blog/x", "error", "500.html"],
    ];
    for (const [path, kind, want, slug] of cases) {
      const m = lookupSegmentPage(entries, kind, path, false, "");
      expect(m && m.file, `${kind} ${path}`).toBe(want);
      if (slug) expect(m!.params!.slug).toBe(slug);
    }
    expect(lookupSegmentPage([], "notFound", "/x", false, "")).toBeNull();
    expect(lookupSegmentPage([{ key: "blog", dir: "blog", notFound: "blog/404.html" }], "notFound", "/x", false, "")).toBeNull();
  });

  it("finds forbidden and unauthorized pages like not-found pages", () => {
    const entries: SegmentEntry[] = [
      { key: "", dir: "", forbidden: "403.html" },
      { key: "admin", dir: "admin", forbidden: "admin/403.html", unauthorized: "admin/401.html" },
    ];
    expect(lookupSegmentPage(entries, "forbidden", "/x", true, "admin/users")?.file).toBe("admin/403.html");
    expect(lookupSegmentPage(entries, "forbidden", "/x", true, "blog")?.file).toBe("403.html");
    expect(lookupSegmentPage(entries, "unauthorized", "/x", true, "admin")?.file).toBe("admin/401.html");
    expect(lookupSegmentPage(entries, "unauthorized", "/x", true, "blog")).toBeNull();
    expect(lookupSegmentPage(entries, "forbidden", "/admin/zzz", false, "")?.file).toBe("admin/403.html");
    expect(SEGMENT_KIND_OF_STATUS[401]).toBe("unauthorized");
    expect(SEGMENT_KIND_OF_STATUS[403]).toBe("forbidden");
  });

  it("route walk", () => {
    const entries: SegmentEntry[] = [
      { key: "", dir: "", notFound: "404.html" },
      { key: "(shop)", dir: "", group: true, notFound: "(shop)/404.html", error: "(shop)/500.html" },
      { key: "(shop)/cart", dir: "cart", group: true, notFound: "(shop)/cart/404.html" },
      { key: "blog", dir: "blog", error: "blog/500.html" },
    ];
    const cases: Array<[string, "notFound" | "error", string]> = [
      ["", "notFound", "404.html"],
      ["(shop)", "notFound", "(shop)/404.html"],
      ["(shop)/cart", "notFound", "(shop)/cart/404.html"],
      ["(shop)/cart/[id]", "notFound", "(shop)/cart/404.html"],
      ["(shop)/cart", "error", "(shop)/500.html"],
      ["blog", "notFound", "404.html"],
      ["blog/[slug]", "error", "blog/500.html"],
      ["other", "error", ""],
    ];
    for (const [segDir, kind, want] of cases) {
      const m = lookupSegmentPage(entries, kind, "/ignored", true, segDir);
      expect(m ? m.file : "", `${kind} ${segDir}`).toBe(want);
    }
  });
});

describe("error prop", () => {
  it("is sanitized in production and escaped into the page", () => {
    const e = newServerError("secret <b>", "at x", "abc1234567", false);
    expect(e).toEqual({ name: "Error", message: "Internal Server Error", digest: "abc1234567" });
    const dev = newServerError("a<b>", "at y", "d", true);
    const out = fillServerError("<html><head></head><body>__KRATE_ERR_MESSAGE__|__KRATE_ERR_DIGEST__|__KRATE_ERR_STACK__</body></html>", dev);
    expect(out).toContain("a&lt;b&gt;|d|at y");
    expect(out).toContain('<script type="application/json" id="__krate_error">{"name":"Error","message":"a\\u003cb\\u003e","digest":"d","stack":"at y"}</script></head>');
  });
});

const config: HandlerConfig = {
  basePath: "",
  trustProxy: true,
  maxBodyBytes: 1 << 20,
  maxAPIBodyBytes: 1 << 20,
  redirects: [],
  rewrites: [],
  headers: [],
  security: { nosniff: true, frameOptions: "", referrerPolicy: "", permissionsPolicy: "", hsts: "", csp: "" },
  cors: { enabled: false, origins: [], methods: [], headers: [], credentials: false, maxAge: 0 },
  ssr: { timeout: 2000, maxCacheSize: 16, ppr: false, pprRevalidate: 60 },
};

const shell = (id: string) => `<html><head><title>t</title></head><body><!--suspense:page-->baked ${id}<!--/suspense:page--></body></html>`;

function segData(): KrateServerData {
  return {
    version: 1,
    config,
    pages: [
      { route: "/blog/boom", mode: "ssr", bundlePath: "boom", segDir: "blog" },
      { route: "/blog/gone", mode: "ssr", bundlePath: "gone", segDir: "blog" },
      { route: "/blog/[slug]", mode: "isr", revalidate: 30, bundlePath: "slug", segDir: "blog" },
      { route: "/boom", mode: "ssr", bundlePath: "boom", segDir: "" },
    ],
    regions: {},
    allPages: [{ route: "/blog/[slug]", mode: "isr", segDir: "blog" }, { route: "/blog/closed/[id]", mode: "ssg", segDir: "blog" }],
    shells: { "/blog/boom": shell("a"), "/blog/gone": shell("b"), "/blog/[slug]": shell("c"), "/boom": shell("d") },
    templates: [{ pattern: "blog/closed/[id]", html: "<html><head></head><body>t</body></html>" }],
    staticOnlyRoutes: ["/blog/closed/[id]"],
    hashedAssets: [],
    html404: "<h1>global 404</h1>",
    html500: "",
    segments: [
      { key: "", dir: "", notFound: "404.html", error: "500.html" },
      { key: "blog", dir: "blog", notFound: "blog/404.html", error: "blog/500.html" },
      { key: "docs/[...rest]", dir: "docs/[...rest]", notFound: "docs/[...rest]/404.html" },
    ],
    segmentPages: {
      "404.html": "<html><head></head><body>root 404</body></html>",
      "500.html": "<html><head></head><body>root 500 __KRATE_ERR_DIGEST__</body></html>",
      "blog/404.html": "<html><head></head><body>blog 404</body></html>",
      "blog/500.html": "<html><head></head><body>blog 500 __KRATE_ERR_MESSAGE__ __KRATE_ERR_DIGEST__ __KRATE_ERR_STACK__</body></html>",
      "docs/[...rest]/404.html": "<html><head></head><body>docs 404 __KRATE_PARAM_rest__</body></html>",
    },
    apiRoutes: [],
    actions: [],
    hasMiddleware: false,
  };
}

let slugRenders = 0;
const mods = {
  pages: {
    boom: { default: () => { throw new Error("secret <boom>"); } },
    gone: { default: async () => { const { notFound } = await import("./server.js"); notFound(); return jsx("p", {}); } },
    slug: { default: () => jsx("main", { children: "slug " + ++slugRenders }) },
  },
};

describe("segment pages in the handler", () => {
  const handler = createKrateHandler({ target: "node", data: segData(), modules: mods as any });
  const get = (p: string) => handler(new Request("http://localhost" + p, { headers: { accept: "text/html" } }));
  const errors = console.error;
  const quiet = () => { console.error = () => {}; };
  const loud = () => { console.error = errors; };

  it("unmatched URLs use the nearest segment 404", async () => {
    const r = await get("/blog/nothing/at/all");
    expect(r.status).toBe(404);
    expect(r.headers.get("cache-control")).toBe("no-store");
    expect(await r.text()).toContain("blog 404");
    expect(await (await get("/zzz")).text()).toContain("root 404");
    const d = await get("/docs/a/b");
    expect(d.status).toBe(404);
    expect(await d.text()).toContain("docs 404 a/b");
  });

  it("a closed dynamic route resolves through its directory", async () => {
    const r = await get("/blog/closed/42/");
    expect(r.status).toBe(404);
    expect(await r.text()).toContain("blog 404");
  });

  it("notFound() answers the route's nearest page and is not cached", async () => {
    const r = await get("/blog/gone/");
    expect(r.status).toBe(404);
    expect(r.headers.get("cache-control")).toBe("no-store");
    expect(await r.text()).toContain("blog 404");
  });

  it("a failed render answers the segment error page with the digest", async () => {
    quiet();
    try {
      const r = await get("/blog/boom/");
      expect(r.status).toBe(500);
      expect(r.headers.get("x-krate-error")).toBe("render");
      expect(r.headers.get("cache-control")).toBe("no-store");
      const body = await r.text();
      expect(body).toContain("blog 500 Internal Server Error ");
      expect(body).not.toContain("secret");
      const m = /"digest":"([0-9a-f]{10})"/.exec(body);
      expect(m).not.toBeNull();
      expect(body).toContain('id="__krate_error"');
      const root = await (await get("/boom/")).text();
      expect(root).toContain("root 500 ");
    } finally {
      loud();
    }
  });

  it("ISR serves from cache, but never stores failed or not-found renders", async () => {
    const a = await get("/blog/x/");
    expect(a.status).toBe(200);
    expect(a.headers.get("x-krate-cache")).toBe("MISS");
    expect((await get("/blog/x/")).headers.get("x-krate-cache")).toBe("HIT");
    expect(slugRenders).toBe(1);
  });

  it("an ISR page that throws is rendered again, not cached", async () => {
    quiet();
    try {
      let n = 0;
      const d = segData();
      const h = createKrateHandler({
        target: "node",
        data: d,
        modules: { pages: { ...mods.pages, slug: { default: () => { n++; throw new Error("x"); } } } } as any,
      });
      for (let i = 0; i < 2; i++) {
        const r = await h(new Request("http://localhost/blog/y/"));
        expect(r.status).toBe(500);
        expect(r.headers.get("cache-control")).toBe("no-store");
        expect(await r.text()).toContain("blog 500");
      }
      expect(n).toBe(2);
    } finally {
      loud();
    }
  });
});
