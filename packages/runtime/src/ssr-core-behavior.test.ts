// @vitest-environment node
import { AsyncLocalStorage } from "node:async_hooks";
import { createHash } from "node:crypto";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createSSRCore, variantKey, type ManifestPage, type RegionMeta, type SSRCoreOptions } from "./ssr-core.js";
import { installRenderStore, markNoStore, recordCacheRead, type CacheScope } from "./cache-scope.js";
import { forbidden, notFound, unauthorized } from "./request-context.js";
import { jsx } from "./server.js";

// The renderer installs the cache scope store once per process; so does this file.
installRenderStore(new AsyncLocalStorage<CacheScope>());

let errors: unknown[][] = [];
beforeEach(() => {
  errors = [];
  vi.spyOn(console, "error").mockImplementation((...a: unknown[]) => void errors.push(a));
  vi.spyOn(console, "log").mockImplementation(() => {});
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
});

type Component = (props: any) => any;

interface Setup {
  pages?: ManifestPage[];
  regions?: Record<string, RegionMeta[]>;
  modules?: Record<string, any>;
  runtime?: Record<string, (propsJSON: string) => string | Promise<string>>;
  opts?: Partial<SSRCoreOptions>;
}

function make(s: Setup) {
  const background: Promise<unknown>[] = [];
  const core = createSSRCore({
    manifest: { pages: s.pages || [], regions: s.regions || {} },
    loadPageModule: async (page) => {
      const m = (s.modules || {})[page.bundlePath || page.source];
      if (!m) throw new Error("no module " + page.source);
      return m;
    },
    loadRuntimeRenderer: (rel) => {
      const r = (s.runtime || {})[rel];
      if (!r) throw new Error("no runtime bundle " + rel);
      return r;
    },
    background: (p) => void background.push(p),
    ...s.opts,
  });
  return { core, settle: () => Promise.allSettled(background.splice(0)) };
}

const req = (route: string, extra: Record<string, unknown> = {}) => ({ route, url: route, method: "GET", headers: {}, ...extra }) as any;
const page = (route: string, mode = "ssr", extra: Partial<ManifestPage> = {}): ManifestPage => ({ route, source: route + ".tsx", mode, bundlePath: route, ...extra });
const text = (s: string): Component => () => jsx("p", { children: s });

describe("variantKey", () => {
  it("sorts and encodes params and query, and keeps them apart", () => {
    expect(variantKey({ route: "/r" })).toBe("/r");
    expect(variantKey({ route: "/r", params: { b: "2", a: "1 x" } })).toBe("/r?a=1%20x&b=2");
    expect(variantKey({ route: "/r", query: { q: "a&b=c" } })).toBe("/r#q=a%26b%3Dc");
    // a param and a query value with the same name are different variants
    expect(variantKey({ route: "/r", params: { a: "1" } })).not.toBe(variantKey({ route: "/r", query: { a: "1" } }));
    expect(variantKey({ route: "/r", params: { a: "1" }, query: { a: "1" } })).toBe("/r?a=1#a=1");
  });

  it("gives two dynamic params two keys", () => {
    expect(variantKey({ route: "/v/[id]", params: { id: "a" } })).not.toBe(variantKey({ route: "/v/[id]", params: { id: "b" } }));
  });
});

