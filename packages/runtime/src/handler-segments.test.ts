// @vitest-environment node
import { describe, expect, it } from "vitest";
import { fillServerError, lookupSegmentPage, newServerError, segmentRoute } from "./handler-segments.js";

describe("request-time segment pages", () => {
  it("names the renderer route of a dynamic entry and only of that role", () => {
    const entries = [
      { key: "", dir: "", notFound: "404.html", error: "500.html", dynamic: ["notFound" as const] },
      { key: "blog", dir: "blog", notFound: "blog/404.html" },
    ];
    expect(lookupSegmentPage(entries, "notFound", "/x", false, "")).toEqual({ file: "404.html", key: "", params: {}, route: "/__404" });
    expect(lookupSegmentPage(entries, "error", "/x", false, "")?.route).toBeUndefined();
    expect(lookupSegmentPage(entries, "notFound", "/blog/x", true, "blog")?.route).toBeUndefined();
    expect(segmentRoute("shop/blog", "unauthorized")).toBe("/shop/blog/__401");
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




