// @vitest-environment node
import { describe, expect, it } from "vitest";
import { buildDestination, compileSource, type MatchResult } from "./route-source.js";

function matchSource(path: string, source: string): MatchResult | null {
  try {
    return compileSource(source, "redirect").match(path);
  } catch {
    return null;
  }
}

// Regex parameters in next.config style sources (route-source.ts, the port of
// internal/routesource).
describe("regex redirect sources", () => {
  const cases: Array<[string, string, Record<string, string> | null]> = [
    ["/store/:slug((?!compare$)[^/]+)", "/store/some-template", { slug: "some-template" }],
    ["/store/:slug((?!compare$)[^/]+)", "/store/some-template/", { slug: "some-template" }],
    ["/store/:slug((?!compare$)[^/]+)", "/store/compare", null],
    ["/store/:slug((?!compare$)[^/]+)", "/store/a/b", null],
    ["/store/:slug((?!compare$)[^/]+)", "/store", null],
    ["/p/:id(\\d+)", "/p/42", { id: "42" }],
    ["/p/:id(\\d+)", "/p/x42", null],
    ["/p/:id(\\d+)?", "/p", { id: "" }],
    ["/p/:id(\\d+)?", "/p/7", { id: "7" }],
    ["/docs/:path((?:a|b)/.*)", "/docs/a/x/y", { path: "a/x/y" }],
    ["/v/:n(\\d+)*", "/v/1/2/3", { n: "1/2/3" }],
    ["/v/:n(\\d+)+", "/v", null],
    ["/(en|de)/about", "/de/about", {}],
    ["/(en|de)/about", "/fr/about", null],
    ["/bad/:x((unclosed)", "/bad/x", null],
  ];
  it.each(cases)("%s ~ %s", (source, path, want) => {
    const m = matchSource(path, source);
    if (want === null) expect(m).toBeNull();
    else expect(m?.params).toMatchObject(want);
  });

  it("fills the destination", () => {
    const m = matchSource("/store/some-template", "/store/:slug((?!compare$)[^/]+)");
    expect(m && buildDestination("/templates/:slug", m)).toBe("/templates/some-template");
  });
});