describe("renderPage", () => {
  it("answers 404 for a route that is not in the manifest", async () => {
    const { core } = make({ pages: [page("/a")] });
    expect(await core.renderPage(req("/zzz"))).toMatchObject({ status: 404, notFound: true, html: "" });
  });

  it("finds a page with or without its trailing slash, and keeps '/' intact", async () => {
    const { core } = make({
      pages: [page("/a/"), page("/b"), page("/")],
      modules: { "/a/": { default: text("A") }, "/b": { default: text("B") }, "/": { default: text("root") } },
    });
    expect((await core.renderPage(req("/a"))).html).toBe("<p>A</p>");
    expect((await core.renderPage(req("/b/"))).html).toBe("<p>B</p>");
    expect((await core.renderPage(req("/"))).html).toBe("<p>root</p>");
    expect(core.findPage("/missing/")).toBeUndefined();
  });

  it("escapes text and attribute values coming from the request", async () => {
    const { core } = make({
      pages: [page("/e")],
      modules: {
        "/e": {
          default: (props: any) =>
            jsx("div", { title: props.query.t, children: [jsx("p", { children: props.query.t }), jsx("p", { children: props.params.id })] }),
        },
      },
    });
    const r = await core.renderPage(req("/e", { query: { t: `"><script>alert(1)</script>` }, params: { id: "<b>&" } }));
    expect(r.status).toBe(200);
    expect(r.html).not.toContain("<script>");
    expect(r.html).not.toContain("<b>");
    expect(r.html).toContain("&lt;script&gt;");
    expect(r.html).toContain("title=");
    expect(r.html).toContain("&lt;b&gt;&amp;");
  });

  it("hands the page awaitable params and searchParams", async () => {
    const { core } = make({
      pages: [page("/p/[id]")],
      modules: {
        "/p/[id]": {
          default: async (props: any) => {
            const { id } = await props.params;
            const { tab } = await props.searchParams;
            return jsx("p", { children: `${id}/${tab}/${props.params.id}/${props.searchParams.tab}` });
          },
        },
      },
    });
    const r = await core.renderPage(req("/p/[id]", { params: { id: "7" }, query: { tab: "x" } }));
    expect(r.html).toBe("<p>7/x/7/x</p>");
  });

  it("serves request headers and cookies to server components", async () => {
    const { getRequestContext } = await import("./request-context.js");
    const seen: string[] = [];
    const { core } = make({
      pages: [page("/h")],
      modules: {
        "/h": {
          default: () => {
            const c = getRequestContext()!;
            seen.push(c.headers.get("x-test") + "|" + c.cookies.get("sid"));
            return jsx("p", { children: "ok" });
          },
        },
      },
    });
    await core.renderPage(req("/h", { headers: { "x-test": "yes", cookie: "sid=abc; other=1" } }));
    expect(seen).toEqual(["yes|abc"]);
  });

  describe("failures", () => {
    const boom = (): Component => () => {
      const e = new Error("boom");
      e.stack = "at x";
      throw e;
    };

    it("answers 500 with a short digest of the error, never the error text in the HTML", async () => {
      const { core } = make({ pages: [page("/b")], modules: { "/b": { default: boom() } } });
      const r = await core.renderPage(req("/b"));
      expect(r.status).toBe(500);
      expect(r.html).toBe("");
      expect(r.error).toBe("boom");
      expect(r.digest).toMatch(/^[0-9a-f]{10}$/);
      expect(errors.flat().join(" ")).toContain(`digest ${r.digest}`);
    });

    it("computes the digest as sha256(message + newline + stack) cut to 10 hex digits, the same on every host", async () => {
      // Pinned: build/segmentserve.go errorDigest must produce this exact value.
      const golden = "8a2474a04d";
      const portable = make({ pages: [page("/b")], modules: { "/b": { default: boom() } } });
      const node = make({
        pages: [page("/b")],
        modules: { "/b": { default: boom() } },
        opts: { sha256Hex: (s) => createHash("sha256").update(s).digest("hex") },
      });
      expect((await portable.core.renderPage(req("/b"))).digest).toBe(golden);
      expect((await node.core.renderPage(req("/b"))).digest).toBe(golden);
      expect(createHash("sha256").update("boom\nat x").digest("hex").slice(0, 10)).toBe(golden);
    });

    it("turns a missing default export into a 500", async () => {
      const { core } = make({ pages: [page("/n")], modules: { "/n": {} } });
      expect(await core.renderPage(req("/n"))).toMatchObject({ status: 500, html: "" });
    });

    it("turns a module that cannot load into a 500", async () => {
      const { core } = make({ pages: [page("/n")], modules: {} });
      const r = await core.renderPage(req("/n"));
      expect(r.status).toBe(500);
      expect(r.error).toContain("no module");
    });

    it("maps forbidden() to 403 and unauthorized() to 401 and never caches them", async () => {
      const { core } = make({
        pages: [{ ...page("/fb"), mode: "isr", revalidate: 60 } as any, page("/un")],
        modules: { "/fb": { default: () => forbidden() }, "/un": { default: () => unauthorized() } },
      });
      expect(await core.renderPage(req("/fb"))).toMatchObject({ status: 403, denied: 403, html: "" });
      expect(await core.renderPage(req("/fb"))).toMatchObject({ status: 403, denied: 403 });
      expect((await core.renderPage(req("/fb"))).cacheStatus).not.toBe("hit");
      expect(await core.renderPage(req("/un"))).toMatchObject({ status: 401, denied: 401 });
      expect(await core.renderPage(req("/un"))).not.toHaveProperty("notFound");
    });

    it("maps notFound() to 404 and a redirect signal to 307", async () => {
      const { core } = make({
        pages: [page("/nf"), page("/rd"), page("/rd2")],
        modules: {
          "/nf": { default: () => notFound() },
          "/rd": {
            default: () => {
              throw Object.assign(new Error("NEXT_REDIRECT"), { __krateRedirect: "/login" });
            },
          },
          "/rd2": {
            default: () => {
              throw Object.assign(new Error("x"), { digest: "NEXT_REDIRECT;replace;/next;307" });
            },
          },
        },
      });
      expect(await core.renderPage(req("/nf"))).toMatchObject({ status: 404, notFound: true });
      expect(await core.renderPage(req("/rd"))).toMatchObject({ status: 307, redirect: "/login" });
      expect(await core.renderPage(req("/rd2"))).toMatchObject({ status: 307, redirect: "/next" });
      expect(errors).toEqual([]);
    });

    it("stops a render that outlives the timeout", async () => {
      const { core } = make({
        pages: [page("/slow")],
        modules: { "/slow": { default: () => new Promise(() => {}) } },
        opts: { timeoutMs: 30 },
      });
      const r = await core.renderPage(req("/slow"));
      expect(r.status).toBe(500);
      expect(r.error).toBe("render timed out after 30ms");
    });
  });
});

