// @vitest-environment node
import { describe, expect, it } from "vitest";
import { createMemoryStaticLayer } from "./static-memory.js";

describe("createMemoryStaticLayer", () => {
  const layer = createMemoryStaticLayer({ "index.html": "home", "about/index.html": "about", "404.html": "nf" });
  const req = (p: string, method = "GET") => new Request("https://x.test" + p + "?q=1", { method });
  it("finds pages by directory and file path", async () => {
    expect(await layer.exists("/")).toBe(true);
    expect(await layer.exists("/about/")).toBe(true);
    expect(await layer.exists("/about")).toBe(true);
    expect(await layer.exists("/404.html")).toBe(true);
    expect(await layer.exists("/nope/")).toBe(false);
    expect(await layer.isAsset("/about/")).toBe(false);
  });
  it("serves HTML, redirects a bare directory and answers HEAD without a body", async () => {
    const ok = (await layer.serve(req("/about/"), "/about/"))!;
    expect(ok.status).toBe(200);
    expect(ok.headers.get("content-type")).toBe("text/html; charset=utf-8");
    expect(await ok.text()).toBe("about");
    const redir = (await layer.serve(req("/about"), "/about"))!;
    expect(redir.status).toBe(301);
    expect(redir.headers.get("location")).toBe("about/?q=1");
    expect(await (await layer.serve(req("/", "HEAD"), "/"))!.text()).toBe("");
    expect(await layer.serve(req("/zzz/"), "/zzz/")).toBeNull();
  });
});
