import { describe, expect, it } from "vitest";
import { HEAD_CLOSE, HEAD_OPEN, mergeHead, setStartTagAttrs, splitDocTags, takeHead } from "./head-tags.js";

const DOC = '<!DOCTYPE html><html lang="en"><head><title>Base</title></head><body class="base"><div id="root">x</div></body></html>';

describe("document attributes from <Head>", () => {
  it("splits <html> and <body> out of a head fragment, later elements winning", () => {
    const { rest, html, body } = splitDocTags('<meta name="a" content="b"><html lang="fr" class="a"></html><html class="b"></html><body data-x="1"></body>');
    expect(rest).toBe('<meta name="a" content="b">');
    expect(Object.fromEntries(html)).toEqual({ lang: "fr", class: "b" });
    expect(Object.fromEntries(body)).toEqual({ "data-x": "1" });
  });

  it("sets attributes on the document elements and escapes values", () => {
    const out = mergeHead(DOC, "", '<html class="dark" data-q="a&quot;b"></html><body class="font-sans"></body>');
    expect(out).toContain('<html class="dark" data-q="a&quot;b" lang="en">');
    expect(out).toContain('<body class="font-sans">');
    expect(out).not.toContain("</html></html>");
  });

  it("keeps the other attributes of a start tag", () => {
    expect(setStartTagAttrs('<body a="1" b="2">', "body", new Map([["b", "3"]]))).toBe('<body a="1" b="3">');
  });

  it("flows from a request-time <Head> through takeHead", () => {
    const split = takeHead("<main>" + HEAD_OPEN + '<html class="dark"></html><title>T</title>' + HEAD_CLOSE + "</main>");
    expect(mergeHead(DOC, split.title, split.head)).toContain('<html class="dark" lang="en">');
  });
});