describe("ISR pages", () => {
  function isr(extra: Partial<SSRCoreOptions> = {}, revalidate = 10) {
    let n = 0;
    const s = make({
      pages: [page("/i/[id]", "isr", { revalidate })],
      modules: {
        "/i/[id]": {
          default: (props: any) => {
            const q = props.searchParams.page;
            return jsx("p", { children: `${props.params.id}:${q ?? "-"}:${++n}` });
          },
        },
      },
      opts: extra,
    });
    return { ...s, renders: () => n, get: (id: string, query?: Record<string, string>) => s.core.renderPage(req("/i/[id]", { params: { id }, query })) };
  }

  it("reports miss, then hit, and keeps variants apart", async () => {
    const { get, renders } = isr();
    expect(await get("a")).toMatchObject({ cacheStatus: "miss", html: "<p>a:-:1</p>" });
    expect(await get("a")).toMatchObject({ cacheStatus: "hit", cached: true, html: "<p>a:-:1</p>" });
    expect(await get("b")).toMatchObject({ cacheStatus: "miss", html: "<p>b:-:2</p>" });
    expect(renders()).toBe(2);
  });

  it("keys the cache by the query keys the page read, and only those", async () => {
    const { get, renders } = isr();
    expect((await get("a", { page: "1", utm: "x" })).cacheStatus).toBe("miss");
    // the page read `page`, not `utm`: another utm value shares the entry
    expect(await get("a", { page: "1", utm: "y" })).toMatchObject({ cacheStatus: "hit", html: "<p>a:1:1</p>" });
    // another `page` is another page
    expect(await get("a", { page: "2" })).toMatchObject({ cacheStatus: "miss", html: "<p>a:2:2</p>" });
    expect(renders()).toBe(2);
  });

  it("keys a page that enumerates the whole query by the whole query", async () => {
    let n = 0;
    const { core } = make({
      pages: [page("/all", "isr", { revalidate: 10 })],
      modules: { "/all": { default: (props: any) => jsx("p", { children: `${JSON.stringify(Object.keys(props.searchParams).sort())}:${++n}` }) } },
    });
    const get = (query: Record<string, string>) => core.renderPage(req("/all", { query }));
    expect((await get({ a: "1" })).cacheStatus).toBe("miss");
    expect((await get({ a: "1" })).cacheStatus).toBe("hit");
    expect((await get({ a: "1", b: "2" })).cacheStatus).toBe("miss");
    expect(n).toBe(2);
  });

  it("serves a stale entry at once and refreshes it once in the background", async () => {
    vi.useFakeTimers();
    const { get, renders, settle } = isr({}, 10);
    await get("a");
    vi.setSystemTime(Date.now() + 11_000);
    const [s1, s2] = await Promise.all([get("a"), get("a")]);
    expect(s1).toMatchObject({ cacheStatus: "stale", html: "<p>a:-:1</p>" });
    expect(s2.cacheStatus).toBe("stale");
    await settle();
    // two concurrent stale requests shared one refresh
    expect(renders()).toBe(2);
    expect(await get("a")).toMatchObject({ cacheStatus: "hit", html: "<p>a:-:2</p>" });
  });

  it("does not cache a render that opted out, nor one that was invalidated while it ran", async () => {
    let n = 0;
    const { core } = make({
      pages: [page("/ns", "isr", { revalidate: 10 })],
      modules: {
        "/ns": {
          default: () => {
            markNoStore();
            return jsx("p", { children: String(++n) });
          },
        },
      },
    });
    const a = await core.renderPage(req("/ns"));
    const b = await core.renderPage(req("/ns"));
    expect(a).toMatchObject({ noStore: true, cacheStatus: "miss", html: "<p>1</p>" });
    expect(b).toMatchObject({ cacheStatus: "miss", html: "<p>2</p>" });
  });

  it("ties entries to the tags the render read, so a tag invalidation finds them", async () => {
    let n = 0;
    const changes: boolean[] = [];
    const { core } = make({
      pages: [page("/t/[id]", "isr", { revalidate: 100 })],
      modules: {
        "/t/[id]": {
          default: (props: any) => {
            recordCacheRead(["posts", "post:" + props.params.id], undefined);
            return jsx("p", { children: String(++n) });
          },
        },
      },
      opts: { onIsrChange: (immediate) => void changes.push(immediate) },
    });
    const get = (id: string) => core.renderPage(req("/t/[id]", { params: { id } }));
    await get("1");
    await get("2");
    expect((await get("1")).cacheStatus).toBe("hit");
    expect(changes).toEqual([false, false]);

    const out = await core.applyInternalOp("cache-invalidate", { tags: ["post:1"] });
    expect(out).toMatchObject({ ok: true, isrEntries: 1 });
    expect(changes[changes.length - 1]).toBe(true);
    expect((await get("1")).cacheStatus).toBe("miss");
    expect((await get("2")).cacheStatus).toBe("hit");

    const all = await core.applyInternalOp("cache-invalidate", { tags: ["posts"] });
    expect(all.isrEntries).toBe(2);
  });
});

