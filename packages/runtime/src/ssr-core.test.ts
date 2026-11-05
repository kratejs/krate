// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { createSSRCore, ISRCache, MemoryIsrStore, takeHeadTitle, type ManifestPage } from "./ssr-core.js";
import { Head, jsx } from "./server.js";

function makeCore(maxCacheSize: number, revalidate = 30) {
  let renders = 0;
  const page: ManifestPage = { route: "/plain/[id]", source: "plain.tsx", mode: "isr", revalidate, bundlePath: "plain" };
  const core = createSSRCore({
    manifest: { pages: [page], regions: {} } as any,
    maxCacheSize,
    loadPageModule: async () => ({ default: (props: any) => jsx("p", { children: `plain-${props.params.id}-${++renders}` }) }),
    loadRuntimeRenderer: () => () => "",
  });
  const render = (id: string) => core.renderPage({ route: "/plain/[id]", url: `/plain/${id}`, method: "GET", headers: {}, params: { id } });
  return { core, render, renders: () => renders };
}

afterEach(() => vi.useRealTimers());

describe("ISR page cache eviction", () => {
  it("keeps a hot page when many cold variants are requested", async () => {
    const { render } = makeCore(8);
    expect((await render("hot")).cacheStatus).toBe("miss");
    for (let i = 0; i < 40; i++) {
      await render("x" + i);
      expect((await render("hot")).cacheStatus, "after x" + i).toBe("hit");
    }
  });

  it("evicts the least recently used entry first", () => {
    const c = new ISRCache(2);
    const entry = (html: string) => ({ html, timestamp: Date.now() });
    c.set("a", entry("a"));
    c.set("b", entry("b"));
    c.get("a");
    c.set("c", entry("c"));
    expect(c.get("a")).toBeDefined();
    expect(c.get("b")).toBeUndefined();
  });
});

describe("ISR background refresh", () => {
  it("skips variants that are younger than their window", async () => {
    const { core, render, renders } = makeCore(8, 30);
    await render("a");
    await render("b");
    expect(renders()).toBe(2);
    const fresh = await core.applyInternalOp("refresh-route", { route: "/plain/[id]" });
    expect(fresh.refreshed).toBe(0);
    expect(renders()).toBe(2);

    vi.useFakeTimers();
    vi.setSystemTime(Date.now() + 31_000);
    const stale = await core.applyInternalOp("refresh-route", { route: "/plain/[id]" });
    expect(stale.refreshed).toBe(2);
    expect(renders()).toBe(4);
  });
});

describe("page region title", () => {
  it("moves the <title> out of the region html", async () => {
    const page: ManifestPage = { route: "/t", source: "t.tsx", mode: "ssr", bundlePath: "t" } as any;
    const core = createSSRCore({
      manifest: { pages: [page], regions: {} } as any,
      maxCacheSize: 4,
      loadPageModule: async () => ({
        default: () => jsx("div", { children: [jsx("title", { children: "Async Title" }), jsx("h1", { children: "hello" })] }),
      }),
      loadRuntimeRenderer: () => () => "",
    });
    const frame = await core.renderPageRegion(page, "page", { route: "/t", url: "/t", method: "GET", headers: {}, params: {} });
    expect(frame.title).toBe("Async Title");
    expect(frame.html).toContain("<h1>hello</h1>");
    expect(frame.html).not.toContain("<title");
  });

  it("carries <Head> meta, link and script tags on the frame, out of the body", async () => {
    const page: ManifestPage = { route: "/t", source: "t.tsx", mode: "ssr", bundlePath: "t" } as any;
    const core = createSSRCore({
      manifest: { pages: [page], regions: {} } as any,
      maxCacheSize: 4,
      loadPageModule: async () => ({
        default: () =>
          jsx("div", {
            children: [
              jsx(Head, { children: [jsx("meta", { name: "description", content: "d" }), jsx("link", { rel: "canonical", href: "/c" })] }),
              jsx("h1", { children: "hello" }),
            ],
          }),
      }),
      loadRuntimeRenderer: () => () => "",
    });
    const frame = await core.renderPageRegion(page, "page", { route: "/t", url: "/t", method: "GET", headers: {}, params: {} });
    expect(frame.head).toBe('<meta name="description" content="d"><link rel="canonical" href="/c">');
    expect(frame.html).toBe("<div><h1>hello</h1></div>");
  });

  it("carries the <Head> of a server component region, on a cache hit too", async () => {
    const page: ManifestPage = { route: "/t", source: "t.tsx", mode: "isr", revalidate: 60, bundlePath: "t" } as any;
    const region = { id: "r", serverExport: "R", revalidate: 60 } as any;
    const core = createSSRCore({
      manifest: { pages: [page], regions: {} } as any,
      maxCacheSize: 4,
      loadPageModule: async () => ({
        R: () => jsx("p", { children: [jsx(Head, { children: jsx("meta", { name: "author", content: "Ada" }) }), "by Ada"] }),
      }),
      loadRuntimeRenderer: () => () => "",
    });
    const req = { route: "/t", url: "/t", method: "GET", headers: {}, params: {} };
    for (const status of ["miss", "hit"]) {
      const frame = await core.renderRegion(page, region, req);
      expect(frame.cacheStatus).toBe(status);
      expect(frame.head).toBe('<meta name="author" content="Ada">');
      expect(frame.html).toBe("<p>by Ada</p>");
    }
  });

  it("keeps svg <title> elements and prefers the last document title", () => {
    const out = takeHeadTitle('<svg><title>Icon</title></svg><title>One</title><p>x</p><title>Two</title>');
    expect(out.title).toBe("Two");
    expect(out.html).toBe("<svg><title>Icon</title></svg><p>x</p>");
    expect(takeHeadTitle("<svg><title>Only</title></svg>")).toEqual({ title: "", html: "<svg><title>Only</title></svg>" });
  });
});

