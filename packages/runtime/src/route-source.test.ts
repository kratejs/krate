import { describe, expect, it } from "vitest";
import { buildDestination, compileConditions, compileMatcher, compileSource, mergeRequestQuery } from "./route-source.js";

const m = (source: string, path: string, kind: "redirect" | "header" = "redirect") => compileSource(source, kind).match(path)?.params ?? null;

describe("route sources", () => {
  const table: Array<[string, string, Record<string, string> | null, ("redirect" | "header")?]> = [
    ["/old", "/old", {}],
    ["/old", "/old/", null],
    ["/legacy/*", "/legacy/a/b", { splat: "a/b" }],
    ["/legacy/*", "/legacy", { splat: "" }],
    ["/u/:name", "/u/ann", { name: "ann" }],
    ["/u/:name", "/u/ann/x", null],
    ["/docs/:path*", "/docs/a/b", { path: "a/b" }],
    ["/docs/:path*", "/docs", { path: "" }],
    ["/docs/:path+", "/docs", null],
    ["/p/:id?", "/p", { id: "" }],
    ["/:path*", "/", { path: "" }],
    ["/files/:id(\\d+)", "/files/42", { id: "42" }],
    ["/files/:id(\\d+)", "/files/x", null],
    ["/store/:slug((?!compare$)[^/]+)", "/store/foo", { slug: "foo" }],
    ["/store/:slug((?!compare$)[^/]+)", "/store/compare", null],
    ["/store/:slug((?!compare$)[^/]+)", "/store/compare/", null],
    ["/store/:slug((?!compare$)[^/]+)", "/store/compare-all", { slug: "compare-all" }],
    ["/img/:name.:ext", "/img/cat.png", { name: "cat", ext: "png" }],
    ["/blog{/:slug}?", "/blog", { slug: "" }],
    ["/assets/*", "/assets/x.js", {}, "header"],
    ["/a/*.js", "/a/x.js", {}, "header"],
    ["/a/*.js", "/a/b/x.js", null, "header"],
    ["/:path*", "/x/y", { path: "x/y" }, "header"],
  ];
  for (const [source, path, want, kind] of table) {
    it(`${source} vs ${path}`, () => expect(m(source, path, kind)).toEqual(want));
  }
});

const req = (target: string, headers: Record<string, string> = {}) => ({ headers: new Headers(headers), url: new URL("http://localhost" + target) });

describe("conditions", () => {
  it("query presence captures the key and ignores empty values", () => {
    const c = compileConditions([{ type: "query", key: "vehicle" }])!;
    expect(c.match(req("/s?vehicle=x"))).toEqual({ vehicle: "x" });
    expect(c.match(req("/s?vehicle="))).toBeNull();
    expect(c.match(req("/s"))).toBeNull();
  });
  it("anchors value regexes and captures named groups", () => {
    const c = compileConditions([{ type: "header", key: "x-m", value: "(?<kind>a|b)" }])!;
    expect(c.match(req("/s", { "x-m": "a" }))).toEqual({ kind: "a" });
    expect(c.match(req("/s", { "x-m": "ab" }))).toBeNull();
  });
  it("host strips the port; missing blocks", () => {
    const c = compileConditions([{ type: "host", value: "(?<sub>[a-z]+)\\.example\\.com" }], [{ type: "cookie", key: "auth" }])!;
    expect(c.match(req("/s", { host: "App.example.com:8080" }))).toEqual({ sub: "app" });
    expect(c.match(req("/s", { host: "app.example.com", cookie: "auth=1" }))).toBeNull();
  });
});

describe("destinations", () => {
  it("fills params and keeps redirects on-site", () => {
    const match = compileMatcher("/u/:name", "redirect").match("/u/%5Cevil.com".replace("%5C", "\\"), req("/"))!;
    expect(buildDestination("/:name", match)).toBe("/%5Cevil.com");
    const q = compileMatcher("/u/:name", "redirect").match("/u/a&b=c", req("/"))!;
    expect(buildDestination("/go?n=:name", q)).toBe("/go?n=a%26b%3Dc");
  });
  it("merges the request query, destination keys win", () => {
    expect(mergeRequestQuery("/t", "?vehicle=x")).toBe("/t?vehicle=x");
    expect(mergeRequestQuery("/t?a=9", "?a=1&b=2")).toBe("/t?a=9&b=2");
    expect(mergeRequestQuery("/t#top", "?a=1")).toBe("/t?a=1#top");
    expect(mergeRequestQuery("/t", "")).toBe("/t");
  });
});
