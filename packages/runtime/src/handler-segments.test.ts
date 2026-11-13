// @vitest-environment node
import { describe, expect, it } from "vitest";
import { fillServerError, newServerError } from "./handler-segments.js";

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