describe("applyInternalOp", () => {
  function counter() {
    let n = 0;
    const s = make({
      pages: [page("/c/[id]", "isr", { revalidate: 100 }), page("/live", "ssr")],
      modules: {
        "/c/[id]": { default: (props: any) => jsx("p", { children: `${props.params?.id ?? "base"}:${++n}` }) },
        "/live": { default: text("live") },
      },
    });
    return { ...s, renders: () => n, get: (id?: string) => s.core.renderPage(req("/c/[id]", id ? { params: { id } } : {})) };
  }

  it("cache-invalidate with a path target drops one params variant, or the whole route without params", async () => {
    const { core, get } = counter();
    await get("a");
    await get("b");
    expect(await core.applyInternalOp("cache-invalidate", { targets: [{ route: "/c/[id]", params: { id: "a" } }] })).toMatchObject({ ok: true, isrEntries: 1 });
    expect((await get("a")).cacheStatus).toBe("miss");
    expect((await get("b")).cacheStatus).toBe("hit");
    expect(await core.applyInternalOp("cache-invalidate", { targets: [{ route: "/c/[id]" }] })).toMatchObject({ isrEntries: 2 });
    expect((await get("b")).cacheStatus).toBe("miss");
  });

  it("cache-invalidate ignores malformed input", async () => {
    const { core } = counter();
    expect(await core.applyInternalOp("cache-invalidate", { tags: [1, "", null], targets: [null, { route: 5 }, "x"] })).toMatchObject({ ok: true, isrEntries: 0, regionEntries: 0 });
  });

  it("revalidate-route clears every variant and renders the base variant again", async () => {
    const { core, get, renders } = counter();
    await get("a");
    const before = renders();
    expect(await core.applyInternalOp("revalidate-route", { route: "/c/[id]" })).toEqual({ ok: true });
    expect(renders()).toBe(before + 1);
    expect((await get("a")).cacheStatus).toBe("miss");
  });

  it("refresh-route does nothing for a page that is not ISR", async () => {
    const { core } = counter();
    expect(await core.applyInternalOp("refresh-route", { route: "/live" })).toEqual({ ok: true, refreshed: 0 });
    expect(await core.applyInternalOp("refresh-route", { route: "/missing" })).toEqual({ ok: true, refreshed: 0 });
  });

  it("devInvalidate drops cached variants of a route", async () => {
    const { core, get } = counter();
    await get("a");
    core.devInvalidate("/c/[id]");
    expect((await get("a")).cacheStatus).toBe("miss");
  });
});

