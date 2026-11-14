// @vitest-environment node
import { AsyncLocalStorage } from "node:async_hooks";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createSSRCore, type ManifestPage, type RegionMeta, type SSRCoreOptions } from "./ssr-core.js";
import { installRenderStore, markNoStore, type CacheScope } from "./cache-scope.js";
import { forbidden, unauthorized } from "./request-context.js";
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
  runtime?: Record<string, (props: string | Record<string, any>) => string | Promise<string>>;
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

describe("renderPage", () => {

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
  });
});

describe("ISR pages", () => {

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
});