describe("server component region cache keys", () => {
  function regionCore() {
    let renders = 0;
    const page: ManifestPage = { route: "/feed", source: "feed.tsx", mode: "isr", revalidate: 60, bundlePath: "feed" } as any;
    const region = { id: "list", serverExport: "List", revalidate: 60 } as any;
    const core = createSSRCore({
      manifest: { pages: [page], regions: {} } as any,
      maxCacheSize: 8,
      loadPageModule: async () => ({
        List: (props: any) => jsx("p", { children: `page-${props.query?.page ?? "none"}-${++renders}` }),
        Whole: (props: any) => jsx("p", { children: Object.keys(props.query || {}).join(",") + "-" + ++renders }),
      }),
      loadRuntimeRenderer: () => () => "",
    });
    const render = (query: Record<string, string>, r = region) =>
      core.renderRegion(page, r, { route: "/feed", url: "/feed", method: "GET", headers: {}, params: {}, query });
    return { render, renders: () => renders, region };
  }

  it("shares one entry across query keys the region never reads", async () => {
    const { render, renders } = regionCore();
    expect((await render({ page: "1" })).cacheStatus).toBe("miss");
    expect(renders()).toBe(1);
    for (let i = 0; i < 20; i++) {
      const f = await render({ page: "1", utm: "junk" + i });
      expect(f.cacheStatus, "junk " + i).toBe("hit");
    }
    expect(renders()).toBe(1);
    expect((await render({ page: "2" })).cacheStatus).toBe("miss");
    expect((await render({ page: "1" })).html).toContain("page-1-1");
  });

  it("a bare first request does not make the region look query-independent", async () => {
    const { render } = regionCore();
    expect((await render({})).html).toContain("page-none-1");
    const f = await render({ page: "7" });
    expect(f.cacheStatus).toBe("miss");
    expect(f.html).toContain("page-7-");
    expect((await render({ page: "7", utm: "x" })).cacheStatus).toBe("hit");
    expect((await render({ page: "8" })).html).toContain("page-8-");
    expect((await render({})).html).toContain("page-none-1");
  });

  it("keeps a hot region when a client sweeps random query values", async () => {
    const { render } = regionCore();
    await render({ page: "1" });
    for (let i = 0; i < 40; i++) await render({ page: "1", sweep: String(i) });
    expect((await render({ page: "1" })).cacheStatus).toBe("hit");
  });

  it("falls back to the whole query when the region enumerates it", async () => {
    const { render, renders, region } = regionCore();
    const whole = { ...region, id: "whole", serverExport: "Whole" };
    await render({ a: "1" }, whole);
    expect((await render({ a: "1", b: "2" }, whole)).cacheStatus).toBe("miss");
    expect((await render({ a: "1", b: "2" }, whole)).cacheStatus).toBe("hit");
    expect(renders()).toBe(2);
  });
});