describe("regions", () => {
  const region = (id: string, extra: Partial<RegionMeta> = {}): RegionMeta => ({ id, bundlePath: "r/" + id, ...extra });

  it("rejects a region without a bundle; it is fatal only for a page with server components", async () => {
    const plain = make({ pages: [page("/p")], regions: {} });
    expect(await plain.core.renderRegion(page("/p"), { id: "x" }, req("/p"))).toMatchObject({ type: "error", id: "x", status: 400, fatal: false });
    const sc = make({ pages: [page("/p", "ssr", { serverComponents: true })] });
    expect(await sc.core.renderRegion(page("/p", "ssr", { serverComponents: true }), { id: "x" }, req("/p"))).toMatchObject({ fatal: true });
  });

  it("passes the baked props with the request params and query to the runtime bundle", async () => {
    const seen: any[] = [];
    const { core } = make({
      pages: [page("/p")],
      runtime: {
        "r/a": (json) => {
          seen.push(JSON.parse(json));
          return "<i>A</i>";
        },
      },
    });
    const frame = await core.renderRegion(page("/p"), region("a", { props: { price: 5 } }), req("/p", { params: { id: "1" }, query: { q: "x" } }));
    expect(frame).toEqual({ type: "region", id: "a", status: 200, html: "<i>A</i>" });
    expect(seen).toEqual([{ price: 5, params: { id: "1" }, query: { q: "x" } }]);
    seen.length = 0;
    await core.renderRegion(page("/p"), region("a", { props: { price: 5 } }), req("/p", { params: {}, query: {} }));
    expect(seen).toEqual([{ price: 5 }]);
  });

  it("does not cache regions of a plain SSR page", async () => {
    let n = 0;
    const { core } = make({ pages: [page("/p")], runtime: { "r/a": () => `<i>${++n}</i>` } });
    const a = await core.renderRegion(page("/p"), region("a"), req("/p"));
    const b = await core.renderRegion(page("/p"), region("a"), req("/p"));
    expect([a.html, b.html, a.cacheStatus]).toEqual(["<i>1</i>", "<i>2</i>", undefined]);
  });

  it("caches regions of an ISR page for the page window: miss, hit, stale with one background refresh", async () => {
    vi.useFakeTimers();
    let n = 0;
    const p = page("/p/[id]", "isr", { revalidate: 10 });
    const { core, settle } = make({ pages: [p], runtime: { "r/a": () => `<i>${++n}</i>` } });
    const r = region("a");
    const q = req("/p/[id]", { params: { id: "1" } });
    expect(await core.renderRegion(p, r, q)).toMatchObject({ html: "<i>1</i>", cacheStatus: "miss" });
    expect(await core.renderRegion(p, r, q)).toMatchObject({ html: "<i>1</i>", cacheStatus: "hit", cached: true });
    // another params variant is another entry
    expect(await core.renderRegion(p, r, req("/p/[id]", { params: { id: "2" } }))).toMatchObject({ html: "<i>2</i>", cacheStatus: "miss" });
    vi.setSystemTime(Date.now() + 11_000);
    const [a, b] = await Promise.all([core.renderRegion(p, r, q), core.renderRegion(p, r, q)]);
    expect([a.cacheStatus, b.cacheStatus]).toEqual(["stale", "stale"]);
    await settle();
    expect(n).toBe(3);
    expect(await core.renderRegion(p, r, q)).toMatchObject({ html: "<i>3</i>", cacheStatus: "hit" });
  });

  it("lets the region's own window win, and ppr caches the regions of a non-ISR page", async () => {
    let n = 0;
    const own = make({ pages: [page("/p")], runtime: { "r/a": () => `<i>${++n}</i>` } });
    const p = page("/p");
    expect((await own.core.renderRegion(p, region("a", { revalidate: 30 }), req("/p"))).cacheStatus).toBe("miss");
    expect((await own.core.renderRegion(p, region("a", { revalidate: 30 }), req("/p"))).cacheStatus).toBe("hit");

    let m = 0;
    const ppr = make({ pages: [page("/p")], runtime: { "r/a": () => `<i>${++m}</i>` }, opts: { ppr: true } });
    expect((await ppr.core.renderRegion(p, region("a"), req("/p"))).cacheStatus).toBe("miss");
    expect((await ppr.core.renderRegion(p, region("a"), req("/p"))).cacheStatus).toBe("hit");
  });

  it("does not cache a region that read uncached data", async () => {
    let n = 0;
    const p = page("/p/[id]", "isr", { revalidate: 10 });
    const { core } = make({
      pages: [p],
      runtime: {
        "r/a": () => {
          markNoStore();
          return `<i>${++n}</i>`;
        },
      },
    });
    const first = await core.renderRegion(p, region("a"), req("/p/[id]"));
    expect(first).toMatchObject({ noStore: true, cacheStatus: "miss" });
    expect((await core.renderRegion(p, region("a"), req("/p/[id]"))).html).toBe("<i>2</i>");
  });

  it("reports a failing region as an error frame with a digest, fatal only for server components", async () => {
    const fail = () => {
      const e = new Error("boom");
      e.stack = "at x";
      throw e;
    };
    const { core } = make({ pages: [page("/p")], runtime: { "r/a": fail }, modules: { "/p": { Broken: fail } } });
    const plain = await core.renderRegion(page("/p"), region("a"), req("/p"));
    expect(plain).toMatchObject({ type: "error", id: "a", status: 500, error: "boom", digest: "8a2474a04d", fatal: false });
    const server = await core.renderRegion(page("/p"), { id: "s", serverExport: "Broken" }, req("/p"));
    expect(server).toMatchObject({ type: "error", status: 500, fatal: true });
  });

  it("answers a region that calls forbidden() or unauthorized() with a 403 or 401 frame", async () => {
    const { core } = make({
      pages: [page("/p")],
      runtime: { "r/fb": () => forbidden(), "r/un": () => unauthorized() },
    });
    expect(await core.renderRegion(page("/p"), region("fb"), req("/p"))).toEqual({ type: "region", id: "fb", status: 403, html: "", denied: 403 });
    expect(await core.renderRegion(page("/p"), region("un"), req("/p"))).toEqual({ type: "region", id: "un", status: 401, html: "", denied: 401 });
  });

  it("answers a region that calls notFound() with a 404 frame and a redirect with a 307 frame", async () => {
    const { core } = make({
      pages: [page("/p")],
      runtime: {
        "r/nf": () => notFound(),
        "r/rd": () => {
          throw Object.assign(new Error("r"), { __krateRedirect: "/to" });
        },
      },
    });
    expect(await core.renderRegion(page("/p"), region("nf"), req("/p"))).toEqual({ type: "region", id: "nf", status: 404, html: "", notFound: true });
    expect(await core.renderRegion(page("/p"), region("rd"), req("/p"))).toEqual({ type: "region", id: "rd", status: 307, html: "", redirect: "/to" });
  });

  it("renders an async server component region from the page bundle's export", async () => {
    const { core } = make({
      pages: [page("/p")],
      modules: {
        "/p": {
          Price: async (props: any) => {
            await new Promise((ok) => setTimeout(ok, 5));
            return jsx("b", { children: `${props.item}:${props.params.id}` });
          },
        },
      },
    });
    const frame = await core.renderRegion(page("/p"), { id: "s", serverExport: "Price", props: { item: "pen" } }, req("/p", { params: { id: "9" } }));
    expect(frame).toMatchObject({ type: "region", status: 200, html: "<b>pen:9</b>" });
    const missing = await core.renderRegion(page("/p"), { id: "s", serverExport: "Nope" }, req("/p"));
    expect(missing).toMatchObject({ type: "error", status: 500, fatal: true });
    expect(String(missing.error)).toContain("does not export Nope");
  });
});

