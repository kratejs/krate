import { describe, expect, it } from "vitest";
import {
  applyDynamicRouteParams,
  compareRouteSpecificity,
  compileMatcher,
  goRedirect,
  headerSourceMatches,
  isHiddenPath,
  isInternalOutputPath,
  matchRedirect,
  matchRoutePattern,
  rewriteDestination,
} from "./handler-routing.js";

// The vectors below are copied from the Go host's tests (internal/build
// serve_test.go, serve_security_test.go, routematch tests) so the portable
// handler decides like `krate start`.
describe("redirect matching", () => {
  const cases: Array<[string, string, boolean]> = [
    ["/old", "/old", true],
    ["/old", "/old/", false],
    ["/old/page", "/old", false],
    ["/old/page", "/old/*", true],
    ["/old", "/old/*", true],
    ["/oldpage", "/old/*", false],
    ["/old/", "/old/*", true],
    ["/old//double", "/old/*", true],
    ["/a", "/b", false],
  ];
  for (const [path, source, want] of cases) {
    it(`${path} ~ ${source}`, () => expect(matchRedirect(path, source)).toBe(want));
  }
});

describe("rewriteDestination", () => {
  const cases: Array<[string, string, string, string]> = [
    ["/legacy//evil.com", "/legacy/*", "/:splat", "/evil.com"],
    ["/legacy/\\evil.com", "/legacy/*", "/:splat", "/%5Cevil.com"],
    ["/u/\\\\evil.com", "/u/:name", "/:name", "/%5C%5Cevil.com"],
    ["/legacy/https://evil.com", "/legacy/*", "/:splat", "/https://evil.com"],
    ["/legacy/a\nb", "/legacy/*", "/x/:splat", "/x/a%0Ab"],
    ["/old/\\evil.com", "/old/*", "/new", "/new/%5Cevil.com"],
    ["/go/x", "/go/*", "https://example.com/:splat", "https://example.com/x"],
    ["/go/x", "/go/*", "//cdn.example.com/:splat", "//cdn.example.com/x"],
    ["/legacy/a b", "/legacy/*", "/new/:splat", "/new/a%20b"],
    ["/docs/a/b", "/docs/:path*", "/documentation/:path*", "/documentation/a/b"],
    ["/docs", "/docs/:path*", "/documentation/:path*", "/documentation/"],
    ["/docs/a", "/docs/:slug?", "/documentation/:slug?", "/documentation/a"],
    ["/p/7", "/p/:id", "/item/:id?ref=1", "/item/7?ref=1"],
    ["/u/bob", "/u/:name", "/:name", "/bob"],
    ["/old", "/old", "/new", "/new"],
  ];
  for (const [path, source, dest, want] of cases) {
    it(`${path} ${source} -> ${dest}`, () => expect(rewriteDestination(path, source, dest)).toBe(want));
  }
  it("rejects non-matching optional/plus forms", () => {
    expect(matchRedirect("/docs", "/docs/:path+")).toBe(false);
    expect(matchRedirect("/docs/a/b", "/docs/:slug?")).toBe(false);
  });
});

describe("route patterns", () => {
  it("matches params and catch-alls", () => {
    expect(matchRoutePattern("/blog/x", "blog/[slug]")).toEqual({ slug: "x" });
    expect(matchRoutePattern("/docs/a/b", "docs/[...rest]")).toEqual({ rest: "a/b" });
    expect(matchRoutePattern("/docs", "docs/[...rest]")).toBeNull();
    expect(matchRoutePattern("/shop", "shop/[[...filter]]")).toEqual({ filter: "" });
    expect(matchRoutePattern("/blog", "blog/[slug]")).toBeNull();
  });
  it("orders by specificity: static > param > catch-all > optional catch-all", () => {
    const routes = ["/a/[[...x]]", "/a/[...x]", "/a/[x]", "/a/b"];
    expect([...routes].sort(compareRouteSpecificity)).toEqual(["/a/b", "/a/[x]", "/a/[...x]", "/a/[[...x]]"]);
  });
});

describe("paths and headers", () => {
  it("hides internal and dot paths", () => {
    for (const p of ["/api-node/hello.js", "/server-manifest.json", "/%61pi-node/x.js", "/.krate/x", "/a/../.krate/x"]) {
      expect(isInternalOutputPath(p)).toBe(true);
    }
    for (const p of ["/manifest.json", "/api/hello", "/api-nodes/x"]) expect(isInternalOutputPath(p)).toBe(false);
    expect(isHiddenPath("/.env")).toBe(true);
    expect(isHiddenPath("/.well-known/acme")).toBe(false);
    expect(isHiddenPath("/a/%2eenv")).toBe(true);
  });
  it("matches header sources", () => {
    expect(headerSourceMatches("/about", "/about")).toBe(true);
    expect(headerSourceMatches("/assets/*", "/assets/a/b.js")).toBe(true);
    expect(headerSourceMatches("/a/*/c", "/a/b/c")).toBe(true);
    expect(headerSourceMatches("/a/*/c", "/a/b/d/c")).toBe(false);
  });
  it("compiles the middleware matcher like the Go runtimes", () => {
    expect(compileMatcher("/api/:path*").test("/api/x/y")).toBe(true);
    expect(compileMatcher("/api/:path*").test("/other")).toBe(false);
    expect(compileMatcher("/admin").test("/admin/")).toBe(true);
    expect(() => compileMatcher("nope")).toThrow(/start with/);
  });
});

describe("templates and redirects", () => {
  it("substitutes sentinels and injects params", () => {
    const html = applyDynamicRouteParams("<head></head><p>__KRATE_PARAM_slug__</p>", { slug: "a<b" });
    expect(html).toContain("<p>a&lt;b</p>");
    expect(html).toContain('<script>window.__KRATE_PARAMS__={"slug":"a\\u003cb"}</script></head>');
  });
  it("builds http.Redirect responses", async () => {
    const r = goRedirect("/about", "/old", "GET", 301);
    expect(r.status).toBe(301);
    expect(r.headers.get("location")).toBe("/about");
    expect(await r.text()).toBe('<a href="/about">Moved Permanently</a>.\n\n');
    expect(await goRedirect("rel", "/a/b", "GET", 302).headers.get("location")).toBe("/a/rel");
    expect(goRedirect("/x", "/y", "HEAD", 302).headers.get("content-type")).toContain("text/html");
  });
});
