// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { createSSRCore, ISRCache, type ManifestPage } from "./ssr-core.js";
import { jsx } from "./server.js";

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