describe("renderPageRegion", () => {
  it("wraps a page render as a coarse region frame and extracts the title", async () => {
    const { core } = make({
      pages: [page("/p", "ssr")],
      modules: { "/p": { default: () => jsx("main", { children: [jsx("title", { children: "Hello &amp; bye" }), jsx("p", { children: "x" })] }) } },
    });
    const frame = await core.renderPageRegion(page("/p"), "page", req("/p"));
    expect(frame).toMatchObject({ type: "region", id: "page", kind: "page", status: 200 });
    expect(frame.title).toBe("Hello &amp;amp; bye");
    expect(String(frame.html)).toContain("<p>x</p>");
  });

  it("carries cache status, not-found, redirect and error details", async () => {
    const { core } = make({
      pages: [page("/i", "isr", { revalidate: 10 }), page("/nf"), page("/rd"), page("/err", "ssr", { serverComponents: true }), page("/err2")],
      modules: {
        "/i": { default: text("i") },
        "/nf": { default: () => notFound() },
        "/rd": {
          default: () => {
            throw Object.assign(new Error("r"), { __krateRedirect: "/to" });
          },
        },
        "/err": { default: () => { throw new Error("bad"); } },
        "/err2": { default: () => { throw new Error("bad"); } },
      },
    });
    expect(await core.renderPageRegion(page("/i", "isr", { revalidate: 10 }), "page", req("/i"))).toMatchObject({ cacheStatus: "miss" });
    expect(await core.renderPageRegion(page("/nf"), "page", req("/nf"))).toMatchObject({ status: 404, notFound: true });
    expect(await core.renderPageRegion(page("/rd"), "page", req("/rd"))).toMatchObject({ status: 307, redirect: "/to" });
    const fatal = await core.renderPageRegion(page("/err", "ssr", { serverComponents: true }), "page", req("/err"));
    expect(fatal).toMatchObject({ status: 500, error: "bad", fatal: true });
    expect(fatal.digest).toMatch(/^[0-9a-f]{10}$/);
    const soft = await core.renderPageRegion(page("/err2"), "page", req("/err2"));
    expect(soft.fatal).toBeUndefined();
  });
});