describe("runtime bundle region cache keys", () => {
  // A runtime component bundle (an IIFE with __krate_render) gets its props as
  // an object whose query records the keys it reads.
  function bundleCore(render: (props: any) => string, maxCacheSize = 8) {
    let renders = 0;
    const page: ManifestPage = { route: "/feed", source: "feed.tsx", mode: "isr", revalidate: 60, bundlePath: "feed" } as any;
    const region = { id: "list", bundlePath: "r/list", revalidate: 60, props: { nested: { n: 1 } } } as any;
    const core = createSSRCore({
      manifest: { pages: [page], regions: {} } as any,
      maxCacheSize,
      loadPageModule: async () => ({}),
      loadRuntimeRenderer: () => (props) => {
        renders++;
        return render(typeof props === "string" ? JSON.parse(props) : props);
      },
    });
    const run = (query: Record<string, string>) =>
      core.renderRegion(page, region, { route: "/feed", url: "/feed", method: "GET", headers: {}, params: {}, query });
    return { run, renders: () => renders, region };
  }

  it("keys a region on the query keys it reads, so random values share one entry", async () => {
    const { run, renders } = bundleCore((p) => `<p>page-${p.query?.page ?? "none"}</p>`);
    expect((await run({ page: "1" })).cacheStatus).toBe("miss");
    for (let i = 0; i < 50; i++) {
      const f = await run({ page: "1", utm: "junk" + i, [`k${i}`]: "v" });
      expect(f.cacheStatus, "junk " + i).toBe("hit");
    }
    expect(renders()).toBe(1);
    const other = await run({ page: "2" });
    expect(other.cacheStatus).toBe("miss");
    expect(other.html).toBe("<p>page-2</p>");
    expect((await run({ page: "1" })).html).toBe("<p>page-1</p>");
  });

  it("a region that never reads the query shares one entry for every query", async () => {
    const { run, renders } = bundleCore(() => "<p>static</p>");
    await run({ a: "1" });
    for (let i = 0; i < 20; i++) expect((await run({ ["x" + i]: "y" })).cacheStatus).toBe("hit");
    expect((await run({})).cacheStatus).toBe("hit");
    expect(renders()).toBe(1);
  });

  it("keys on the whole query when the region enumerates it", async () => {
    const { run, renders } = bundleCore((p) => `<p>${Object.keys(p.query || {}).join(",")}</p>`);
    await run({ a: "1" });
    expect((await run({ a: "1", b: "2" })).cacheStatus).toBe("miss");
    expect((await run({ a: "1", b: "2" })).cacheStatus).toBe("hit");
    expect(renders()).toBe(2);
  });

  it("serializing the props reads everything too", async () => {
    const { run } = bundleCore((p) => `<p>${JSON.stringify(p)}</p>`);
    await run({ a: "1" });
    expect((await run({ a: "1", b: "2" })).cacheStatus).toBe("miss");
  });

  it("does not let a bundle change the manifest's baked props", async () => {
    const { run, region } = bundleCore((p) => {
      p.nested.n++;
      return `<p>${p.nested.n}</p>`;
    });
    expect((await run({})).html).toBe("<p>2</p>");
    expect(region.props.nested.n).toBe(1);
  });

  it("renders instead of guessing when it does not know what a region reads", async () => {
    // An entry cached by a request without a query must not answer a request
    // with one when the reads are unknown (a fresh process after a restart).
    const page: ManifestPage = { route: "/feed", source: "feed.tsx", mode: "isr", revalidate: 60, bundlePath: "feed" } as any;
    const region = { id: "list", bundlePath: "r/list", revalidate: 60 } as any;
    const mk = () =>
      createSSRCore({
        manifest: { pages: [page], regions: {} } as any,
        maxCacheSize: 8,
        loadPageModule: async () => ({}),
        loadRuntimeRenderer: () => (props: any) => `<p>page-${props.query?.page ?? "none"}</p>`,
      });
    const first = mk();
    const r = (core: ReturnType<typeof mk>, query: Record<string, string>) =>
      core.renderRegion(page, region, { route: "/feed", url: "/feed", method: "GET", headers: {}, params: {}, query });
    expect((await r(first, {})).html).toBe("<p>page-none</p>");
    expect((await r(first, { page: "2" })).html).toBe("<p>page-2</p>");
  });
});

