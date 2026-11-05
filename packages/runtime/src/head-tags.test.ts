import { describe, expect, it } from "vitest";
import { Head, renderToStringAsync, jsx } from "./server.js";
import { mergeHead, parseHeadTags, takeHead } from "./head-tags.js";

// The merge cases below are mirrored in internal/build/headmerge_test.go: the
// Go host and this handler must produce the same document head.
const MERGE_CASES: Array<{ name: string; doc: string; title: string; head: string; want: string }> = [
  {
    name: "adds tags before </head>, title when the shell has none",
    doc: "<html><head><meta charset=UTF-8></head><body>x</body></html>",
    title: "Post",
    head: '<meta name="description" content="d"><link rel="canonical" href="https://e.com/a">',
    want:
      "<html><head><meta charset=UTF-8><title>Post</title>" +
      '<meta name="description" content="d"><link rel="canonical" href="https://e.com/a"></head><body>x</body></html>',
  },
  {
    name: "page wins over baked tags with the same name, property and canonical (minified shell)",
    doc:
      "<head><meta name=description content=\"layout\"/><meta property=og:title content=Site /><meta property=og:site_name content=Site />" +
      "<link rel=canonical href=https://e.com/layout /><title>Baked</title></head>",
    title: "Fresh",
    head:
      '<meta name="description" content="page"><meta property="og:title" content="Page">' +
      '<link rel="canonical" href="https://e.com/page">',
    want:
      "<head><meta property=og:site_name content=Site /><title>Fresh</title>" +
      '<meta name="description" content="page"><meta property="og:title" content="Page">' +
      '<link rel="canonical" href="https://e.com/page"></head>',
  },
  {
    name: "repeated property keeps every tag, repeated name keeps the last",
    doc: "<head></head>",
    title: "",
    head:
      '<meta property="og:image" content="a"><meta property="og:image" content="b">' +
      '<meta name="robots" content="index"><meta name="robots" content="noindex">',
    want: '<head><meta property="og:image" content="a"><meta property="og:image" content="b"><meta name="robots" content="noindex"></head>',
  },
  {
    name: "identical tags are not added twice, scripts and links are appended",
    doc: '<head><link rel=stylesheet href=/a.css><script type=application/ld+json>{"a":1}</script></head>',
    title: "",
    head:
      '<link rel="stylesheet" href="/a.css"><link rel="stylesheet" href="/b.css">' +
      '<script type="application/ld+json">{"a":1}</script><script type="application/ld+json">{"b":2}</script>',
    want:
      '<head><link rel=stylesheet href=/a.css><script type=application/ld+json>{"a":1}</script>' +
      '<link rel="stylesheet" href="/b.css"><script type="application/ld+json">{"b":2}</script></head>',
  },
  {
    name: "a meta inside a baked script is not a tag, other elements are dropped",
    doc: '<head><script>var s="<meta name=description content=x>";</script></head>',
    title: "",
    head: '<meta name="description" content="p"><div>no</div><p>no</p>',
    want: '<head><script>var s="<meta name=description content=x>";</script><meta name="description" content="p"></head>',
  },
  {
    name: "escaped attribute values pass through",
    doc: "<head></head>",
    title: "A &amp; B",
    head: '<meta name="description" content="About &lt;&quot;x&quot;&gt; &amp; more">',
    want: '<head><title>A &amp; B</title><meta name="description" content="About &lt;&quot;x&quot;&gt; &amp; more"></head>',
  },
  {
    name: "no head tags and no title leaves the document alone",
    doc: "<head><title>Baked</title></head>",
    title: "",
    head: "",
    want: "<head><title>Baked</title></head>",
  },
  {
    name: "title only replaces the baked one",
    doc: "<head><title>Baked</title><meta name=description content=keep></head>",
    title: "New",
    head: "",
    want: "<head><title>New</title><meta name=description content=keep></head>",
  },
];

describe("takeHead", () => {
  it("lifts the title and the Head content out of the body, in place order", () => {
    const html =
      '<main><!--krate-head--><title>T</title><meta name="description" content="d"><!--/krate-head--><h1>x</h1>' +
      '<!--krate-head--><link rel="canonical" href="/c"><!--/krate-head--></main>';
    const out = takeHead(html);
    expect(out.title).toBe("T");
    expect(out.head).toBe('<meta name="description" content="d"><link rel="canonical" href="/c">');
    expect(out.html).toBe("<main><h1>x</h1></main>");
  });

  it("keeps a body <meta itemprop> that is not inside <Head>", () => {
    const html = '<div itemscope><meta itemprop="name" content="n"><span>x</span></div>';
    expect(takeHead(html)).toEqual({ title: "", head: "", html });
  });

  it("flattens nested Head blocks and keeps an svg title", () => {
    const html =
      '<!--krate-head--><meta name="a" content="1"><!--krate-head--><meta name="b" content="2"><!--/krate-head--><!--/krate-head-->' +
      "<svg><title>icon</title></svg>";
    const out = takeHead(html);
    expect(out.head).toBe('<meta name="a" content="1"><meta name="b" content="2">');
    expect(out.html).toBe("<svg><title>icon</title></svg>");
    expect(out.title).toBe("");
  });

  it("an unterminated Head block does not swallow the page", () => {
    const out = takeHead('<p>a</p><!--krate-head--><meta name="x" content="y">');
    expect(out.html).toBe("<p>a</p>");
    expect(out.head).toBe('<meta name="x" content="y">');
  });
});

describe("mergeHead", () => {
  for (const c of MERGE_CASES) {
    it(c.name, () => {
      expect(mergeHead(c.doc, c.title, c.head)).toBe(c.want);
    });
  }

  it("does nothing to a document without a head section", () => {
    expect(mergeHead("<p>x</p>", "T", '<meta name="a" content="b">')).toBe("<p>x</p>");
  });
});

describe("parseHeadTags", () => {
  it("reads unquoted, quoted and valueless attributes", () => {
    const tags = parseHeadTags("<script async src=/x.js></script><link rel='canonical' href=\"/c\"/>");
    expect(tags.map((t) => t.el.name)).toEqual(["script", "link"]);
    expect(tags[0].el.attrs.get("src")).toBe("/x.js");
    expect(tags[0].el.attrs.has("async")).toBe(true);
    expect(tags[1].key).toBe("link:canonical");
  });
});

describe("<Head> in a request-time render", () => {
  it("renders its children between markers and script text raw", async () => {
    const html = await renderToStringAsync(
      jsx(Head, {
        children: [
          jsx("meta", { name: "description", content: 'a "b" <c>' }),
          jsx("script", { type: "application/ld+json", children: JSON.stringify({ n: "</script><b>" }) }),
        ],
      }),
    );
    const out = takeHead(html);
    expect(out.html).toBe("");
    expect(out.head).toContain('<meta name="description" content="a &quot;b&quot; &lt;c&gt;">');
    expect(out.head).toContain('<script type="application/ld+json">{"n":"<\\/script><b>"}</script>');
  });
});