describe("page cache never answers a query it cannot match", () => {
  it("after a restart the persisted entry of the bare page does not answer ?page=2", async () => {
    const page: ManifestPage = { route: "/list", source: "list.tsx", mode: "isr", revalidate: 60, bundlePath: "list" } as any;
    const store = new MemoryIsrStore(8);
    const mk = () =>
      createSSRCore({
        manifest: { pages: [page], regions: {} } as any,
        isrStore: store,
        loadPageModule: async () => ({
          default: async (props: any) => {
            const { page: n } = await props.searchParams;
            return jsx("p", { children: `page-${n ?? "none"}` });
          },
        }),
        loadRuntimeRenderer: () => () => "",
      });
    const req = (query: Record<string, string>) => ({ route: "/list", url: "/list", method: "GET", headers: {}, params: {}, query });
    const before = mk();
    expect((await before.renderPage(req({}))).html).toBe("<p>page-none</p>");
    // A new process shares the persisted entries but has not learned what the page reads.
    const after = mk();
    const hit = await after.renderPage(req({}));
    expect(hit.cacheStatus).toBe("hit");
    const second = await after.renderPage(req({ page: "2" }));
    expect(second.html).toBe("<p>page-2</p>");
    expect(second.cacheStatus).toBe("miss");
    expect((await after.renderPage(req({ page: "2" }))).cacheStatus).toBe("hit");
    expect((await after.renderPage(req({ page: "3" }))).html).toBe("<p>page-3</p>");
  });

  it("a page that reads no query key is cached once for every query string", async () => {
    const page: ManifestPage = { route: "/plain", source: "p.tsx", mode: "isr", revalidate: 60, bundlePath: "p" } as any;
    let renders = 0;
    const core = createSSRCore({
      manifest: { pages: [page], regions: {} } as any,
      maxCacheSize: 8,
      loadPageModule: async () => ({ default: () => jsx("p", { children: "n" + ++renders }) }),
      loadRuntimeRenderer: () => () => "",
    });
    const req = (query: Record<string, string>) => ({ route: "/plain", url: "/plain", method: "GET", headers: {}, params: {}, query });
    await core.renderPage(req({ a: "1" }));
    for (let i = 0; i < 30; i++) expect((await core.renderPage(req({ ["q" + i]: "v" }))).cacheStatus).toBe("hit");
    expect(renders).toBe(1);
  });
});

describe("one client cannot flush the page and region caches", () => {
  it("keeps a page that was hit when 520 one-off variants follow it", async () => {
    const { render } = makeCore(512);
    expect((await render("hot")).cacheStatus).toBe("miss");
    expect((await render("hot")).cacheStatus).toBe("hit");
    expect((await render("hot")).cacheStatus).toBe("hit");
    for (let i = 1; i <= 520; i++) await render("x" + i);
    expect((await render("hot")).cacheStatus).toBe("hit");
  });

  it("keeps a hot region when a client mints 520 params variants", async () => {
    const page: ManifestPage = { route: "/p/[id]", source: "p.tsx", mode: "isr", revalidate: 60, bundlePath: "p" } as any;
    const region = { id: "r", bundlePath: "r/r", revalidate: 60 } as any;
    const core = createSSRCore({
      manifest: { pages: [page], regions: {} } as any,
      maxCacheSize: 512,
      loadPageModule: async () => ({}),
      loadRuntimeRenderer: () => (props: any) => `<p>${props.params.id}</p>`,
    });
    const run = (id: string) => core.renderRegion(page, region, { route: "/p/[id]", url: "/p/" + id, method: "GET", headers: {}, params: { id }, query: {} });
    await run("hot");
    expect((await run("hot")).cacheStatus).toBe("hit");
    for (let i = 1; i <= 520; i++) await run("x" + i);
    expect((await run("hot")).cacheStatus).toBe("hit");
  });

  it("still evicts one-off entries and bounds the cache", () => {
    const c = new ISRCache(4);
    const entry = (html: string) => ({ html, timestamp: Date.now() });
    for (let i = 0; i < 100; i++) c.set("k" + i, entry("v"));
    expect(c.size).toBe(4);
  });
});
